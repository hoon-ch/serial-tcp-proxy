use crate::config::Config;
use chrono::{Local, SecondsFormat};
use serde_json::{Value, json};
use std::{
    collections::{HashMap, VecDeque},
    fs::OpenOptions,
    io::{BufWriter, Write},
    sync::{
        Arc, Mutex,
        atomic::{AtomicU64, Ordering},
    },
    time::{Duration, Instant},
};
use tokio::{
    io::{AsyncReadExt, AsyncWriteExt},
    net::{TcpListener, TcpStream, tcp::OwnedWriteHalf},
    sync::{Mutex as AsyncMutex, mpsc, watch},
    time::{sleep, timeout},
};

pub fn timestamp() -> String {
    Local::now().to_rfc3339_opts(SecondsFormat::Secs, true)
}
fn timestamp_nano() -> String {
    Local::now().to_rfc3339_opts(SecondsFormat::AutoSi, true)
}

// Mirrors Go's LogPacket as shipped, which rebuilds the string for every byte.
#[cfg(not(feature = "linear-hex"))]
fn hex_spaced(data: &[u8]) -> String {
    use std::fmt::Write as _;
    let mut encoded = String::with_capacity(data.len() * 2);
    for b in data {
        let _ = write!(encoded, "{b:02x}");
    }
    let mut hex = String::new();
    for (i, pair) in encoded.as_bytes().chunks_exact(2).enumerate() {
        if i > 0 {
            hex = format!("{hex} ");
        }
        hex = format!("{hex}{}", std::str::from_utf8(pair).unwrap());
    }
    hex
}

#[cfg(feature = "linear-hex")]
fn hex_spaced(data: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut out = Vec::with_capacity(data.len() * 3);
    for (i, b) in data.iter().enumerate() {
        if i > 0 {
            out.push(b' ');
        }
        out.extend_from_slice(&[DIGITS[(b >> 4) as usize], DIGITS[(b & 0x0f) as usize]]);
    }
    String::from_utf8(out).unwrap()
}

struct Client {
    addr: String,
    connected_at: String,
    writer: Arc<AsyncMutex<OwnedWriteHalf>>,
    cancel: watch::Sender<bool>,
}
struct WebClient {
    addr: String,
    connected_at: String,
    cancel: watch::Sender<bool>,
}
struct Inner {
    state: &'static str,
    last_connected: Option<String>,
    clients: HashMap<String, Client>,
    web: HashMap<String, WebClient>,
    sse: usize,
    logs: VecDeque<String>,
    subscribers: HashMap<u64, (bool, mpsc::Sender<String>, watch::Sender<bool>)>,
    sessions: HashMap<String, Instant>,
    file: Option<BufWriter<std::fs::File>>,
}

pub struct State {
    pub config: Config,
    start: Instant,
    start_time: String,
    inner: Mutex<Inner>,
    next: AtomicU64,
    next_tcp: AtomicU64,
    next_web: AtomicU64,
    upstream: AsyncMutex<Option<OwnedWriteHalf>>,
}
impl State {
    pub fn new(config: Config) -> Arc<Self> {
        let file = if config.log_packets && !config.log_file.is_empty() {
            OpenOptions::new()
                .create(true)
                .append(true)
                .open(&config.log_file)
                .ok()
                .map(|f| BufWriter::with_capacity(4096, f))
        } else {
            None
        };
        Arc::new(Self {
            config,
            start: Instant::now(),
            start_time: timestamp(),
            inner: Mutex::new(Inner {
                state: "Disconnected",
                last_connected: None,
                clients: HashMap::new(),
                web: HashMap::new(),
                sse: 0,
                logs: VecDeque::new(),
                subscribers: HashMap::new(),
                sessions: HashMap::new(),
                file,
            }),
            next: AtomicU64::new(0),
            next_tcp: AtomicU64::new(0),
            next_web: AtomicU64::new(0),
            upstream: AsyncMutex::new(None),
        })
    }
    pub fn info(&self, level: &str, msg: &str) {
        let line = format!("{} [{}] {}\n", timestamp_nano(), level, msg);
        print!("{line}");
        self.publish(line);
    }
    pub fn packet(&self, direction: &str, data: &[u8], source: &str) {
        let hex = hex_spaced(data);
        let suffix = if source.is_empty() {
            String::new()
        } else {
            format!(" from {source}")
        };
        let line = format!(
            "{} [PKT] [{}] {} ({} bytes){}\n",
            timestamp_nano(),
            direction,
            hex,
            data.len(),
            suffix
        );
        if self.config.log_packets {
            print!("{line}");
            if let Some(file) = self.inner.lock().unwrap().file.as_mut() {
                let _ = file.write_all(line.as_bytes());
            }
        }
        self.publish(line);
    }
    fn publish(&self, line: String) {
        let websocket_line = json!({"type": "log", "data": line}).to_string();
        let mut g = self.inner.lock().unwrap();
        if g.logs.len() == 1000 {
            g.logs.pop_front();
        }
        g.logs.push_back(line.clone());
        g.subscribers.retain(|_, (ws, tx, cancel)| {
            let message = if *ws { &websocket_line } else { &line };
            match tx.try_send(message.clone()) {
                Ok(_) => true,
                Err(mpsc::error::TrySendError::Full(_)) if !*ws => true,
                Err(_) => {
                    let _ = cancel.send(true);
                    false
                }
            }
        });
    }
    pub fn subscribe(
        &self,
        ws: bool,
        addr: String,
    ) -> Option<(
        u64,
        Option<String>,
        Vec<String>,
        mpsc::Receiver<String>,
        watch::Receiver<bool>,
    )> {
        let mut g = self.inner.lock().unwrap();
        if g.clients.len() + g.web.len() + g.sse >= self.config.max_clients as usize {
            return None;
        }
        let id = self.next.fetch_add(1, Ordering::Relaxed) + 1;
        let (tx, rx) = mpsc::channel(if ws { 256 } else { 10 });
        let (cancel, crx) = watch::channel(false);
        let web_id = if ws {
            let name = format!("web#{}", self.next_web.fetch_add(1, Ordering::Relaxed) + 1);
            g.web.insert(
                name.clone(),
                WebClient {
                    addr,
                    connected_at: timestamp(),
                    cancel: cancel.clone(),
                },
            );
            Some(name)
        } else {
            g.sse += 1;
            None
        };
        g.subscribers.insert(id, (ws, tx, cancel));
        Some((id, web_id, g.logs.iter().cloned().collect(), rx, crx))
    }
    pub fn unsubscribe(&self, id: u64, web_id: Option<&str>) {
        let mut g = self.inner.lock().unwrap();
        g.subscribers.remove(&id);
        if let Some(id) = web_id {
            g.web.remove(id);
        } else {
            g.sse = g.sse.saturating_sub(1);
        }
    }
    pub fn status(&self) -> Value {
        let g = self.inner.lock().unwrap();
        json!({"upstream_state":g.state,"upstream_addr":self.config.upstream_addr(),"listen_addr":self.config.listen_addr(),"connected_clients":g.clients.len()+g.web.len()+g.sse,"max_clients":self.config.max_clients,"start_time":self.start_time})
    }
    pub fn health(&self) -> Value {
        let g = self.inner.lock().unwrap();
        let connected = g.state == "Connected";
        let mut up = json!({"status":if connected {"healthy"} else {"unhealthy"},"connected":connected,"address":self.config.upstream_addr()});
        if let Some(t) = &g.last_connected {
            up["last_connected"] = json!(t);
        }
        json!({"status":if connected {"healthy"} else {"degraded"},"version":"dev","uptime":self.start.elapsed().as_secs(),"checks":{"upstream":up,"clients":{"status":"healthy","count":g.clients.len()+g.web.len()+g.sse,"max":self.config.max_clients},"web_server":{"status":"healthy","port":self.config.web_port}},"timestamp":timestamp()})
    }
    pub fn clients(&self) -> Value {
        let g = self.inner.lock().unwrap();
        let mut list: Vec<Value> = g
            .clients
            .iter()
            .map(
                |(id, c)| json!({"id":id,"addr":c.addr,"connected_at":c.connected_at,"type":"tcp"}),
            )
            .collect();
        list.extend(g.web.iter().map(
            |(id, c)| json!({"id":id,"addr":c.addr,"connected_at":c.connected_at,"type":"web"}),
        ));
        json!({"clients":list,"tcp_count":g.clients.len(),"web_count":g.web.len()+g.sse,"total_count":g.clients.len()+g.web.len()+g.sse,"max_clients":self.config.max_clients})
    }
    pub fn disconnect(&self, id: &str) -> bool {
        let mut g = self.inner.lock().unwrap();
        if let Some(c) = g.clients.remove(id) {
            let _ = c.cancel.send(true);
            let total = g.clients.len() + g.web.len() + g.sse;
            drop(g);
            self.info(
                "INFO",
                &format!(
                    "Client disconnected: {} [{}] (total: {})",
                    c.addr, id, total
                ),
            );
            true
        } else if let Some(c) = g.web.get(id) {
            let _ = c.cancel.send(true);
            true
        } else {
            false
        }
    }
    pub fn session_valid(&self, token: &str) -> bool {
        self.inner
            .lock()
            .unwrap()
            .sessions
            .get(token)
            .is_some_and(|t| t.elapsed() < Duration::from_secs(86400))
    }
    pub fn add_session(&self, token: String) {
        self.inner
            .lock()
            .unwrap()
            .sessions
            .insert(token, Instant::now());
    }
    pub fn remove_session(&self, token: &str) {
        self.inner.lock().unwrap().sessions.remove(token);
    }
    pub async fn write_upstream(&self, data: &[u8]) -> std::io::Result<()> {
        let mut g = self.upstream.lock().await;
        let w = g.as_mut().ok_or_else(|| {
            std::io::Error::new(
                std::io::ErrorKind::NotConnected,
                "use of closed network connection",
            )
        })?;
        timeout(Duration::from_secs(5), w.write_all(data))
            .await
            .map_err(|_| std::io::Error::from(std::io::ErrorKind::TimedOut))?
    }
    pub async fn broadcast(&self, data: &[u8]) {
        let clients: Vec<_> = {
            let g = self.inner.lock().unwrap();
            g.clients
                .iter()
                .map(|(id, c)| (id.clone(), c.writer.clone()))
                .collect()
        };
        for (id, w) in clients {
            if !matches!(
                timeout(Duration::from_millis(100), async {
                    w.lock().await.write_all(data).await
                })
                .await,
                Ok(Ok(()))
            ) {
                self.disconnect(&id);
            }
        }
    }
    pub async fn inject(&self, target: &str, data: &[u8]) -> bool {
        match target {
            "upstream" => {
                if self.inner.lock().unwrap().state != "Connected" {
                    return false;
                }
                self.packet("->UP", data, "INJECT");
                self.write_upstream(data).await.is_ok()
            }
            "downstream" => {
                self.packet("UP->", data, "INJECT");
                self.broadcast(data).await;
                true
            }
            _ => false,
        }
    }
    pub async fn run_upstream(self: Arc<Self>) {
        let mut delay = 1;
        loop {
            self.inner.lock().unwrap().state = "Connecting";
            self.info(
                "INFO",
                &format!("Connecting to upstream {}", self.config.upstream_addr()),
            );
            match timeout(
                Duration::from_secs(10),
                TcpStream::connect(self.config.upstream_addr()),
            )
            .await
            {
                Ok(Ok(stream)) => {
                    delay = 1;
                    let (mut rd, wr) = stream.into_split();
                    *self.upstream.lock().await = Some(wr);
                    {
                        let mut g = self.inner.lock().unwrap();
                        g.state = "Connected";
                        g.last_connected = Some(timestamp());
                    }
                    self.info(
                        "INFO",
                        &format!("Connected to upstream {}", self.config.upstream_addr()),
                    );
                    let mut buf = [0u8; 4096];
                    loop {
                        match timeout(Duration::from_secs(60), rd.read(&mut buf)).await {
                            Ok(Ok(n)) if n > 0 => {
                                self.packet("UP->", &buf[..n], "");
                                self.broadcast(&buf[..n]).await;
                            }
                            Ok(Err(error)) => {
                                self.info("WARN", &format!("Upstream read error: {error}"));
                                break;
                            }
                            _ => {
                                self.info(
                                    "WARN",
                                    "Upstream read error: connection closed or timed out",
                                );
                                break;
                            }
                        }
                    }
                    *self.upstream.lock().await = None;
                    self.inner.lock().unwrap().state = "Disconnected";
                    self.info("WARN", "Upstream connection lost, reconnecting...");
                }
                result => {
                    self.inner.lock().unwrap().state = "Disconnected";
                    self.info(
                        "ERROR",
                        &format!("Failed to connect to upstream: {result:?}"),
                    );
                    sleep(Duration::from_secs(delay)).await;
                    delay = (delay * 2).min(30);
                }
            }
        }
    }
    pub async fn run_listener(self: Arc<Self>, listener: TcpListener) {
        while let Ok((stream, addr)) = listener.accept().await {
            let mut g = self.inner.lock().unwrap();
            if g.clients.len() + g.web.len() + g.sse >= self.config.max_clients as usize {
                drop(g);
                self.info(
                    "WARN",
                    &format!(
                        "Rejecting connection from {addr}: max clients ({}) reached",
                        self.config.max_clients
                    ),
                );
                drop(stream);
                continue;
            }
            let sock = socket2::SockRef::from(&stream);
            let _ = sock.set_tcp_keepalive(
                &socket2::TcpKeepalive::new().with_time(Duration::from_secs(30)),
            );
            #[cfg(target_os = "linux")]
            {
                use std::os::fd::AsRawFd;
                let value: libc::c_int = 30000;
                unsafe {
                    libc::setsockopt(
                        stream.as_raw_fd(),
                        libc::IPPROTO_TCP,
                        libc::TCP_USER_TIMEOUT,
                        &value as *const _ as *const _,
                        std::mem::size_of_val(&value) as _,
                    );
                }
            }
            let id = format!(
                "client#{}",
                self.next_tcp.fetch_add(1, Ordering::Relaxed) + 1
            );
            let (mut rd, wr) = stream.into_split();
            let (cancel, mut crx) = watch::channel(false);
            g.clients.insert(
                id.clone(),
                Client {
                    addr: addr.to_string(),
                    connected_at: timestamp(),
                    writer: Arc::new(AsyncMutex::new(wr)),
                    cancel,
                },
            );
            let total = g.clients.len() + g.web.len() + g.sse;
            drop(g);
            self.info(
                "INFO",
                &format!("Client connected: {addr} [{id}] (total: {total})"),
            );
            let state = self.clone();
            tokio::spawn(async move {
                let mut buf = [0u8; 4096];
                loop {
                    tokio::select! {
                        _ = crx.changed() => break,
                        result = rd.read(&mut buf) => match result {
                            Ok(n) if n > 0 => {
                                state.packet("->UP", &buf[..n], &id);
                                if state.write_upstream(&buf[..n]).await.is_err() {
                                    state.info("WARN", &format!("Upstream not connected, dropping packet from {id}"));
                                }
                            }
                            _ => break,
                        }
                    }
                }
                state.disconnect(&id);
            });
        }
    }
    pub async fn flush_loop(self: Arc<Self>) {
        loop {
            sleep(Duration::from_secs(1)).await;
            if let Some(f) = self.inner.lock().unwrap().file.as_mut() {
                let _ = f.flush();
            }
        }
    }
    pub async fn shutdown(&self) {
        let mut g = self.inner.lock().unwrap();
        for (_, c) in g.clients.drain() {
            let _ = c.cancel.send(true);
        }
        for (_, c) in g.web.drain() {
            let _ = c.cancel.send(true);
        }
        if let Some(f) = g.file.as_mut() {
            let _ = f.flush();
        }
        drop(g);
        *self.upstream.lock().await = None;
    }
}
