use super::*;

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
pub(super) fn authenticated(headers: &HeaderMap, s: &State) -> bool {
    if !s.config.web_auth_enabled || s.session_valid(cookie(headers)) {
        return true;
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
