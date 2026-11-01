use super::*;

impl State {
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
}
