# JSON v1 NOTES

## Limitations / behavior
- JsonObject key order is not preserved; stringify sorts keys lexicographically for deterministic output.
- Parser operates on full input and copies string input into a byte array.
- Recursion depth is unbounded; deeply nested JSON may overflow the stack.
- stringify returns an empty string if UTF-8 conversion fails (should not happen for valid JsonValue).
- Map-backed JsonObject and Map.keys are VM-only today; LLVM/native backends lack map intrinsics.

## Missing helpers / friction
- Needed a Map.keys helper to iterate object keys (added rt_map_keys + Map.keys).
- No fast string builder; escaping and appending use manual byte loops.
- No BytesView slicing helper; parser copies ranges to build number tokens.

## Tricky edges handled
- String escapes include \uXXXX with surrogate pair validation; invalid pairs return JSON_ERR_PARSE.
- Numbers are parsed per JSON grammar and stored as exact token text.

## Future improvements
- Streaming parser and incremental serializer for large payloads.
- Avoid copying by parsing from BytesView and reusing slices where possible.
- Add JsonDecodable once conversion contracts are ready.
