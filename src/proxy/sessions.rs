use super::*;

impl State {
    pub fn session_valid(&self, token: &str) -> bool {
        let mut inner = self.inner.lock().unwrap();
        match inner.sessions.get(token) {
            Some(at) if at.elapsed() < Duration::from_secs(86400) => true,
            Some(_) => {
                inner.sessions.remove(token);
                false
            }
            None => false,
        }
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
    pub async fn cleanup_sessions(self: Arc<Self>) {
        loop {
            sleep(Duration::from_secs(3600)).await;
            self.inner
                .lock()
                .unwrap()
                .sessions
                .retain(|_, at| at.elapsed() < Duration::from_secs(86400));
        }
    }
}
