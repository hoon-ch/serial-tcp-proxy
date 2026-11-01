use super::*;

static ASSETS: Dir<'_> = include_dir!("$CARGO_MANIFEST_DIR/internal/web/static");
pub(super) fn asset(uri: &Uri) -> Response {
    let path = uri.path();
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
