use super::*;

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
pub(super) async fn events(AxumState(s): AxumState<Arc<State>>, headers: HeaderMap) -> Response {
    if !authenticated(&headers, &s) {
        return unauthorized();
    }
    let Some((id, web_id, logs, rx, cancel)) = s.subscribe(false, String::new()) else {
        return response(StatusCode::SERVICE_UNAVAILABLE, "Max clients reached\n");
    };
    let mut queue = VecDeque::new();
    queue.push_back(
        Event::default()
            .event("status")
            .data(s.status().to_string()),
    );
    for line in logs {
        queue.push_back(
            Event::default()
                .event("log")
                .data(line.trim_end_matches('\n')),
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
                tokio::select! { line=rx.recv()=> Event::default().event("log").data(line?.trim_end_matches('\n')), _=status.tick()=>Event::default().event("status").data(sub.state.status().to_string()), _=heartbeat.tick()=>Event::default().comment("heartbeat"), _=cancel.changed()=>return None }
            };
            Some((
                Ok::<_, std::convert::Infallible>(event),
                (sub, queue, rx, cancel, status, heartbeat),
            ))
        },
    );
    let mut r = Sse::new(events).into_response();
    r.headers_mut().insert(
        "cache-control",
        "no-cache, no-store, must-revalidate".parse().unwrap(),
    );
    r.headers_mut()
        .insert("x-accel-buffering", "no".parse().unwrap());
    r.headers_mut()
        .insert("x-content-type-options", "nosniff".parse().unwrap());
    r
}
pub(super) async fn websocket(AxumState(s): AxumState<Arc<State>>, request: Request) -> Response {
    let (mut parts, _) = request.into_parts();
    let headers = &parts.headers;
    if !authenticated(headers, &s) {
        return unauthorized();
    }
    let origin = header(headers, "origin");
    if !origin.is_empty()
        && origin
            .parse::<Uri>()
            .ok()
            .and_then(|uri| uri.authority().map(|a| a.as_str().to_ascii_lowercase()))
            != Some(header(headers, "host").to_ascii_lowercase())
    {
        return response(StatusCode::FORBIDDEN, "Forbidden\n");
    }
    let upgrade = match WebSocketUpgrade::from_request_parts(&mut parts, &()).await {
        Ok(upgrade) => upgrade.max_message_size(512),
        Err(error) => return error.into_response(),
    };
    let addr = parts
        .extensions
        .get::<axum::extract::ConnectInfo<std::net::SocketAddr>>()
        .map(|address| address.0.to_string())
        .unwrap_or_default();
    let Some((id, web_id, logs, mut rx, mut cancel)) = s.subscribe(true, addr) else {
        return response(StatusCode::SERVICE_UNAVAILABLE, "Max clients reached\n");
    };
    let sub = Subscription {
        state: s,
        id,
        web_id,
    };
    upgrade.on_upgrade(move |mut socket| async move {
        let send = |kind: &str, data: Value| Message::Text(json!({"type":kind,"data":data}).to_string().into());
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
                line = rx.recv() => match line { Some(line) => Message::Text(line.into()), None => break },
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
    }).into_response()
}
