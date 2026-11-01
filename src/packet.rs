use chrono::{Local, SecondsFormat};

pub fn timestamp() -> String {
    Local::now().to_rfc3339_opts(SecondsFormat::Secs, true)
}

pub fn timestamp_nano() -> String {
    let now = Local::now();
    let prefix = now.format("%Y-%m-%dT%H:%M:%S").to_string();
    let nanos = now.timestamp_subsec_nanos();
    let fraction = if nanos == 0 {
        String::new()
    } else {
        format!(".{nanos:09}").trim_end_matches('0').to_owned()
    };
    let offset = now.format("%:z").to_string();
    format!(
        "{prefix}{fraction}{}",
        if offset == "+00:00" { "Z" } else { &offset }
    )
}

pub fn hex_spaced(data: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut out = Vec::with_capacity(data.len().saturating_mul(3).saturating_sub(1));
    for (i, b) in data.iter().enumerate() {
        if i > 0 {
            out.push(b' ');
        }
        out.extend_from_slice(&[DIGITS[(b >> 4) as usize], DIGITS[(b & 0x0f) as usize]]);
    }
    String::from_utf8(out).unwrap()
}
