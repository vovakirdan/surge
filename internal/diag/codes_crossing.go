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
	// --- Block 1: `far` type modifier (SEM 3188-3194) ---

	// SemaFarNested rejects nested remote handles (`far far T`).
	SemaFarNested Code = 3188
	// SemaFarRemoteOwn rejects `far own T` (remote ownership).
	SemaFarRemoteOwn Code = 3189
	// SemaFarRemoteBorrow rejects `far &T` / `far &mut T` (remote borrowed lifetimes).
	SemaFarRemoteBorrow Code = 3190
	// SemaFarExternTarget rejects `far extern<T>` as a value capability.
	SemaFarExternTarget Code = 3191
	// SemaFarGroupingUnsupported rejects grouped type forms that would change `far` precedence.
	SemaFarGroupingUnsupported Code = 3192
	// SemaFarNonCapability rejects `far` over a non-remote-handle-capable base type.
	SemaFarNonCapability Code = 3193
	// SemaFarLocalOp rejects local operations on `far T` outside an accepted crossing context.
	SemaFarLocalOp Code = 3194

	// --- Block 2: `on dst { ... }` placement crossing (SEM 3195, 3144-3153) ---

	// SemaOnDestFarTask rejects `far Task<T>` used as an `on` destination.
	SemaOnDestFarTask Code = 3195
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
	// SemaSelectFarArmsSingleOwner rejects a select that mixes far channel arms
	// with any other arm kind: a remote select ships to one owner shard whole.
	SemaSelectFarArmsSingleOwner Code = 3176

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
	// FutCrossingSyncContext marks a crossing whose only blocker is the missing async context.
	FutCrossingSyncContext Code = 7019
	// FutCrossingPayloadNotShippable marks a crossing whose payload or capture cannot cross shards yet.
	FutCrossingPayloadNotShippable Code = 7020
	// FutChannelShareBackendUnavailable marks `far Channel<T>.share()` as unavailable in this backend/configuration.
	FutChannelShareBackendUnavailable Code = 7021
	// FutChannelSelectBackendUnavailable marks a remote select as unavailable in this backend/configuration.
	FutChannelSelectBackendUnavailable Code = 7022
)
