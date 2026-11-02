mod clients;
mod sessions;
mod upstream;

use crate::config::Config;
use crate::packet::{hex_spaced, timestamp, timestamp_nano};
use serde_json::{Value, json};
use std::{
    collections::{HashMap, VecDeque},
    fs::OpenOptions,
    io::{BufWriter, Write},
    sync::{
        Arc, Mutex,
        atomic::{AtomicU64, Ordering},
    },
    time::Duration,
};
use tokio::{
    io::{AsyncReadExt, AsyncWriteExt},
    net::{TcpListener, TcpStream, tcp::OwnedWriteHalf},
    sync::{Mutex as AsyncMutex, mpsc, watch},
    time::{Instant, sleep, timeout},
};

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
type Subscription = (
    u64,
    Option<String>,
    Vec<String>,
    mpsc::Receiver<String>,
    watch::Receiver<bool>,
);
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
        let (file, file_error) = if config.log_packets && !config.log_file.is_empty() {
            match OpenOptions::new()
                .create(true)
                .append(true)
                .open(&config.log_file)
            {
                Ok(file) => (Some(BufWriter::with_capacity(4096, file)), None),
                Err(error) => (None, Some(error)),
            }
        } else {
            (None, None)
        };
        let state = Arc::new(Self {
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
        });
        if let Some(error) = file_error {
            state.info(
                "WARN",
                &format!(
                    "Failed to open log file {}: {error}, packet logging to file disabled",
                    state.config.log_file
                ),
            );
        }
        state
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
    pub fn subscribe(&self, ws: bool, addr: String) -> Option<Subscription> {
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
        json!({"status":if connected {"healthy"} else {"degraded"},"version":option_env!("STP_VERSION").unwrap_or("dev"),"uptime":self.start.elapsed().as_secs(),"checks":{"upstream":up,"clients":{"status":"healthy","count":g.clients.len()+g.web.len()+g.sse,"max":self.config.max_clients},"web_server":{"status":"healthy","port":self.config.web_port}},"timestamp":timestamp()})
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
    pub async fn flush_loop(self: Arc<Self>) {
        loop {
            sleep(Duration::from_secs(1)).await;
            if let Some(f) = self.inner.lock().unwrap().file.as_mut() {
                let _ = f.flush();
            }
        }
    }
    pub async fn shutdown(&self) {
        {
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
        }
        *self.upstream.lock().await = None;
    }
}
