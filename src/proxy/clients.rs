use super::*;

impl State {
    pub async fn broadcast(&self, data: &[u8]) {
        let clients: Vec<_> = {
            let g = self.inner.lock().unwrap();
            g.clients
                .iter()
                .map(|(id, c)| (id.clone(), c.addr.clone(), c.writer.clone()))
                .collect()
        };
        for (id, addr, w) in clients {
            let result = timeout(Duration::from_millis(100), async {
                w.lock().await.write_all(data).await
            }).await;
            let error = match result {
                Ok(Ok(())) => continue,
                Ok(Err(error)) => error.to_string(),
                Err(error) => error.to_string(),
            };
            self.info("WARN", &format!("Failed to write to {addr} [{id}]: {error}"));
            self.disconnect(&id);
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
}
