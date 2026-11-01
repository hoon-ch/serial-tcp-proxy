use serde::Deserialize;
use std::{env, fs};

#[derive(Clone, Deserialize)]
#[serde(default)]
pub struct Config {
    pub upstream_host: String,
    pub upstream_port: i32,
    pub listen_port: i32,
    pub max_clients: i32,
    pub log_packets: bool,
    pub log_file: String,
    pub web_port: i32,
    pub web_auth_enabled: bool,
    pub web_auth_username: String,
    pub web_auth_password: String,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            upstream_host: String::new(),
            upstream_port: 8899,
            listen_port: 18899,
            max_clients: 10,
            log_packets: false,
            log_file: "/data/packets.log".into(),
            web_port: 18080,
            web_auth_enabled: false,
            web_auth_username: String::new(),
            web_auth_password: String::new(),
        }
    }
}

impl Config {
    pub fn load() -> Result<Self, String> {
        let mut c: Self = match fs::read("/data/options.json") {
            Ok(data) => serde_json::from_slice(&data)
                .map_err(|e| format!("failed to parse options.json: {e}"))?,
            Err(_) => Self::default(),
        };
        fn value(name: &str) -> Option<String> {
            env::var(name).ok().filter(|v| !v.is_empty())
        }
        if let Some(v) = value("UPSTREAM_HOST") {
            c.upstream_host = v;
        }
        if let Some(v) = value("UPSTREAM_PORT").and_then(|v| v.parse().ok()) {
            c.upstream_port = v;
        }
        if let Some(v) = value("LISTEN_PORT").and_then(|v| v.parse().ok()) {
            c.listen_port = v;
        }
        if let Some(v) = value("MAX_CLIENTS").and_then(|v| v.parse().ok()) {
            c.max_clients = v;
        }
        if let Some(v) = value("LOG_PACKETS") {
            c.log_packets = v == "true" || v == "1";
        }
        if let Some(v) = value("LOG_FILE") {
            c.log_file = v;
        }
        if let Some(v) = value("WEB_PORT").and_then(|v| v.parse().ok()) {
            c.web_port = v;
        }
        if let Some(v) = value("WEB_AUTH_ENABLED") {
            c.web_auth_enabled = v == "true" || v == "1";
        }
        if let Some(v) = value("WEB_AUTH_USERNAME") {
            c.web_auth_username = v;
        }
        if let Some(v) = value("WEB_AUTH_PASSWORD") {
            c.web_auth_password = v;
        }
        if c.upstream_host.is_empty() {
            return Err("UPSTREAM_HOST is required".into());
        }
        if !(1..=65535).contains(&c.upstream_port) {
            return Err(format!("invalid UPSTREAM_PORT: {}", c.upstream_port));
        }
        if !(1..=65535).contains(&c.listen_port) {
            return Err(format!("invalid LISTEN_PORT: {}", c.listen_port));
        }
        if !(1..=100).contains(&c.max_clients) {
            return Err("MAX_CLIENTS must be between 1 and 100".into());
        }
        if c.web_auth_enabled && c.web_auth_username.is_empty() {
            return Err("WEB_AUTH_USERNAME is required when WEB_AUTH_ENABLED is true".into());
        }
        if c.web_auth_enabled && c.web_auth_password.is_empty() {
            return Err("WEB_AUTH_PASSWORD is required when WEB_AUTH_ENABLED is true".into());
        }
        Ok(c)
    }
    pub fn upstream_addr(&self) -> String {
        format!("{}:{}", self.upstream_host, self.upstream_port)
    }
    pub fn listen_addr(&self) -> String {
        format!(":{}", self.listen_port)
    }
}
