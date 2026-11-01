mod auth;
mod static_files;
mod streams;

use auth::{authenticated, cookie, credentials_match, header};
use static_files::asset;
use streams::{events, websocket};

use crate::proxy::State;
use axum::{
    Router,
    body::{Body, Bytes},
    extract::{
        FromRequestParts, Request, State as AxumState,
        ws::{Message, WebSocketUpgrade},
    },
    http::{HeaderMap, StatusCode, Uri},
    response::{
        IntoResponse, Response,
        sse::{Event, Sse},
    },
    routing::get,
};
use base64::{Engine, engine::general_purpose::STANDARD};
use futures_util::stream;
use include_dir::{Dir, include_dir};
use serde_json::{Value, json};
use std::{collections::VecDeque, sync::Arc, time::Duration};
use subtle::ConstantTimeEq;
use tokio::time::{Instant, interval_at};

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
async fn handle_get(AxumState(s): AxumState<Arc<State>>, headers: HeaderMap, uri: Uri) -> Response {
    let headers = &headers;
    let path = uri.path();
    let protected = path.starts_with("/api/")
        && !matches!(
            path,
            "/api/health" | "/api/auth/check" | "/api/login" | "/api/logout"
        );
    if protected && !authenticated(headers, &s) {
        return unauthorized();
    }
    let c = &s.config;
    match path {
        "/api/health" => json_response(StatusCode::OK, s.health()),
        "/api/status" => json_response(StatusCode::OK, s.status()),
        "/api/config" => json_response(
            StatusCode::OK,
            json!({"upstream_host":c.upstream_host,"upstream_port":c.upstream_port,"listen_port":c.listen_port,"max_clients":c.max_clients,"log_packets":c.log_packets,"web_port":c.web_port}),
        ),
        "/api/clients" => json_response(StatusCode::OK, s.clients()),
        "/api/auth/check" => json_response(
            StatusCode::OK,
            json!({"authenticated":!c.web_auth_enabled || s.session_valid(cookie(headers)),"auth_enabled":c.web_auth_enabled}),
        ),
        "/api/login" | "/api/logout" | "/api/inject" | "/api/clients/disconnect" => {
            response(StatusCode::METHOD_NOT_ALLOWED, "Method not allowed\n")
        }
        _ if !matches!(path, "/login.html" | "/style.css" | "/favicon.png")
            && !authenticated(headers, &s) =>
        {
            Response::builder()
                .status(StatusCode::FOUND)
                .header("location", "/login.html")
                .body(Body::empty())
                .unwrap()
        }
        _ => asset(&uri),
    }
}
async fn handle_post(
    AxumState(s): AxumState<Arc<State>>,
    headers: HeaderMap,
    uri: Uri,
    body: Bytes,
) -> Response {
    let headers = &headers;
    let path = uri.path();
    if header(headers, "content-type")
        .split(';')
        .next()
        .unwrap_or("")
        .trim()
        != "application/json"
    {
        return response(
            StatusCode::UNSUPPORTED_MEDIA_TYPE,
            "Unsupported Media Type\n",
        );
    }
    if path.starts_with("/api/")
        && !matches!(
            path,
            "/api/login" | "/api/logout" | "/api/health" | "/api/auth/check"
        )
        && !authenticated(headers, &s)
    {
        return unauthorized();
    }
    if path == "/api/logout" {
        s.remove_session(cookie(headers));
        let mut r = json_response(StatusCode::OK, json!({"success":true}));
        r.headers_mut().insert(
            "set-cookie",
            "session_token=; Path=/; Max-Age=0; HttpOnly"
                .parse()
                .unwrap(),
        );
        return r;
    }
    if path == "/api/login" && !s.config.web_auth_enabled {
        return json_response(StatusCode::OK, json!({"success":true}));
    }
    if !matches!(
        path,
        "/api/login" | "/api/inject" | "/api/clients/disconnect"
    ) {
        return response(StatusCode::METHOD_NOT_ALLOWED, "Method not allowed\n");
    }
    let Ok(v) = serde_json::from_slice::<Value>(&body) else {
        return if path == "/api/login" {
            json_response(
                StatusCode::BAD_REQUEST,
                json!({"error":"Invalid request body"}),
            )
        } else {
            response(StatusCode::BAD_REQUEST, "Invalid JSON\n")
        };
    };
    let text = |key| v.get(key).and_then(Value::as_str).unwrap_or("");
    match path {
        "/api/login" => {
            if !credentials_match(&s, text("username"), text("password")) {
                json_response(
                    StatusCode::UNAUTHORIZED,
                    json!({"error":"Invalid username or password"}),
                )
            } else {
                let mut random = [0u8; 32];
                if getrandom::fill(&mut random).is_err() {
                    return response(StatusCode::INTERNAL_SERVER_ERROR, "Internal Server Error\n");
                }
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
                    return response(StatusCode::BAD_REQUEST, "Invalid Hex\n");
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
    }
}
pub fn router(s: Arc<State>) -> Router {
    Router::new()
        .route("/api/events", get(events).post(handle_post))
        .route("/api/ws", get(websocket).post(handle_post))
        .route("/", get(handle_get).post(handle_post))
        .route("/{*path}", get(handle_get).post(handle_post))
        .with_state(s)
}
