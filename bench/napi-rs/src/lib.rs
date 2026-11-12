//! The napi-rs implementation of the benchmark addon (bench/README.md): the same functions as the
//! napi-go and node-addon-api implementations, written the idiomatic way with #[napi].

use napi::bindgen_prelude::Buffer;
use napi_derive::napi;

/// Does nothing: the cost of a call.
#[napi]
pub fn noop() {}

/// Returns a + b.
#[napi]
pub fn add(a: f64, b: f64) -> f64 {
    a + b
}

/// Returns s: a string converted from JS and back.
#[napi]
pub fn echo(s: String) -> String {
    s
}

/// Returns the length of b, read in place.
#[napi]
pub fn buffer_length(b: &[u8]) -> u32 {
    b.len() as u32
}

/// Returns a new zero-filled Buffer of n bytes.
#[napi]
pub fn make_buffer(n: u32) -> Buffer {
    vec![0u8; n as usize].into()
}

/// Returns a Promise resolved with n by the async runtime of napi-rs (tokio).
#[napi]
pub async fn resolve_async(n: f64) -> f64 {
    n
}
