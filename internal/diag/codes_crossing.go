package diag

// explicit-crossing language diagnostics (far / on / spawn on / crosses
// and the shard-mobility attributes). These codes were reserved during the
// test-preparation pass ahead of the parser/sema implementation.
//
// They live in this sibling file rather than codes.go so that codes.go stays
// within its file-size ceiling (check_file_sizes.sh). Code.ID() is range-based
// (SEM 3xxx, SYN 2xxx, FUT 7xxx), so these render with the correct prefix
// without any change there. The init() below merges the descriptions into the
// shared codeDescription map defined in codes.go.
//
// The placeholder -> code mapping is documented in
// docs/runtime-v2-epics/11-tasks/README.md. One code per invariant: Block 4
// owns the shared crosses/capture families; Blocks 2 and 3 reference them.

const (
	// --- Block 1: `far` type modifier (SEM 3136-3142) ---

	// SemaFarNested rejects nested remote handles (`far far T`).
	SemaFarNested Code = 3136
	// SemaFarRemoteOwn rejects `far own T` (remote ownership).
	SemaFarRemoteOwn Code = 3137
	// SemaFarRemoteBorrow rejects `far &T` / `far &mut T` (remote borrowed lifetimes).
	SemaFarRemoteBorrow Code = 3138
	// SemaFarExternTarget rejects `far extern<T>` as a value capability.
	SemaFarExternTarget Code = 3139
	// SemaFarGroupingUnsupported rejects grouped type forms that would change `far` precedence.
	SemaFarGroupingUnsupported Code = 3140
	// SemaFarNonCapability rejects `far` over a non-remote-handle-capable base type.
	SemaFarNonCapability Code = 3141
	// SemaFarLocalOp rejects local operations on `far T` outside an accepted crossing context.
	SemaFarLocalOp Code = 3142

	// --- Block 2: `on dst { ... }` placement crossing (SEM 3143-3153) ---

	// SemaOnDestFarTask rejects `far Task<T>` used as an `on` destination.
	SemaOnDestFarTask Code = 3143
	// SemaOnDestNotPlacement rejects an `on` destination that is not `Placement` or an accepted far handle.
	SemaOnDestNotPlacement Code = 3144
	// SemaOnDestTypeName rejects a type name used as an `on` destination.
	SemaOnDestTypeName Code = 3145
	// SemaOnDestBareFn rejects a bare function name used as an `on` destination.
	SemaOnDestBareFn Code = 3146
	// SemaOnBodyReturn rejects `return` escaping through an `on` crossing block.
	SemaOnBodyReturn Code = 3147
	// SemaOnBodyMissingRet rejects an `on` value block that never produces its result with `ret`.
	SemaOnBodyMissingRet Code = 3148
	// SemaOnResultTaskResult rejects treating the `on` result as `T` instead of `TaskResult<T>`.
	SemaOnResultTaskResult Code = 3149
	// SemaOnAnchorUnproven rejects acting through a far handle not anchored by the `on` destination.
	SemaOnAnchorUnproven Code = 3150
	// SemaOnTcpRemoteIO rejects remote socket I/O through `far TcpConn`.
	SemaOnTcpRemoteIO Code = 3151
	// SemaOnSuspendContext rejects `on` where suspension is not legal.
	SemaOnSuspendContext Code = 3152
	// SemaOnNested rejects nested `on` crossing blocks.
	SemaOnNested Code = 3153

	// --- Block 3: `spawn on dst { ... }` remote spawn (SEM 3154-3161) ---

	// SemaSpawnOnDestNotPlacement rejects a `spawn on` destination that is not `Placement`.
	SemaSpawnOnDestNotPlacement Code = 3154
	// SemaSpawnOnDestTypeName rejects a type name used as a `spawn on` destination.
	SemaSpawnOnDestTypeName Code = 3155
	// SemaSpawnOnDestBareFn rejects a bare function name used as a `spawn on` destination.
	SemaSpawnOnDestBareFn Code = 3156
	// SemaSpawnOnDestFarHandle rejects a far handle destination for `spawn on`.
	SemaSpawnOnDestFarHandle Code = 3157
	// SemaSpawnOnDestFarTask rejects `far Task<T>` used as a `spawn on` destination.
	SemaSpawnOnDestFarTask Code = 3158
	// SemaSpawnOnBodyReturn rejects `return` escaping through a `spawn on` block.
	SemaSpawnOnBodyReturn Code = 3159
	// SemaSpawnOnBodyMissingRet rejects a `spawn on` block that never produces its result with `ret`.
	SemaSpawnOnBodyMissingRet Code = 3160
	// SemaSpawnOnUnreachableAfterRet rejects unreachable statements after `ret` in a `spawn on` block.
	SemaSpawnOnUnreachableAfterRet Code = 3161

	// --- Block 4: crossing contracts, SEM (3162-3174) ---

	// SemaCrossesMissing (RETIRED): the explicit `crosses` requirement was dropped
	// when the `crosses` grammar was removed — the effect is inferred, not demanded.
	// Number reserved, never reuse.
	SemaCrossesMissing Code = 3162
	// SemaCrossesCallerMissing (RETIRED): `crosses` call-propagation requirement
	// dropped with the `crosses` grammar removal. Number reserved, never reuse.
	SemaCrossesCallerMissing Code = 3163
	// SemaFarTaskCrossesMissing (RETIRED): `far Task<T>` await/cancel now infer
	// the crossing effect. Number reserved, never reuse.
	SemaFarTaskCrossesMissing Code = 3164
	// SemaCrossBorrowCapture rejects a borrowed value captured into a crossing boundary.
	SemaCrossBorrowCapture Code = 3165
	// SemaCrossNosendCapture rejects a `@nosend` value crossing outside `@local spawn`.
	SemaCrossNosendCapture Code = 3166
	// SemaCrossPinnedCapture rejects a `@shard_pinned` value crossing as `own T`.
	SemaCrossPinnedCapture Code = 3167
	// SemaCrossNotShardMovable rejects an unmarked owned user value crossing as `own T`.
	SemaCrossNotShardMovable Code = 3168
	// SemaShardMovableSendInsufficient rejects `@send`-only user types crossing as `own T`.
	SemaShardMovableSendInsufficient Code = 3169
	// SemaShardMovableCopyInsufficient rejects `@copy`-only user types crossing as `own T`.
	SemaShardMovableCopyInsufficient Code = 3170
	// SemaShardMovableField rejects a `@shard_movable` type with a non-shard-movable field.
	SemaShardMovableField Code = 3171
	// SemaShardAttrConflict rejects `@shard_movable` and `@shard_pinned` on one type.
	SemaShardAttrConflict Code = 3172
	// SemaCrossesAttribute rejects `@crosses` used as an attribute.
	SemaCrossesAttribute Code = 3173
	// SemaLocalSpawnOn rejects `@local spawn on`.
	SemaLocalSpawnOn Code = 3174
	// SemaOnChannelOp rejects an anchored channel operation outside the supported send/recv/close surface.
	SemaOnChannelOp Code = 3175

	// --- Parse-level crossing diagnostics (SYN 2031-2036) ---

	// SynFarReservedIdent rejects `far` used as an identifier once it is a keyword.
	SynFarReservedIdent Code = 2031
	// SynSpawnOnMissingBlock rejects `spawn on dst` without a `{ ret expr; }` block.
	SynSpawnOnMissingBlock Code = 2032
	// SynSpawnOnMissingDestination rejects `spawn on { ... }` without a destination.
	SynSpawnOnMissingDestination Code = 2033
	// SynCrossesPlacement (RETIRED): the `crosses` grammar was removed, so its
	// placement can no longer be misplaced. Number reserved, never reuse.
	SynCrossesPlacement Code = 2034
	// SynCrossesTarget (RETIRED): `crosses` is no longer a function modifier.
	// Number reserved, never reuse.
	SynCrossesTarget Code = 2035
	// SynCrossesFnType (RETIRED): `crosses fn(...)` type syntax removed with the
	// `crosses` grammar. Number reserved, never reuse.
	SynCrossesFnType Code = 2036

	// --- Postponed / backend-unavailable crossing surfaces (FUT 7009-7017) ---

	// FutFarArrayPostponed marks `far T[]` / `far T[N]` remote array handles as postponed.
	FutFarArrayPostponed Code = 7009
	// FutFarLocalArrayPostponed marks local arrays of `far` handles as postponed.
	FutFarLocalArrayPostponed Code = 7010
	// FutFarFnHandle marks `far fn(...) -> T` remote function handles as postponed.
	FutFarFnHandle Code = 7011
	// FutOnDestBlocking marks `on blocking` as postponed.
	FutOnDestBlocking Code = 7012
	// FutSpawnOnDestBlocking marks `spawn on blocking` as postponed.
	FutSpawnOnDestBlocking Code = 7013
	// FutOnBackendUnavailable marks `on` placement crossing as unavailable in this backend/configuration.
	FutOnBackendUnavailable Code = 7014
	// FutSpawnOnBackendUnavailable marks `spawn on` as unavailable in this backend/configuration.
	FutSpawnOnBackendUnavailable Code = 7015
	// FutFarTaskAwaitBackendUnavailable marks `far Task<T>.await()` as unavailable in this backend/configuration.
	FutFarTaskAwaitBackendUnavailable Code = 7016
	// FutFarTaskCancelBackendUnavailable marks `far Task<T>.cancel()` as unavailable in this backend/configuration.
	FutFarTaskCancelBackendUnavailable Code = 7017
	// FutChannelOnBackendUnavailable marks `channel_on(...)` as unavailable in this backend/configuration.
	FutChannelOnBackendUnavailable Code = 7018
)

// crossingCodeDescriptions holds the human-readable titles for the
// crossing diagnostics. Merged into codeDescription by init() so Title()/String()
// resolve them like any other code. Messages name the invariant, matching the
// message shapes recorded in the block matrices.
var crossingCodeDescriptions = map[Code]string{
	SemaFarNested:              "nested `far` handles are not allowed",
	SemaFarRemoteOwn:           "`far own T` is invalid; move `own T` through `on` or `spawn on`",
	SemaFarRemoteBorrow:        "`far &T` and `far &mut T` are invalid remote lifetimes",
	SemaFarExternTarget:        "`extern<T>` is not a value capability for `far`",
	SemaFarGroupingUnsupported: "grouping does not change `far` array precedence",
	SemaFarNonCapability:       "`far` requires a remote-handle-capable type",
	SemaFarLocalOp:             "operation on `far T` requires an accepted remote context",

	SemaOnDestFarTask:      "`far Task<T>` is not an `on` destination; use `await()` or `cancel()`",
	SemaOnDestNotPlacement: "`on` destination must be a `Placement` value or an accepted `far` handle",
	SemaOnDestTypeName:     "a type name is not a placement target",
	SemaOnDestBareFn:       "a function value is not a placement target; call it if it returns `Placement`",
	SemaOnBodyReturn:       "`return` cannot exit through a crossing block; use `ret`",
	SemaOnBodyMissingRet:   "an `on` crossing block must produce its value with `ret`",
	SemaOnResultTaskResult: "`on` evaluates to `TaskResult<T>`, not `T`",
	SemaOnAnchorUnproven:   "this remote handle is not anchored by the current `on` destination",
	SemaOnTcpRemoteIO:      "remote socket I/O through `far TcpConn` is not supported yet",
	SemaOnSuspendContext:   "`on` is allowed only where suspension is legal",
	SemaOnNested:           "nested `on` crossing blocks are not allowed",

	SemaSpawnOnDestNotPlacement:    "`spawn on` destination must be a `Placement` value",
	SemaSpawnOnDestTypeName:        "a type name is not a `spawn on` placement target",
	SemaSpawnOnDestBareFn:          "a bare function name is not a `spawn on` placement target; call it if it returns `Placement`",
	SemaSpawnOnDestFarHandle:       "`far` handle destinations are invalid for `spawn on`; use `on` for immediate handle operations",
	SemaSpawnOnDestFarTask:         "`far Task<T>` is not a `spawn on` destination; use `await()` or `cancel()`",
	SemaSpawnOnBodyReturn:          "`return` cannot exit through a `spawn on` block; use `ret`",
	SemaSpawnOnBodyMissingRet:      "a `spawn on` block must produce its result with `ret`",
	SemaSpawnOnUnreachableAfterRet: "unreachable statement after `ret` in a `spawn on` block",

	SemaCrossesMissing:               "retired diagnostic: crossing effects are inferred",
	SemaCrossesCallerMissing:         "retired diagnostic: crossing call effects are inferred",
	SemaFarTaskCrossesMissing:        "retired diagnostic: remote task operations infer crossing effects",
	SemaCrossBorrowCapture:           "borrowed values cannot cross shard boundaries",
	SemaCrossNosendCapture:           "`@nosend` values cannot cross task or shard boundaries outside `@local spawn`",
	SemaCrossPinnedCapture:           "this operation would move a shard-pinned resource; use a far handle or explicit migration",
	SemaCrossNotShardMovable:         "owned user-defined values must be `@shard_movable` to cross shard boundaries",
	SemaShardMovableSendInsufficient: "`@send` is not sufficient for shard movement; add `@shard_movable`",
	SemaShardMovableCopyInsufficient: "`@copy` is not sufficient for shard movement; add `@shard_movable`",
	SemaShardMovableField:            "a `@shard_movable` type must have only shard-movable fields",
	SemaShardAttrConflict:            "`@shard_movable` conflicts with `@shard_pinned`",
	SemaCrossesAttribute:             "`@crosses` is not supported; crossing effects are inferred",
	SemaLocalSpawnOn:                 "`@local spawn on` is invalid; local spawn and remote placement are mutually exclusive",
	SemaOnChannelOp:                  "Unsupported anchored channel operation",

	SynFarReservedIdent:          "`far` is a reserved keyword; rename this identifier",
	SynSpawnOnMissingBlock:       "`spawn on` requires a `{ ret expr; }` block",
	SynSpawnOnMissingDestination: "`spawn on` requires a `Placement` destination",
	SynCrossesPlacement:          "retired diagnostic: `crosses` is not a function modifier",
	SynCrossesTarget:             "retired diagnostic: `crosses` is not a function modifier",
	SynCrossesFnType:             "`crosses fn(...)` function types are not part of the language",

	FutFarArrayPostponed:               "array types cannot be used as `far` remote handles yet",
	FutFarLocalArrayPostponed:          "local arrays of `far` handles are not supported yet",
	FutFarFnHandle:                     "function types cannot be used as `far` remote handles yet",
	FutOnDestBlocking:                  "`on blocking` is not a valid crossing destination",
	FutSpawnOnDestBlocking:             "`spawn on blocking` is not a valid placement destination",
	FutOnBackendUnavailable:            "`on` placement crossing cannot be executed: no available backend supports cross-shard transport",
	FutSpawnOnBackendUnavailable:       "`spawn on` remote spawn cannot be executed: no available backend supports cross-shard transport",
	FutFarTaskAwaitBackendUnavailable:  "`far Task<T>.await()` cannot be executed: no available backend supports remote task transport",
	FutFarTaskCancelBackendUnavailable: "`far Task<T>.cancel()` cannot be executed: no available backend supports remote task transport",
	FutChannelOnBackendUnavailable:     "`channel_on(...)` cannot be executed: this backend has no remote channel transport",
}

func init() {
	for c, d := range crossingCodeDescriptions {
		codeDescription[c] = d
	}
}
