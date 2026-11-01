use crate::proxy::State;
use base64::{Engine, engine::general_purpose::STANDARD};
use futures_util::stream;
use include_dir::{Dir, include_dir};
use serde_json::{Value, json};
use std::{collections::VecDeque, sync::Arc, time::Duration};
use tokio::time::{Instant, interval_at};
use topcoat::{
    Result,
    context::{Cx, app_context},
    router::{
        Body, Router, StatusCode,
        content::{
            sse::{Event, Sse},
            websocket::{Message, WebSocketUpgrade},
        },
        request::{self, Bytes, FromRequest},
        response::{IntoResponse, Response},
        route,
    },
};

static ASSETS: Dir<'_> = include_dir!("$CARGO_MANIFEST_DIR/../internal/web/static");
fn state(cx: &Cx) -> Arc<State> {
    app_context::<Arc<State>>(cx).clone()
}
fn header<'a>(cx: &'a Cx, name: &str) -> &'a str {
    request::headers(cx)
        .get(name)
        .and_then(|v| v.to_str().ok())
        .unwrap_or("")
}
fn cookie(cx: &Cx) -> &str {
    header(cx, "cookie")
        .split(';')
        .map(str::trim)
        .find_map(|p| p.strip_prefix("session_token="))
        .unwrap_or("")
}
fn authenticated(cx: &Cx, s: &State) -> bool {
    if !s.config.web_auth_enabled || s.session_valid(cookie(cx)) {
        return true;
    }
    let Some(raw) = header(cx, "authorization").strip_prefix("Basic ") else {
        return false;
    };
    let Ok(raw) = STANDARD.decode(raw) else {
        return false;
    };
    let raw = String::from_utf8_lossy(&raw);
    let (user, pass) = raw.split_once(':').unwrap_or((&raw, ""));
    credentials_match(s, user, pass)
}
fn credentials_match(s: &State, user: &str, pass: &str) -> bool {
    let user_ok = ct_eq(user.as_bytes(), s.config.web_auth_username.as_bytes());
    let pass_ok = ct_eq(pass.as_bytes(), s.config.web_auth_password.as_bytes());
    user_ok & pass_ok
}
fn ct_eq(a: &[u8], b: &[u8]) -> bool {
    a.len() == b.len() && a.iter().zip(b).fold(0u8, |acc, (x, y)| acc | (x ^ y)) == 0
}
fn response(code: StatusCode, text: impl Into<String>) -> Response {
    Response::builder()
        .status(code)
        .header("content-type", "text/plain; charset=utf-8")
        .body(Body::from(text.into()))
        .unwrap()
}
fn json_response(code: StatusCode, body: Value) -> Response {
    Response::builder()
        .status(code)
        .header("content-type", "application/json")
        .body(Body::from(format!("{body}\n")))
        .unwrap()
}
fn unauthorized() -> Response {
    response(StatusCode::UNAUTHORIZED, "Unauthorized\n")
}
fn asset(cx: &Cx) -> Response {
    let path = request::uri(cx).path();
    let path = if path == "/" {
        "index.html"
    } else {
        path.trim_start_matches('/')
    };
    let ct = if path.ends_with(".html") {
        "text/html; charset=utf-8"
    } else if path.ends_with(".js") {
        "text/javascript; charset=utf-8"
    } else if path.ends_with(".css") {
        "text/css; charset=utf-8"
    } else if path.ends_with(".png") {
        "image/png"
    } else {
        "application/octet-stream"
    };
    match ASSETS.get_file(path) {
        Some(f) => Response::builder()
            .header("content-type", ct)
            .body(Body::from(Bytes::from_static(f.contents())))
            .unwrap(),
        None => response(StatusCode::NOT_FOUND, "404 page not found\n"),
    }
}
#[route(GET "/")]
async fn root(cx: &Cx) -> Result<Response> {
    get(cx).await
}
#[route(GET "/{*path}")]
async fn all_get(cx: &Cx) -> Result<Response> {
    get(cx).await
}
async fn get(cx: &Cx) -> Result<Response> {
    let s = state(cx);
    let path = request::uri(cx).path();
    let protected = path.starts_with("/api/")
        && !matches!(
            path,
            "/api/health" | "/api/auth/check" | "/api/login" | "/api/logout"
        );
    if protected && !authenticated(cx, &s) {
        return Ok(unauthorized());
    }
    let c = &s.config;
    Ok(match path {
        "/api/health" => json_response(StatusCode::OK, s.health()),
        "/api/status" => json_response(StatusCode::OK, s.status()),
        "/api/config" => json_response(
            StatusCode::OK,
            json!({"upstream_host":c.upstream_host,"upstream_port":c.upstream_port,"listen_port":c.listen_port,"max_clients":c.max_clients,"log_packets":c.log_packets,"web_port":c.web_port}),
        ),
        "/api/clients" => json_response(StatusCode::OK, s.clients()),
        "/api/auth/check" => json_response(
            StatusCode::OK,
            json!({"authenticated":!c.web_auth_enabled || s.session_valid(cookie(cx)),"auth_enabled":c.web_auth_enabled}),
        ),
        "/api/login" | "/api/logout" | "/api/inject" | "/api/clients/disconnect" => {
            response(StatusCode::METHOD_NOT_ALLOWED, "Method not allowed\n")
        }
        _ if !matches!(path, "/login.html" | "/style.css" | "/favicon.png")
            && !authenticated(cx, &s) =>
        {
            Response::builder()
                .status(StatusCode::FOUND)
                .header("location", "/login.html")
                .body(Body::empty())
                .unwrap()
        }
        _ => asset(cx),
    })
}
#[route(POST "/{*path}")]
async fn post(cx: &Cx, body: Bytes) -> Result<Response> {
    let s = state(cx);
    let path = request::uri(cx).path();
    if header(cx, "content-type")
        .split(';')
        .next()
        .unwrap_or("")
        .trim()
        != "application/json"
    {
        return Ok(response(
            StatusCode::UNSUPPORTED_MEDIA_TYPE,
            "Unsupported Media Type\n",
        ));
    }
    if path.starts_with("/api/")
        && !matches!(
            path,
            "/api/login" | "/api/logout" | "/api/health" | "/api/auth/check"
        )
        && !authenticated(cx, &s)
    {
        return Ok(unauthorized());
    }
    if path == "/api/logout" {
        s.remove_session(cookie(cx));
        let mut r = json_response(StatusCode::OK, json!({"success":true}));
        r.headers_mut().insert(
            "set-cookie",
            "session_token=; Path=/; Max-Age=0; HttpOnly"
                .parse()
                .unwrap(),
        );
        return Ok(r);
    }
    if path == "/api/login" && !s.config.web_auth_enabled {
        return Ok(json_response(StatusCode::OK, json!({"success":true})));
    }
    if !matches!(
        path,
        "/api/login" | "/api/inject" | "/api/clients/disconnect"
    ) {
        return Ok(response(
            StatusCode::METHOD_NOT_ALLOWED,
            "Method not allowed\n",
        ));
    }
    let Ok(v) = serde_json::from_slice::<Value>(&body) else {
        return Ok(if path == "/api/login" {
            json_response(
                StatusCode::BAD_REQUEST,
                json!({"error":"Invalid request body"}),
            )
        } else {
            response(StatusCode::BAD_REQUEST, "Invalid JSON\n")
        });
    };
    let text = |key| v.get(key).and_then(Value::as_str).unwrap_or("");
    Ok(match path {
        "/api/login" => {
            if !credentials_match(&s, text("username"), text("password")) {
                json_response(
                    StatusCode::UNAUTHORIZED,
                    json!({"error":"Invalid username or password"}),
                )
            } else {
                let mut random = [0u8; 32];
                getrandom::fill(&mut random).map_err(|e| std::io::Error::other(e.to_string()))?;
                let token: String = random.iter().map(|b| format!("{b:02x}")).collect();
                s.add_session(token.clone());
                let mut r = json_response(StatusCode::OK, json!({"success":true}));
                r.headers_mut().insert(
                    "set-cookie",
                    format!("session_token={token}; Path=/; Max-Age=86400; HttpOnly; SameSite=Lax")
                        .parse()
                        .unwrap(),
                );
                r
            }
        }
        "/api/clients/disconnect" => {
            let id = text("client_id");
            if id.is_empty() {
                response(StatusCode::BAD_REQUEST, "client_id is required\n")
            } else if !s.disconnect(id) {
                response(StatusCode::NOT_FOUND, "Client not found\n")
            } else {
                json_response(StatusCode::OK, json!({"success":true}))
            }
        }
        _ => {
            let data = if text("format") == "hex" {
                let raw = text("data").replace([' ', '\n', '\r'], "");
                let raw = raw.strip_prefix("0x").unwrap_or(&raw);
                if raw.len() % 2 != 0 || !raw.bytes().all(|b| b.is_ascii_hexdigit()) {
                    return Ok(response(StatusCode::BAD_REQUEST, "Invalid Hex\n"));
                }
                (0..raw.len())
                    .step_by(2)
                    .map(|i| u8::from_str_radix(&raw[i..i + 2], 16).unwrap())
                    .collect::<Vec<_>>()
            } else {
                text("data").as_bytes().to_vec()
            };
            if s.inject(text("target"), &data).await {
                json_response(StatusCode::OK, json!({"success":true}))
            } else {
                response(StatusCode::INTERNAL_SERVER_ERROR, "Injection failed\n")
            }
        }
    })
}
struct Subscription {
    state: Arc<State>,
    id: u64,
    web_id: Option<String>,
}
impl Drop for Subscription {
    fn drop(&mut self) {
        self.state.unsubscribe(self.id, self.web_id.as_deref());
    }
}
#[route(GET "/api/events")]
async fn events(cx: &Cx) -> Result<Response> {
    let s = state(cx);
    if !authenticated(cx, &s) {
        return Ok(unauthorized());
    }
    let Some((id, web_id, logs, rx, cancel)) = s.subscribe(false, String::new()) else {
        return Ok(response(
            StatusCode::SERVICE_UNAVAILABLE,
            "Max clients reached\n",
        ));
    };
    let mut queue = VecDeque::new();
    queue.push_back(Event::new().event("status").data(s.status().to_string()));
    for line in logs {
        queue.push_back(
            Event::new()
                .event("log")
                .data(line.trim_end_matches('\n').to_string()),
        );
    }
    let sub = Subscription {
        state: s,
        id,
        web_id,
    };
    let status = interval_at(
        Instant::now() + Duration::from_secs(2),
        Duration::from_secs(2),
    );
    let heartbeat = interval_at(
        Instant::now() + Duration::from_secs(15),
        Duration::from_secs(15),
    );
    let events = stream::unfold(
        (sub, queue, rx, cancel, status, heartbeat),
        |(sub, mut queue, mut rx, mut cancel, mut status, mut heartbeat)| async move {
            let event = if let Some(e) = queue.pop_front() {
                e
            } else {
                tokio::select! { line=rx.recv()=> Event::new().event("log").data(line?.trim_end_matches('\n').to_string()), _=status.tick()=>Event::new().event("status").data(sub.state.status().to_string()), _=heartbeat.tick()=>Event::new().comment("heartbeat"), _=cancel.changed()=>return None }
            };
            Some((
                Ok::<_, topcoat::Error>(event),
                (sub, queue, rx, cancel, status, heartbeat),
            ))
        },
    );
    let mut r = Sse::new(events).into_response(cx)?;
    r.headers_mut().insert(
        "cache-control",
        "no-cache, no-store, must-revalidate".parse().unwrap(),
    );
    r.headers_mut()
        .insert("x-accel-buffering", "no".parse().unwrap());
    r.headers_mut()
        .insert("x-content-type-options", "nosniff".parse().unwrap());
    Ok(r)
}
#[route(GET "/api/ws")]
async fn websocket(cx: &Cx, body: Body) -> Result<Response> {
    let s = state(cx);
    if !authenticated(cx, &s) {
        return Ok(unauthorized());
    }
    let origin = header(cx, "origin");
    if !origin.is_empty() && origin.split_once("://").map(|(_, x)| x) != Some(header(cx, "host")) {
        return Ok(response(StatusCode::FORBIDDEN, "Forbidden\n"));
    }
    let upgrade = WebSocketUpgrade::from_request(cx, body)
        .await?
        .max_message_size(512);
    let addr = request::remote_addr(cx)
        .map(|a| a.to_string())
        .unwrap_or_default();
    let Some((id, web_id, logs, mut rx, mut cancel)) = s.subscribe(true, addr) else {
        return Ok(response(
            StatusCode::SERVICE_UNAVAILABLE,
            "Max clients reached\n",
        ));
    };
    let sub = Subscription {
        state: s,
        id,
        web_id,
    };
    upgrade.on_upgrade(move |mut socket| async move {
        let send = |kind: &str, data: Value| Message::text(json!({"type":kind,"data":data}).to_string());
        if socket.send(send("status", sub.state.status())).await.is_err() { return; }
        for line in logs {
            if socket.send(send("log", json!(line))).await.is_err() { return; }
        }
        let mut ticker = interval_at(Instant::now() + Duration::from_secs(2), Duration::from_secs(2));
        let mut ping = interval_at(Instant::now() + Duration::from_secs(30), Duration::from_secs(30));
        let deadline = tokio::time::sleep(Duration::from_secs(60));
        tokio::pin!(deadline);
        loop {
            let message = tokio::select! {
                line = rx.recv() => match line { Some(line) => Message::text(line), None => break },
                _ = ticker.tick() => send("status", sub.state.status()),
                _ = ping.tick() => Message::Ping(Bytes::new()),
                _ = cancel.changed() => break,
                _ = &mut deadline => break,
                incoming = socket.recv() => {
                    match incoming {
                        Some(Ok(Message::Pong(_))) => deadline.as_mut().reset(Instant::now() + Duration::from_secs(60)),
                        Some(Ok(Message::Close(_))) | Some(Err(_)) | None => break,
                        _ => {}
                    }
                    continue;
                }
            };
            if !matches!(tokio::time::timeout(Duration::from_secs(10), socket.send(message)).await, Ok(Ok(()))) { break; }
        }
    })
}
pub fn router(s: Arc<State>) -> Router {
    Router::builder()
        .app_context(s)
        .route(root)
        .route(all_get)
        .route(post)
        .route(events)
        .route(websocket)
        .build()
}
