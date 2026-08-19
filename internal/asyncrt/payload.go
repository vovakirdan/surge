package asyncrt

// Payload is the language value a channel, a task result or a wake carries.
//
// The runtime never inspects one — it moves payloads between queues and hands
// them back — so the constraint admits anything. What it buys is that the
// fields SAY so: `buf []Payload` names what the buffer holds, where `[]any`
// named nothing and let a receiver's type assertion be the first thing that
// noticed a wrong value. Declaring the constraint once also keeps the
// unconstrained spelling out of two dozen signatures.
type Payload interface{}

// TaskState is the runtime-opaque state a task carries between polls.
//
// It is NOT a language value: a sleep carries its delay, a timeout its deadline
// and a user task its suspension frame, and the runtime hands each back to the
// VM without ever looking inside. That is why it is a separate name from
// Payload rather than the same parameter — the two are opaque for different
// reasons, and collapsing them would say a channel can carry a sleep's state.
type TaskState interface{}
