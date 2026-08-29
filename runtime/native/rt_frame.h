#ifndef SURGE_RUNTIME_NATIVE_RT_FRAME_H
#define SURGE_RUNTIME_NATIVE_RT_FRAME_H

#include "rt_typed_carrier_abi.generated.h"

// A suspension frame: the storage a paused computation lives in while the
// RUNTIME holds it, past the return of the function that built it.
//
// Its allocation and its release used to be stated by two authorities. Compiled
// code took the block from rt_alloc at a size the emitter wrote into the call,
// gave it back through a per-type entry point that wrote that size a second
// time, and the runtime's own reclamation asked the type's descriptor instead.
// One authority now: the descriptor states the width and the alignment, and
// both ends of the frame's life read it from there.

// The two lifecycle words a frame's field 0 may hold.
//
// Field 0 sits at offset zero of all three frame kinds -- the async state
// machine's frame, a `spawn on` capture set, a `blocking` capture set -- so a
// reader holding only the frame's address finds the word without knowing which
// kind it got. The compiler side of this pairing is FrameStateField in
// internal/mir/suspension_frame.go, and the two spellings are checked against
// each other rather than trusted.
//
// Neither value is zero, because fresh storage and released storage both tend
// to read as zero and a check that could not tell those from PACKED would wave
// through exactly the frame it exists to catch.
//
// The field is a Surge `int`, so the word arrives fixnum-tagged; rt_frame.c
// decodes it rather than comparing raw bytes.
#define SURGE_FRAME_STATE_PACKED 0x5041434B // "PACK": the frame holds a suspension's live locals.
#define SURGE_FRAME_STATE_SPENT 0x53504E54  // "SPNT": nothing in the frame owns anything.

// Reserves one frame at the width and alignment `ops` states, zero-filled.
//
// It answers NULL when the allocator refuses and reports nothing itself. The
// caller is generated code, which tests the answer and stops the process naming
// the TYPE whose storage was refused (owner ruling 2026-08-28) -- and the type
// is the one fact the person reading that line can act on, which only the
// caller has. A NULL descriptor answers NULL for the same reason it allocates
// nothing: there is no width to reserve.
void* rt_frame_alloc(const rt_value_ops* ops);

// Gives one frame back, deciding from the FRAME what that means.
//
// PACKED: the frame is the only owner of what it holds -- a yield packs the
// live locals and abandons it -- so the members are destroyed before the
// storage goes back. SPENT: a poll took those locals out of the frame and the
// bytes left behind are a bitwise duplicate of values the locals now own, so
// walking them would free a string, a task handle or a channel a second time;
// the storage alone goes back.
//
// The answer used to belong to the SITE giving the frame up, recorded in prose
// beside each one. It is here because more than one site does, and a
// reclamation that arrives holding only an address cannot be told from outside
// what it is reclaiming.
void rt_frame_release(const rt_value_ops* ops, void* frame);

#endif
