mod config;
mod proxy;
mod web;

use config::Config;
use proxy::State;
use std::time::Duration;
use tokio::net::TcpListener;

fn listener(port: u16) -> std::io::Result<TcpListener> {
    let ipv6 = || -> std::io::Result<TcpListener> {
        let socket = socket2::Socket::new(
            socket2::Domain::IPV6,
            socket2::Type::STREAM,
            Some(socket2::Protocol::TCP),
        )?;
        socket.set_only_v6(false)?;
        socket.set_reuse_address(true)?;
        socket.set_nonblocking(true)?;
        socket.bind(&std::net::SocketAddr::from(([0u16; 8], port)).into())?;
        socket.listen(1024)?;
        TcpListener::from_std(socket.into())
    };
    ipv6().or_else(|_| {
        let socket = std::net::TcpListener::bind(("0.0.0.0", port))?;
        socket.set_nonblocking(true)?;
        TcpListener::from_std(socket)
    })
}

#[cfg(feature = "mimalloc")]
#[global_allocator]
static GLOBAL: mimalloc::MiMalloc = mimalloc::MiMalloc;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Register before any port opens: a SIGTERM that lands while tokio installs
    // its handler is lost, and a client that saw the port could send one then.
    #[cfg(unix)]
    let mut term = tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())?;
    let config = match Config::load() {
        Ok(config) => config,
        Err(e) => {
            eprintln!("Configuration error: {e}");
            std::process::exit(1);
        }
    };
    let state = State::new(config.clone());
    state.info("INFO", "Starting Serial TCP Proxy vdev");
    state.info("INFO", &format!("Upstream: {}", config.upstream_addr()));
    state.info("INFO", &format!("Listen: {}", config.listen_addr()));
    state.info("INFO", &format!("Max clients: {}", config.max_clients));
    state.info("INFO", &format!("Packet logging: {}", config.log_packets));
    let tcp = listener(config.listen_port as u16)?;
    state.info("INFO", &format!("Listening on {}", config.listen_addr()));
    let listener_task = tokio::spawn(state.clone().run_listener(tcp));
    let upstream_task = tokio::spawn(state.clone().run_upstream());
    let flush_task = tokio::spawn(state.clone().flush_loop());
    if let Ok(web) = listener(config.web_port as u16) {
        state.info(
            "INFO",
            &format!("Web UI listening on http://localhost:{}", config.web_port),
        );
        let web_state = state.clone();
        tokio::spawn(async move {
            let _ = topcoat::serve(web, web::router(web_state)).await;
        });
    }
    #[cfg(unix)]
    tokio::select! { _=tokio::signal::ctrl_c()=>{}, _=term.recv()=>{} }
    #[cfg(not(unix))]
    tokio::signal::ctrl_c().await?;
    listener_task.abort();
    let _ = tokio::time::timeout(Duration::from_secs(5), state.shutdown()).await;
    upstream_task.abort();
    flush_task.abort();
    Ok(())
}
