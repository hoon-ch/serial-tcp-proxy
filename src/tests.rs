use crate::{config::Config, proxy::State, web};
use axum::{
    body::{Body, to_bytes},
    http::{Request, StatusCode},
};
use base64::{Engine, engine::general_purpose::STANDARD};
use tokio::{
    io::{AsyncReadExt, AsyncWriteExt},
    net::{TcpListener, TcpStream},
    time::{Duration, timeout},
};
use tower::ServiceExt;

fn config() -> Config {
    Config {
        upstream_host: "127.0.0.1".into(),
        web_auth_enabled: true,
        web_auth_username: "user".into(),
        web_auth_password: "secret".into(),
        ..Config::default()
    }
}

async fn request(
    app: &axum::Router,
    method: &str,
    path: &str,
    content_type: Option<&str>,
    authorization: Option<&str>,
    cookie: Option<&str>,
    body: &str,
) -> axum::response::Response {
    let mut builder = Request::builder().method(method).uri(path);
    if let Some(v) = content_type {
        builder = builder.header("content-type", v);
    }
    if let Some(v) = authorization {
        builder = builder.header("authorization", v);
    }
    if let Some(v) = cookie {
        builder = builder.header("cookie", v);
    }
    app.clone()
        .oneshot(builder.body(Body::from(body.to_owned())).unwrap())
        .await
        .unwrap()
}

#[tokio::test(start_paused = true)]
async fn web_auth_session_validation_and_expiry() {
    let state = State::new(config());
    let app = web::router(state);
    let basic = format!("Basic {}", STANDARD.encode("user:secret"));
    assert_eq!(
        request(&app, "GET", "/api/status", None, None, None, "")
            .await
            .status(),
        StatusCode::UNAUTHORIZED
    );
    assert_eq!(
        request(&app, "GET", "/api/status", None, Some(&basic), None, "")
            .await
            .status(),
        StatusCode::OK
    );
    assert_eq!(
        request(
            &app,
            "POST",
            "/api/inject",
            Some("text/plain"),
            None,
            None,
            "{}"
        )
        .await
        .status(),
        StatusCode::UNSUPPORTED_MEDIA_TYPE
    );
    assert_eq!(
        request(
            &app,
            "POST",
            "/api/login",
            Some("application/json"),
            None,
            None,
            "{"
        )
        .await
        .status(),
        StatusCode::BAD_REQUEST
    );
    let login = request(
        &app,
        "POST",
        "/api/login",
        Some("application/json"),
        None,
        None,
        r#"{"username":"user","password":"secret"}"#,
    )
    .await;
    assert_eq!(login.status(), StatusCode::OK);
    let cookie = login.headers()["set-cookie"]
        .to_str()
        .unwrap()
        .split(';')
        .next()
        .unwrap()
        .to_owned();
    assert_eq!(
        request(&app, "GET", "/api/status", None, None, Some(&cookie), "")
            .await
            .status(),
        StatusCode::OK
    );
    let config_response = request(&app, "GET", "/api/config", None, None, Some(&cookie), "").await;
    let body = to_bytes(config_response.into_body(), 8192).await.unwrap();
    assert!(!String::from_utf8_lossy(&body).contains("secret"));
    assert_eq!(
        request(
            &app,
            "POST",
            "/api/inject",
            Some("application/json"),
            None,
            Some(&cookie),
            r#"{"target":"invalid","data":"x"}"#
        )
        .await
        .status(),
        StatusCode::INTERNAL_SERVER_ERROR
    );
    tokio::time::advance(Duration::from_secs(86400)).await;
    assert_eq!(
        request(&app, "GET", "/api/status", None, None, Some(&cookie), "")
            .await
            .status(),
        StatusCode::UNAUTHORIZED
    );
    let login = request(
        &app,
        "POST",
        "/api/login",
        Some("application/json"),
        None,
        None,
        r#"{"username":"user","password":"secret"}"#,
    )
    .await;
    let cookie = login.headers()["set-cookie"]
        .to_str()
        .unwrap()
        .split(';')
        .next()
        .unwrap()
        .to_owned();
    assert_eq!(
        request(
            &app,
            "POST",
            "/api/logout",
            Some("application/json"),
            None,
            Some(&cookie),
            "{}"
        )
        .await
        .status(),
        StatusCode::OK
    );
    assert_eq!(
        request(&app, "GET", "/api/status", None, None, Some(&cookie), "")
            .await
            .status(),
        StatusCode::UNAUTHORIZED
    );
}

#[tokio::test]
async fn websocket_origin_rule_on_listener() {
    let mut c = config();
    c.web_auth_enabled = false;
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let task = tokio::spawn(async move {
        axum::serve(listener, web::router(State::new(c)))
            .await
            .unwrap()
    });
    for (origin, expected) in [
        (Some("http://evil.example".to_string()), 403),
        (Some(format!("http://{addr}")), 101),
        (None, 101),
    ] {
        let mut stream = TcpStream::connect(addr).await.unwrap();
        let origin = origin
            .map(|x| format!("Origin: {x}\r\n"))
            .unwrap_or_default();
        stream.write_all(format!("GET /api/ws HTTP/1.1\r\nHost: {addr}\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n{origin}\r\n").as_bytes()).await.unwrap();
        let mut buf = [0u8; 512];
        let n = timeout(Duration::from_secs(1), stream.read(&mut buf))
            .await
            .unwrap()
            .unwrap();
        assert!(String::from_utf8_lossy(&buf[..n]).starts_with(&format!("HTTP/1.1 {expected}")));
    }
    task.abort();
}

#[tokio::test]
async fn proxy_forwarding_reconnect_limit_and_shutdown() {
    let upstream = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let clients = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let mut c = config();
    c.upstream_port = upstream.local_addr().unwrap().port() as i32;
    c.max_clients = 2;
    let state = State::new(c);
    let client_addr = clients.local_addr().unwrap();
    let listener_task = tokio::spawn(state.clone().run_listener(clients));
    let upstream_task = tokio::spawn(state.clone().run_upstream());
    let (mut up, _) = timeout(Duration::from_secs(2), upstream.accept())
        .await
        .unwrap()
        .unwrap();
    let mut a = TcpStream::connect(client_addr).await.unwrap();
    let mut b = TcpStream::connect(client_addr).await.unwrap();
    while state.clients()["tcp_count"] != 2 {
        tokio::task::yield_now().await;
    }
    let mut extra = TcpStream::connect(client_addr).await.unwrap();
    let mut buf = [0u8; 4];
    assert_eq!(
        timeout(Duration::from_secs(1), extra.read(&mut buf))
            .await
            .unwrap()
            .unwrap(),
        0
    );
    up.write_all(b"ping").await.unwrap();
    timeout(Duration::from_secs(1), a.read_exact(&mut buf))
        .await
        .unwrap()
        .unwrap();
    assert_eq!(&buf, b"ping");
    timeout(Duration::from_secs(1), b.read_exact(&mut buf))
        .await
        .unwrap()
        .unwrap();
    assert_eq!(&buf, b"ping");
    a.write_all(b"pong").await.unwrap();
    timeout(Duration::from_secs(1), up.read_exact(&mut buf))
        .await
        .unwrap()
        .unwrap();
    assert_eq!(&buf, b"pong");
    assert!(
        timeout(Duration::from_millis(50), b.read(&mut buf))
            .await
            .is_err()
    );
    drop(up);
    let (mut up, _) = timeout(Duration::from_secs(2), upstream.accept())
        .await
        .unwrap()
        .unwrap();
    up.write_all(b"back").await.unwrap();
    timeout(Duration::from_secs(1), a.read_exact(&mut buf))
        .await
        .unwrap()
        .unwrap();
    assert_eq!(&buf, b"back");
    timeout(Duration::from_secs(1), b.read_exact(&mut buf))
        .await
        .unwrap()
        .unwrap();
    timeout(Duration::from_secs(1), state.shutdown())
        .await
        .unwrap();
    assert_eq!(
        timeout(Duration::from_secs(1), a.read(&mut buf))
            .await
            .unwrap()
            .unwrap(),
        0
    );
    listener_task.abort();
    upstream_task.abort();
}
