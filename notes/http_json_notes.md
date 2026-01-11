# HTTP JSON helpers notes (REST pt.2)

- Request JSON parsing uses `json.parse_bytes`; errors are mapped to HTTP 400 with message + offset.
- Content-Type is not enforced yet (no existing header lookup helper); consider adding a header accessor and an opt-in strictness flag.
- `BodyReader.read_all(limit)` enforces a per-call limit but does not prevent the initial buffering already done by the HTTP parser; a streaming parse path would avoid this copy.
- Limit value `0` is treated as "no additional limit" and still respects any server max-body limit for chunked requests.
- Typed JSON decoding (`JsonDecodable`) is still missing; `Request.json_value` returns `JsonValue` only.
- Contract-bound use of `JsonEncodable` currently triggers `SEM3043` (method attribute/modifier mismatch for `to_json`), so `Response.json` is implemented via overloads for built-in types + `JsonValue` instead of a generic bound.
