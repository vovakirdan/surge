// Package dialect provides lightweight detection for "foreign dialect" signals
// (Rust/Go/TypeScript/Python-ish) that can be used to emit extra diagnostics.
//
// It is intentionally non-invasive: evidence collection must never change parsing
// or semantic behavior, and hint diagnostics are always optional.
package dialect
