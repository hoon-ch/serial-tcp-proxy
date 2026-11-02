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
pub(super) async fn events(
    AxumState(s): AxumState<Arc<State>>,
    caller: Caller,
    headers: HeaderMap,
    method: Method,
    uri: Uri,
) -> Response {
    if !authenticated(&headers, &s, &caller, true) {
        return unauthorized(&s, &caller, method.as_str(), uri.path());
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
        .insert("pragma", "no-cache".parse().unwrap());
    r.headers_mut().insert("expires", "0".parse().unwrap());
    r.headers_mut()
        .insert("x-accel-buffering", "no".parse().unwrap());
    r.headers_mut()
        .insert("x-content-type-options", "nosniff".parse().unwrap());
    r
}
pub(super) async fn websocket(
    AxumState(s): AxumState<Arc<State>>,
    caller: Caller,
    request: Request,
) -> Response {
    let (mut parts, _) = request.into_parts();
    let headers = &parts.headers;
    if !authenticated(headers, &s, &caller, true) {
        return unauthorized(&s, &caller, parts.method.as_str(), parts.uri.path());
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
    let addr = caller.addr();
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
        if !deliver(&mut socket, &mut cancel, send("status", sub.state.status())).await { return; }
        for line in logs {
            if !deliver(&mut socket, &mut cancel, send("log", json!(line))).await { return; }
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
            if !deliver(&mut socket, &mut cancel, message).await { break; }
        }
    }).into_response()
}

// Every send, including the log replay, gets Go's 10 s write deadline and stops on
// disconnect, so a client that stops reading cannot pin its slot.
async fn deliver(
    socket: &mut axum::extract::ws::WebSocket,
    cancel: &mut tokio::sync::watch::Receiver<bool>,
    message: Message,
) -> bool {
    tokio::select! {
        sent = tokio::time::timeout(Duration::from_secs(10), socket.send(message)) => matches!(sent, Ok(Ok(()))),
        _ = cancel.changed() => false,
    }
}
