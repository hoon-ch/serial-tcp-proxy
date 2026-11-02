use super::*;

use std::net::{IpAddr, Ipv4Addr, SocketAddr};

// Supervisor has already authenticated requests forwarded through ingress.
const SUPERVISOR_INGRESS_IP: IpAddr = IpAddr::V4(Ipv4Addr::new(172, 30, 32, 2));

pub(super) struct Caller(Option<SocketAddr>);

impl<S: Send + Sync> FromRequestParts<S> for Caller {
    type Rejection = std::convert::Infallible;

    async fn from_request_parts(
        parts: &mut axum::http::request::Parts,
        state: &S,
    ) -> Result<Self, Self::Rejection> {
        Ok(Self(
            axum::extract::ConnectInfo::<SocketAddr>::from_request_parts(parts, state)
                .await
                .ok()
                .map(|info| info.0),
        ))
    }
}

impl Caller {
    fn is_ingress(&self) -> bool {
        self.0
            .is_some_and(|address| address.ip().to_canonical() == SUPERVISOR_INGRESS_IP)
    }
}

pub(super) fn header<'a>(headers: &'a HeaderMap, name: &str) -> &'a str {
    headers
        .get(name)
        .and_then(|v| v.to_str().ok())
        .unwrap_or("")
}
pub(super) fn cookie(headers: &HeaderMap) -> &str {
    header(headers, "cookie")
        .split(';')
        .map(str::trim)
        .find_map(|p| p.strip_prefix("session_token="))
        .unwrap_or("")
}
pub(super) fn authenticated(
    headers: &HeaderMap,
    s: &State,
    caller: &Caller,
    allow_basic: bool,
) -> bool {
    if !s.config.web_auth_enabled || caller.is_ingress() || s.session_valid(cookie(headers)) {
        return true;
    }
    if !allow_basic {
        return false;
    }
    let Some(raw) = header(headers, "authorization").strip_prefix("Basic ") else {
        return false;
    };
    let Ok(raw) = STANDARD.decode(raw) else {
        return false;
    };
    let raw = String::from_utf8_lossy(&raw);
    let (user, pass) = raw.split_once(':').unwrap_or((&raw, ""));
    credentials_match(s, user, pass)
}
pub(super) fn credentials_match(s: &State, user: &str, pass: &str) -> bool {
    let user_ok = ct_eq(user.as_bytes(), s.config.web_auth_username.as_bytes());
    let pass_ok = ct_eq(pass.as_bytes(), s.config.web_auth_password.as_bytes());
    user_ok & pass_ok
}
fn ct_eq(a: &[u8], b: &[u8]) -> bool {
    bool::from(a.ct_eq(b))
}
