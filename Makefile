.PHONY: build run test runtime-v2-check runtime-v2-abi-manifest-check runtime-v2-slot-control-check runtime-v2-ownership-check runtime-v2-crossing-check runtime-v2-heap-check runtime-v2-waiter-check runtime-v2-fd-registry-check runtime-v2-net-handle-check runtime-v2-http-owner-check runtime-v2-accept-check runtime-v2-lock-check runtime-v2-lifecycle-check runtime-v2-perf-check runtime-v2-syncpoint-check runtime-v2-panic-surface-check runtime-v2-transport-contract-check runtime-v2-transport-check runtime-v2-carrier-check runtime-v2-carrier-sanitizer-check runtime-v2-place-overwrite-check runtime-v2-carrier-bench runtime-v2-carrier-baseline-capture runtime-v2-carrier-bench-final vet sec format fmt lint staticcheck pprof-cpu pprof-mem trace install install-system uninstall uninstall-system completion completion-install completion-install-system install-hooks
.PHONY: golden golden-update golden-check golden-corpus-determinism behaviour-check behaviour-check-all behaviour-check-mt stats
.PHONY: c-check cfmt-check c-warnings ctidy cppcheck c-check-changed

# ===== Variables =====
GO ?= go
PYTHON ?= python3
SURGE_SKIP_TIMEOUT_TESTS ?= 1
SURGE_MT_TIMEOUT_SCALE ?= 3

GOBIN := $(shell $(GO) env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell $(GO) env GOPATH)/bin
endif

LDFLAGS_SCRIPT := ./scripts/ldflags.sh

GOLANGCI_LINT := $(GOBIN)/golangci-lint
GOLANGCI_LINT_VERSION := v2.7.2

STATICCHECK := $(GOBIN)/staticcheck
GOSEC := $(GOBIN)/gosec

# ===== C Runtime Variables =====
CC ?= clang
CXX ?= clang++
C_RUNTIME_DIR := runtime/native
C_SOURCES := $(shell find $(C_RUNTIME_DIR) -name '*.c' 2>/dev/null)
C_HEADERS := $(shell find $(C_RUNTIME_DIR) -name '*.h' 2>/dev/null)
C_FILES := $(C_SOURCES) $(C_HEADERS)

# Strict warning flags for C compilation
C_WARN_FLAGS := -Wall -Wextra -Wpedantic -Werror \
	-Wshadow -Wconversion -Wsign-conversion -Wcast-qual -Wcast-align \
	-Wstrict-prototypes -Wmissing-prototypes -Wold-style-definition \
	-Wformat=2 -Wundef -Wdouble-promotion -fno-common

C_STD := -std=c11
C_INCLUDES := -I$(C_RUNTIME_DIR)

# ===== OS Detection =====
# Определение операционной системы
UNAME_S := $(shell uname -s 2>/dev/null || echo "Unknown")
ifeq ($(UNAME_S),Darwin)
	OS := darwin
	# На macOS используем стандартные пути
	SYSTEM_BINDIR := /usr/local/bin
	SYSTEM_SHAREDIR := /usr/local/share/surge
	# На macOS нет /etc/profile.d/, используем /etc/paths.d/ для PATH
	# Для переменных окружения лучше использовать ~/.zshrc или ~/.bash_profile
	PROFILE_DIR := /etc/paths.d
	PROFILE_FILE := /etc/paths.d/surge
else
	# Linux и другие Unix-подобные системы
	OS := linux
	SYSTEM_BINDIR := /usr/local/bin
	SYSTEM_SHAREDIR := /usr/local/share/surge
	PROFILE_DIR := /etc/profile.d
	PROFILE_FILE := /etc/profile.d/surge.sh
endif

# ===== Build =====
build:
	@echo ">> Building surge"
	@rm -f surge
	@$(GO) build -ldflags "$$($(LDFLAGS_SCRIPT) --local)" -o surge ./cmd/surge/

# ===== Run =====
run:
	@echo ">> Running surge $(filter-out $@,$(MAKECMDGOALS))"
	$(GO) run ./cmd/surge/ $(filter-out $@,$(MAKECMDGOALS))

# ===== Vet =====
vet:
	@echo ">> Running vet"
	$(GO) vet ./...

sec:
	@echo ">> Running gosec"
	$(GOSEC) ./...

# ===== Test =====
# The per-package timeout is a HANG DETECTOR, not a budget: it exists so a test
# that never returns cannot block the pre-commit hook forever, and it measures
# nothing about the code. It was 90s while `internal/vm` legitimately takes ~80s,
# which left about ten seconds of headroom and made the hook fail whenever
# anything else was running on the machine - three times on 2026-08-14 alone, at
# 90.012s each, on commits that had nothing to do with it. Raising it does not
# weaken an assertion; a hung test still hangs and is still caught, 210 seconds
# later. See RV2-DEBT-220 for the measurements.
test:
	@echo ">> Running tests"
	SURGE_SKIP_TIMEOUT_TESTS=$(SURGE_SKIP_TIMEOUT_TESTS) $(GO) test ./... --timeout 300s

.PHONY: runtime-v2-file-size-check
runtime-v2-file-size-check:
	@./scripts/runtime_v2_file_size_check.sh

runtime-v2-check:
	@echo ">> Checking Runtime V2 LLVM toolchain"
	@if ! command -v clang >/dev/null 2>&1; then \
		echo "Error: clang not found. Install with: sudo apt-get install -y clang llvm lld binutils"; \
		exit 1; \
	fi
	@if ! command -v ar >/dev/null 2>&1; then \
		echo "Error: ar not found. Install with: sudo apt-get install -y binutils"; \
		exit 1; \
	fi
	$(MAKE) runtime-v2-abi-manifest-check
	$(MAKE) runtime-v2-slot-control-check
	@echo ">> Running Runtime V2 liveness gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 SURGE_MT_TIMEOUT_SCALE=$(SURGE_MT_TIMEOUT_SCALE) $(GO) test ./internal/vm -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$$' -count=1 -parallel=1 -p=1 -v --timeout 120s
	$(MAKE) runtime-v2-ownership-check
	$(MAKE) runtime-v2-crossing-check
	$(MAKE) runtime-v2-heap-check
	$(MAKE) runtime-v2-waiter-check
	$(MAKE) runtime-v2-fd-registry-check
	$(MAKE) runtime-v2-net-handle-check
	$(MAKE) runtime-v2-http-owner-check
	$(MAKE) runtime-v2-accept-check
	$(MAKE) runtime-v2-lock-check
	$(MAKE) runtime-v2-lifecycle-check
	$(MAKE) runtime-v2-perf-check
	$(MAKE) runtime-v2-syncpoint-check
	$(MAKE) runtime-v2-panic-surface-check
	$(MAKE) runtime-v2-carrier-check
	$(MAKE) runtime-v2-transport-check

runtime-v2-abi-manifest-check:
	@echo ">> Checking Runtime V2 typed-carrier ABI manifest"
	@command -v clang >/dev/null 2>&1 || { \
		echo "Error: clang is required for the typed-carrier strong-link ABI proof"; \
		exit 1; \
	}
	@command -v clang++ >/dev/null 2>&1 || { \
		echo "Error: clang++ is required for the typed-carrier C++ linkage proof"; \
		exit 1; \
	}
	@command -v llvm-nm >/dev/null 2>&1 || command -v nm >/dev/null 2>&1 || { \
		echo "Error: llvm-nm or nm is required for the typed-carrier strong-link ABI proof"; \
		exit 1; \
	}
	$(GO) run ./cmd/abi-manifest-gen -check
	SURGE_REQUIRE_TYPED_CARRIER_ABI_TOOLS=1 $(GO) test ./internal/abimanifest -count=1 --timeout 60s
	SURGE_REQUIRE_TYPED_CARRIER_ABI_TOOLS=1 $(GO) test ./internal/backend/llvm -run '^TestTypedCarrier' -count=1 --timeout 120s
	SURGE_REQUIRE_TYPED_CARRIER_ABI_TOOLS=1 $(GO) test ./internal/backend/llvm -run '^Test(PlanCrossUnavailableStub|AReturningPlanCrossStub|EmittedValueOpsDescriptor|ADefectiveDescriptor)' -count=1 --timeout 120s
	$(GO) test ./internal/buildpipeline -run '^Test(TypedCarrier|DiscoverRuntimeABIHash)' -count=1 --timeout 60s
	$(GO) test ./internal/vm -run '^TestRuntimeV2TypedCarrier' -count=1 --timeout 60s
	$(CC) $(C_STD) $(C_WARN_FLAGS) $(C_INCLUDES) -fsyntax-only runtime/native/rt_typed_carrier_abi.generated.c

runtime-v2-carrier-check:
	@echo ">> Running Runtime V2 carrier harness and bridge gate"
	PYTHONDONTWRITEBYTECODE=1 $(PYTHON) -m unittest discover -s scripts -p 'runtime_v2_carrier_bench*_test.py'
	$(GO) test ./internal/buildpipeline -run '^TestRuntime(TestSyncPoint|CarrierBench)BuildFlag$$' -count=1
	$(GO) test ./internal/carriergate -count=1
	$(GO) test -race ./internal/vm -run '^TestRuntimeV2CarrierBench' -count=1 --timeout 120s

# The mandatory carrier sanitizer gate (epic 23b section 12). It is a CLOSEOUT
# gate and deliberately not part of `make check`: check is the pre-commit hook,
# and this target costs minutes of valgrind. Its owned exemption from the
# gate-integrity reachability rule is in internal/gatecheck/exemptions.txt.
#
# Availability comes FIRST and never skips: the preflight proves Valgrind,
# ASan/UBSan and TSan are installed AND actually instrumenting on this host by
# making each one catch a planted defect. A missing or inert tool fails here.
#
# Every row then runs through the script's `run` mode, which fails the target
# on any `--- SKIP` or empty selection the row prints. Both rows below contain
# a live skip-on-missing site today (a clang skip in the blocking lost-wake
# proof, a valgrind skip in the strict census); this is what disables them.
#
# Every row also NAMES the tests it must execute, via --expect. Exit status
# cannot tell a full row from one whose -run alternation has lost a member:
# delete the two sanitizer tests below and `go test` still exits 0 on the
# 0.00s survivor, leaving a mandatory sanitizer gate green with no sanitizer
# execution at all. --expect fails the row unless every named test printed
# `--- PASS:`. The lists are cross-checked against the live `go test -list`
# selection by TestCarrierSanitizerMakefileTargetShape, which catches a declared
# test that the row's own -run no longer selects.
#
# That cross-check alone would NOT catch a row narrowed in both fields at once,
# because both fields are in this file and a shrunken row stays self-consistent.
# The ratchet that does catch it is requiredSanitizerCoverage, in
# internal/gatecheck/runtime_v2_carrier_sanitizer_check_test.go: the rows here
# must together cover that list, so shrinking this gate means editing that file
# too.
#
# Each `$(GO) test` row is spelled `bash scripts/...` and not `./scripts/...`
# on purpose: internal/gatecheck parses every `$(GO) test` recipe line and
# reads a `./`-prefixed field as a package name, so a `./`-spelled wrapper
# would be handed to `go test -list` as if it were a package. (The preflight
# line below carries no `$(GO) test` and is therefore never parsed, which is
# why `./scripts/...` is fine there.)
#
# The rows are the carrier rows that carry sanitizer or valgrind coverage
# today: the typed carrier slot API under ASan/UBSan and TSan, carrier
# reclamation under valgrind, and the carrier bench bridge under the race
# detector. The purely static carrier rows stay in runtime-v2-carrier-check,
# which section 12 already runs twice. Wave D owner migrations add their rows
# here as they land.
#
# Migration tripwire for whoever moves the runtime-v2-slot-control-check row
# below through this wrapper. TestRuntimeV2SlotControlRequiredSanitizersFailClosed
# (internal/vm/runtime_v2_slot_control_test.go) pins a LITERAL substring of this
# Makefile: `SURGE_REQUIRE_SLOT_CONTROL_SANITIZERS=1`
# immediately followed by a space and `$(GO) test`. The wrapper spelling used
# by this target puts the script between them, so it does NOT satisfy that
# assertion; the assertion passes today only because the slot-control row is
# still spelled the old way. Migrate that row and it fails with
# "runtime-v2-slot-control-check must require sanitizer support", which names
# the wrong cause — relax the assertion in the same commit as the migration.
# (The two fragments are deliberately quoted on separate lines here: written
# out as one string, this comment alone would satisfy the assertion and the
# tripwire would become a gate nothing can fail.)
runtime-v2-carrier-sanitizer-check:
	@echo ">> Running Runtime V2 carrier sanitizer gate"
	@./scripts/runtime_v2_carrier_sanitizer_check.sh preflight
	SURGE_REQUIRE_SLOT_CONTROL_SANITIZERS=1 bash scripts/runtime_v2_carrier_sanitizer_check.sh run --expect TestRuntimeV2SlotControlAddressAndUndefinedSanitizers,TestRuntimeV2SlotControlThreadSanitizer,TestRuntimeV2SlotControlRequiredSanitizersFailClosed -- $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2SlotControl(AddressAndUndefinedSanitizers|ThreadSanitizer|RequiredSanitizersFailClosed)$$' -count=1 -parallel=1 -p=1 -v --timeout 300s
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 bash scripts/runtime_v2_carrier_sanitizer_check.sh run --expect TestRuntimeV2DropFarChannelHandleAndObjectValgrindZero,TestRuntimeV2CrossingStrictCensusValgrindBounded -- $(GO) test ./internal/vm -run '^TestRuntimeV2(DropFarChannelHandleAndObjectValgrindZero|CrossingStrictCensusValgrindBounded)$$' -count=1 -parallel=1 -p=1 -v --timeout 900s
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 bash scripts/runtime_v2_carrier_sanitizer_check.sh run --expect TestRuntimeV2CarrierBenchBlockingRegisterThenVerify,TestRuntimeV2CarrierBenchCounterMatrix,TestRuntimeV2CarrierBenchBridgeHasNoHotPathEnvironmentLookup -- $(GO) test -race ./internal/vm -run '^TestRuntimeV2CarrierBench' -count=1 -parallel=1 -p=1 -v --timeout 300s

runtime-v2-carrier-bench:
	@echo ">> Running fail-closed Runtime V2 carrier benchmark"
	PYTHONDONTWRITEBYTECODE=1 taskset -c 0,2 $(PYTHON) scripts/runtime_v2_carrier_bench.py --phase=final

runtime-v2-carrier-baseline-capture:
	@echo ">> Capturing complete Wave-A endpoint RED baseline (protocol failures remain fatal)"
	PYTHONDONTWRITEBYTECODE=1 taskset -c 0,2 $(PYTHON) scripts/runtime_v2_carrier_bench.py --phase=wave-a --capture-wave-a-baseline

runtime-v2-carrier-bench-final:
	@$(MAKE) runtime-v2-carrier-bench

runtime-v2-slot-control-check:
	@echo ">> Running Runtime V2 owner-private slot-control gate"
	SURGE_REQUIRE_SLOT_CONTROL_SANITIZERS=1 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2SlotControl(Protocol|AddressAndUndefinedSanitizers|ThreadSanitizer|IsOwnerPrivateAndCallbackFree|CopyInitTrapIsNamedAndUndispatched|MoveAndDropDispatchThroughTheDetachedHelpers|RequiredSanitizersFailClosed)$$' -count=1 -parallel=1 -p=1 -v --timeout 120s

# The overwritten-value obligation, per place shape, on both lanes.
#
# GREEN ON BOTH LANES. It was red on native by design — a store over a struct
# field, a tuple element, a `&mut` target or an array element did not free what
# it displaced there — and it carried a `runtime_v2_pending` build tag so that a
# known-red gate stayed committable. Both lanes report zero now, the tag is
# gone, and the tests therefore ALSO run inside `make check` and the pre-commit
# hook. This target stays because it names the obligation and runs the gate
# alone, verbosely, when that is what is being worked on.
#
# The selection covers LoopBinding as well as PlaceOverwrite, and that is not
# tidiness: the two are one defect family. Recording the obligation is only safe
# where the place is actually owned, and a loop binding is a copy the container
# still owns — freeing through it was an invalid free BEFORE this obligation
# existed, for the whole-binding spelling. A selection naming only one of them
# would let the other regress with the gate still green.
runtime-v2-place-overwrite-check:
	@echo ">> Running Runtime V2 overwritten-value obligation gate"
	SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test ./internal/vm -run '^TestRuntimeV2(PlaceOverwrite|LoopBinding)' -count=1 -parallel=1 -p=1 -v --timeout 300s

runtime-v2-ownership-check:
	@echo ">> Running Runtime V2 ownership corpus gate"
	SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_ownership_corpus ./internal/ownershipgate -run '^Test(OwnershipCorpusCompileProfileContract|OwnershipCorpusInventoryDigestContract|OwnershipCorpusCensusReportContract|OwnershipCorpusCensusReportAccountingContract|OwnershipCorpusCensusReportInvalidationAndAtomicFailure|OwnershipCorpusFailureSignatureContract|OwnershipCorpusLLVMBackendContract|RuntimeV2OwnershipCorpus)$$' -count=1 -parallel=1 -p=1 -v --timeout 900s

# The crossinggate package runs for about a minute on the reference host, so the
# 60s it used to get made this gate a coin flip: measured runs landed at 59.9s
# and 60.0s on either side of the limit. A timeout that close to the real
# runtime does not catch a hang, it manufactures a red gate that goes green on
# a re-run, which is the habit most corrosive to every other gate here. The
# headroom below is deliberate; a genuine hang is unbounded and will still be
# caught. Measure before lowering it.
runtime-v2-crossing-check:
	@echo ">> Running Runtime V2 crossing readiness gate"
	$(GO) test ./internal/crossinggate -count=1 --timeout 300s
	$(GO) test ./internal/buildpipeline ./internal/hir -run '^(TestCrossingBackendUnavailableMessages|TestCrossingBackendGuardsAreDefaultClosed|TestCrossingBackendGuardDoesNotMaskSemaErrors|TestCrossingBackendGuardsCoverImportedModules|TestVMAndUnknownBackendsKeepExecutableAsyncFormsGuarded|TestLLVMTransportCapabilityOpensAsyncSpawnOn|TestLLVMTransportCapabilityOpensAsyncImmediateOn|TestLLVMTransportCapabilityOpensAsyncFarTaskLifecycle|TestLowerOnCrossingBypassReturnsError|TestLowerSpawnOnCrossingBypassReturnsError|TestLowerFarTaskCrossingBypassReturnsError|TestLowerCrossingRepresentationWithExplicitCapability)$$' -count=1 --timeout 60s
	$(GO) test ./internal/mono ./internal/mir -run '^(TestMonoPreservesCrossingRepresentation|TestMIRCrossingRepresentationWithExplicitCapability|TestMIRCrossingValidationDefaultClosed|TestMIRAsyncCrossingSuspendRepresentation)$$' -count=1 --timeout 60s
	$(GO) test ./internal/sema ./internal/driver -run '^(TestCrossingLowering.*|TestFunctionCrossingEffectInference|TestCrossingReadinessDebt024ModuleImportDoesNotRequireImportedEffects)$$' -count=1 --timeout 60s

runtime-v2-syncpoint-check:
	@echo ">> Running Runtime V2 sync-point proving-spike static gate"
	./check_sync_points.sh

# The panic-surface census. It enumerates every place the compiler and the C
# runtime can raise a panic and refuses one that is neither reached by a
# behavioural fixture on both backends nor excused by an owned row. The package
# already rides `go test ./...`; it is named here as well because a gate nobody
# can run alone is a gate nobody runs while fixing it.
runtime-v2-panic-surface-check:
	@echo ">> Running the panic-surface census gate"
	$(GO) test ./internal/panicgate -run '^Test(PanicSitesAreCoveredOrExcused|EveryRecordedFixtureIsActuallyRun|PanicScanFindsTheKnownSurface|PanicReportersAreAllKnown|EmitterRaisesOnlyFromTheKnownPackage)$$' -count=1 --timeout 120s

# The stable transport gate: park/wake spine acceptance, publication rows,
# the crossing e2e verticals, race rows, and the negative matrix all run
# through the contract target below.
runtime-v2-transport-check: runtime-v2-transport-contract-check
	@echo ">> Runtime V2 transport gate complete"

runtime-v2-transport-contract-check:
	@echo ">> Running Runtime V2 transport contract gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2Transport(SeamStaticShape|SpineBehavior|SyncPointAllowlistShape|ProbeRowsDocumented)$$' -count=1 -parallel=1 -p=1 -v --timeout 60s
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_transport_spine ./internal/vm -run '^TestRuntimeV2TransportSpineAcceptanceRows$$' -count=1 -parallel=1 -p=1 -v --timeout 120s
	@echo ">> Running Runtime V2 remote task acceptance gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2Remote(TaskBehavior|TaskReplyValidationIsGenerationQualified|TaskSourcesRespectFileLimit|TaskStateHasInitRollbackPair|ChannelSelfDeadlockPanics|StateHandoffStaticContract|SpawnAbandonEdges|SpawnStaleGenerationRows|SelectAbandonEdges|Publication(APIShape|Behavior|FailurePathStaticGuards))$$|^TestRuntimeV2FarSelectInitialFailurePayloadOwnershipStaticContract$$|^TestRuntimeV2ImmediateOnAbandonEdges$$|^TestRuntimeV2TransportReplyWaitersHaveExplicitShardRouting$$' -count=1 -parallel=1 -p=1 -v --timeout 300s
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test ./internal/vm -run '^TestRuntimeV2FarTaskSource(OverrideAcrossShards|ProductionCapability)$$|^TestRuntimeV2SpawnOnPoolProductionCapabilityFailsDeterministically$$' -count=1 -parallel=1 -p=1 -v --timeout 300s
	@echo ">> Running Runtime V2 immediate-on acceptance gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test ./internal/vm -run '^TestRuntimeV2ImmediateOn(SourceOverrideAcrossShards|SourceProductionCapability|PoolProductionCapabilityFailsDeterministically)$$|^TestRuntimeV2ImportedCrossingProductionCapability$$' -count=1 -parallel=1 -p=1 -v --timeout 300s
	@echo ">> Running Runtime V2 channel genesis gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test ./internal/vm -run '^TestRuntimeV2(ChannelGenesisOverrideAcrossShards|OnChAnchoredOpsAcrossShards|ShareFanOutAcrossShards|RemoteSelectFanInAcrossShards|OwnedCaptureMigrationAcrossShards|CrossingHeapCaptureArrivesIntact|FarTaskCallerCancel)$$' -count=1 -parallel=1 -p=1 -v --timeout 300s

runtime-v2-heap-check:
	@echo ">> Running Runtime V2 heap accounting gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test ./internal/vm -run '^TestLLVMNative(HeapStats|BufferedChannelAllocatesSingleBlock)$$' -count=1 -parallel=1 -p=1 -v --timeout 120s
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test ./internal/vm -run '^TestRuntimeV2HeapAccounting(SequentialContracts|ConcurrentWorkersContract)$$' -count=1 -parallel=1 -p=1 -v --timeout 180s
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2HeapAccountingStatic(PublicABI|ShardCellSkeletonShape|RecordMigrationShape|SnapshotAggregationShape)$$' -count=1 -parallel=1 -p=1 -v --timeout 60s
	@echo ">> Running Runtime V2 drop reclamation gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test ./internal/vm -run '^TestRuntimeV2Drop(LeafReclamation|ScopeExit|FieldAliasDoesNotDoubleFree|ArmSynthesis|Composite|SelectSendArm|UnionCastReclamation)$$|^TestRuntimeV2(FarSelectCancelNonCopySendArm|LocalSelectCancelNonCopySendArm)$$|^TestRuntimeV2CrossingHeapCaptureCensusBalanced$$|^TestRuntimeV2CrossingStrictCensusBalanced$$|^TestRuntimeV2IterProtocolReclamationCensusBalanced$$|^TestRuntimeV2CompareScrutineeReleaseCensusBalanced$$' -count=1 -parallel=1 -p=1 -v --timeout 300s
	@echo ">> Running Runtime V2 strict-census valgrind gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test ./internal/vm -run '^TestRuntimeV2CrossingStrictCensusValgrindBounded$$|^TestRuntimeV2DropFarChannelHandleAndObjectValgrindZero$$' -count=1 -parallel=1 -p=1 -v --timeout 300s
	@echo ">> Running Runtime V2 array-view reclamation gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test ./internal/vm -run '^TestRuntimeV2ArrayViewHeaderReclaimedPerSlice$$' -count=1 -parallel=1 -p=1 -v --timeout 300s
	@echo ">> Running Runtime V2 fixnum inline-int gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test ./internal/vm -run '^TestRuntimeV2Fixnum(HotLoopHeapBalanced|BoundaryValues)$$' -count=1 -parallel=1 -p=1 -v --timeout 120s
	@echo ">> Running Runtime V2 integer range-for gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test ./internal/vm -run '^TestRuntimeV2RangeFor(IntegerHead|StoredValue)$$' -count=1 -parallel=1 -p=1 -v --timeout 120s

runtime-v2-waiter-check:
	@echo ">> Running Runtime V2 waiter liveness gate"
	$(GO) test ./internal/vm -run '^TestRuntimeV2WaiterHelperStaticBoundary$$' -count=1 -v --timeout 30s
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2(CancelledRecvWaiterDoesNotConsumeNextWake|CancelledSendWaiterDoesNotConsumeNextRecv|ChannelCloseWakesRecvWaiters|ChannelCloseWakesSendWaiters|SelectTimeoutCleansLosingChannelWaiter|CancelledSelectCleansWaitKeysAndTimers|CancelledJoinWaiterDoesNotConsumeTaskCompletionWake|FailfastScopeCancellationWakesOwner|BlockingCompletionWakesAwaiter|CancelledBlockingWaiterDoesNotConsumeCompletionWake|OwnerLocalWaiterSkeletonStaticShape|OwnerLocalTraceAggregatesShardWaiters|OwnerLocalNetWaiterBehavior|NetWaiterTraceContract)$$' -count=1 -parallel=1 -p=1 -v --timeout 120s
	SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2FreedChannelWaiterRouting$$' -count=1 -parallel=1 -p=1 -v --timeout 900s
	SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2(TimeoutOverNetWaitSurvivesItsWallBudget|SleepWithoutNetWaiterStillAdvancesInstantly)$$' -count=1 -parallel=1 -p=1 -v --timeout 300s

runtime-v2-fd-registry-check:
	@echo ">> Running Runtime V2 fd registry liveness gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2FDRegistry(RepeatedReadinessSingleFD|ReadWriteInterestSharesFDRow|DuplicateReadWaitersBothComplete|ClosedFDFailsFast|StaticShape|StaticBoundary|GenerationStaleSnapshotProof|CloseWakePollNotificationProof|ShutdownDrainStaticContract|ShutdownDrainBehavior|CancelledDuplicateReadWaiterPreservesLiveAndReregister|CancelledReadInterestPreservesWriteInterest|CloseWakesParkedAcceptWaiter|CloseWakesParkedReadWaiter|WakeFDObservedForInterestAddedDuringPoll|CancelledInterestWakesPoller|HandleWordPublishedInline)$$' -count=1 -parallel=1 -p=1 -v --timeout 180s

runtime-v2-net-handle-check:
	@echo ">> Running Runtime V2 net-handle guard gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2NetHandle(StaleCopyReusedFD|GuardStaticShape|CanonicalOutlivesPublicBox|ResultAllocationRollback)$$' -count=1 -parallel=1 -p=1 -v --timeout 180s

runtime-v2-http-owner-check:
	@echo ">> Running Runtime V2 HTTP owner-local gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2HTTPOwnerLocal(StaticShape|Behavior)$$' -count=1 -parallel=1 -p=1 -v --timeout 180s

runtime-v2-accept-check:
	@echo ">> Running Runtime V2 accept CI gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test ./internal/vm -run '^TestRuntimeV2AcceptShardOneNativeNetCompatibility$$' -count=1 -parallel=1 -p=1 -v --timeout 120s
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2(NetMetadata(StaticShape|MultiShardListenClose)|Accept(ShardConfigInitializesRequestedShardCount|RejectsInvalidShardConfig|RejectsConflictingThreadCount|DistributionAcrossOwnerShards|OwnerShardLifecycleTraceContract|NetOwnershipNoShard0Shortcut|DynamicShardArrayShape|ReadinessClearsSiblingWaitKeys|ListenerRegistryGrowsUnderLock))$$' -count=1 -parallel=1 -p=1 -v --timeout 240s
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2NetPoller(PerShardWakeShape|PerShardWakeBehavior|ShardLocalPollInput|GlobalIOThreadDoesNotOwnMultiShardNetPolling|ShutdownWakesEveryShard)$$' -count=1 -parallel=1 -p=1 -v --timeout 120s
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2SchedulerPlacement(WorkerShape|NoStealPolicy|NoStealWorkerPath|StealPathSourceGate|ParkedWithWorkSourceGate|ParkedWithWorkInvariant|InvalidOwnerFailsClosed)$$|^TestRuntimeV2Placement(ABIStaticShape|ResolverRows)$$|^TestRuntimeV2SkeletonStaticShape$$' -count=1 -parallel=1 -p=1 -v --timeout 180s

runtime-v2-lock-check:
	@echo ">> Running Runtime V2 lock split gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2LockSplit(LaneAPIShape|ShardSyncShape|WorkerLoopShardLane|NoAmbiguousGlobalLock|ClockAndSleepStoreShape|NoWholeTableSleepScan|ChannelOwnerShape|GlobalCondvarRetirement)$$' -count=1 -parallel=1 -p=1 -v --timeout 120s
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2LockSplit(CrossShardJoin|CrossShardCancel|CrossShardChannelFifoAndClose|ChannelCloseWakesParkedReceiver|SelectAcrossOwners|TimeoutAcrossOwners|SleepIdleAdvanceMultiShard|BlockingCompletionCrossShard|ShutdownWakesAllLanes)$$' -count=1 -parallel=1 -p=1 -v --timeout 300s

# Manual liveness stress (quarantined; owner: runtime maintainers). Bounds
# the RV2-DEBT-027 double-poll recurrence: 50 in-suite repetitions of the
# park/unpark MT load plus the TSan completion-pin suite, which carries the
# closest sanitizer coverage of the polling paths. Not part of make check;
# recorded as the gate manifest's single seeded exemption.
runtime-v2-liveness-stress:
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 SURGE_MT_TIMEOUT_SCALE=3 $(GO) test ./internal/vm -run '^TestMTChannelParkUnpark$$' -count=50 -parallel=1 -p=1 --timeout 900s
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2LifecycleCompletionPinInterleavingTSan$$' -count=3 -parallel=1 -p=1 --timeout 900s

runtime-v2-lifecycle-check:
	@echo ">> Running Runtime V2 task-lifecycle lane gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2Lifecycle(StaticCompletionResultVisibilityOrder|StaticControlSiteEnumShape|StaticJoinWaiterRoutesByTargetOwner|StaticTaskTableAtomicSnapshot|StaticJoinScopeWaitersUnqualified|StaticCreateSiteCounterWired|StaticCensusSitesTagged|TraceControlSiteContract|OwnerLocalCreateAndReadyPublication|JoinPollResultObservation|JoinWaiterCleanupRegisterThenVerify|CloneReleaseLastReferenceFree|ScopeEnterRegisterJoinExit|ScopeFailfastCancellation|ScopeCancelledPollTeardown|WorkerAwaitVsExternalAwait|ShutdownWithParkedTasks|StaticCreateReadyPushOwnerShard|CancelSpawnChildrenRace|CancelSpawnChildrenRaceTSan|StaticJoinPollOwnerLane|StaticScopeOwnerLane|JoinConsumePlacementAdoption|ScopeEnterRegisterJoinExitAcrossShards|ScopeFailfastCancellationAcrossShards|ScopeCancelledPollTeardownAcrossShards|ScopeCrossOwnerChildDone|Debt020MigrateGapProof|Debt020MigrateGapNegativeControl|Debt022DoneCVStoreLoadProof|Debt022DoneCVStoreLoadNegativeControl|Debt022ExternalAwaitMatrix|Debt023CancelParkWakeTokenProof|Debt023CancelParkWakeTokenNegativeControl|CompletionPinInterleavingTSan|StaticAwaitCompatCountedSeparately|TraceAwaitCompatCountedSeparately|ReadyRequeueWakeRaceProof|ReadyRequeueWakeRaceNegativeControl|Debt046JoinStaleRemovalProof|Debt046JoinStaleRemovalNegativeControl|SleepFiredBatchIsNotIdleProof|SleepFiredBatchIsNotIdleNegativeControl|Debt201AbortedParkRetiresChannelEntry|Debt201AbortedParkRetiresChannelEntryNegativeControl|Debt201AbortRetirementStaticShape)$$' -count=1 -parallel=1 -p=1 -v --timeout 300s

runtime-v2-perf-check:
	@echo ">> Running Runtime V2 performance CI gate"
	SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 $(GO) test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2PerfControlLaneGate$$' -count=1 -parallel=1 -p=1 -v --timeout 180s

# ===== Format =====
format: fmt

fmt:
	@echo ">> Formatting code"
	$(GO) fmt ./...

golden: golden-check

golden-update: build
	@$(GO) run ./cmd/goldencheck update -- ./scripts/golden_update.sh

golden-check: build
	@$(GO) run ./cmd/goldencheck check --runs 2 -- ./scripts/golden_update.sh

# Two serialized regenerations must leave the corpus exactly as reviewed. Its
# preflight refuses a corpus with uncommitted changes, because there is no
# reviewed starting point to compare against, so this cannot live in the
# pre-commit hook - a hook runs on precisely the state it refuses.
golden-corpus-determinism:
	@echo ">> Checking that two golden regenerations preserve the corpus (needs a committed testdata/golden)"
	$(GO) test -tags golden ./internal/goldencheck -run TestGoldenUpdateDeterminism -count=1 --timeout 600s

# The behavioural corpus: compile each recorded program, run it, and compare its
# real output against the recorded .out. This is the only thing in the repository
# that can observe a wrong runtime answer.
#
# `behaviour-check` is the VM lane and is what `make check` already runs as part
# of `make test`; this target names it so it can be run alone.
behaviour-check:
	@echo ">> Running the behavioural corpus (vm)"
	$(GO) test ./internal/vm -run 'Golden|Determinism' -count=1 --timeout 900s

# `behaviour-check-all` adds the native lane. It costs about six seconds per
# fixture against the VM lane's quarter-second, because clang compiles and links
# the runtime for each one, so it is run at an important step rather than on
# every commit. It FAILS rather than skips when the toolchain is missing.
behaviour-check-all:
	@echo ">> Running the behavioural corpus (vm + native)"
	SURGE_BEHAVIOUR_BACKENDS=vm,llvm $(GO) test ./internal/vm -run 'Golden' -count=1 --timeout 3600s

# `behaviour-check-mt` is the other half of the same question. The two lanes
# above compare the backends with ONE worker, because that is the only
# configuration in which "did the two backends agree" has a single right answer.
# This one runs the async corpus on the native backend with SEVERAL workers and
# asserts what several workers actually promise: that the program terminates,
# exits with its recorded code, and reports no allocator or sanitizer failure.
# It deliberately does not compare output text - ordering across workers is a
# documented non-guarantee, so asserting it would be asserting a licence.
#
# Worker counts and repeats are configurable: SURGE_BEHAVIOUR_MT=2,4,8 and
# SURGE_BEHAVIOUR_MT_REPEATS=5 for a longer sweep.
behaviour-check-mt:
	@echo ">> Running the async corpus on the native backend, multi-worker"
	SURGE_BEHAVIOUR_MT=1 SURGE_BEHAVIOUR_BACKENDS=llvm $(GO) test ./internal/vm -run 'BehaviourCorpusMT' -count=1 --timeout 3600s

check:
	@echo ">> Checking code"
	$(MAKE) test
	$(MAKE) lint
	$(MAKE) c-check
	@echo ">> Checking file sizes"
	@echo "It may take a while... please wait..."
	./check_file_sizes.sh

# ===== Lint =====
$(GOLANGCI_LINT):
	@echo ">> Installing golangci-lint $(GOLANGCI_LINT_VERSION)"
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

lint: $(GOLANGCI_LINT)
	@echo ">> Running linters"
	$(GOLANGCI_LINT) run --config .golangci.yaml

# ===== Staticcheck =====
$(STATICCHECK):
	@echo ">> Installing staticcheck"
	$(GO) install honnef.co/go/tools/cmd/staticcheck@latest

staticcheck: $(STATICCHECK)
	@echo ">> Running staticcheck"
	$(STATICCHECK) ./...

# ===== C Runtime Checks =====
# Check C code formatting with clang-format
cfmt-check:
	@echo ">> Checking C code formatting"
	@if ! command -v clang-format >/dev/null 2>&1; then \
		echo "Error: clang-format not found. Install with: sudo apt-get install -y clang-format"; \
		exit 1; \
	fi
	@failed=0; \
	for file in $(C_FILES); do \
		if [ -f "$$file" ]; then \
			if ! clang-format --dry-run --Werror "$$file" >/dev/null 2>&1; then \
				echo "Formatting error in $$file"; \
				clang-format "$$file" | diff -u "$$file" - || true; \
				failed=1; \
			fi; \
		fi; \
	done; \
	if [ $$failed -eq 1 ]; then \
		echo "C code formatting check failed. Run 'clang-format -i' on the files above."; \
		exit 1; \
	fi
	@echo ">> C code formatting OK"

# Compile C code with strict warnings
c-warnings:
	@echo ">> Compiling C runtime with strict warnings"
	@if ! command -v $(CC) >/dev/null 2>&1; then \
		echo "Error: $(CC) not found. Install with: sudo apt-get install -y clang llvm"; \
		exit 1; \
	fi
	@failed=0; \
	tmpdir=$$(mktemp -d); \
	trap "rm -rf $$tmpdir" EXIT; \
	for src in $(C_SOURCES); do \
		if [ -f "$$src" ]; then \
			obj=$$tmpdir/$$(basename $$src .c).o; \
			if ! $(CC) $(C_STD) $(C_WARN_FLAGS) $(C_INCLUDES) -c "$$src" -o "$$obj" 2>&1; then \
				echo "Compilation failed for $$src"; \
				failed=1; \
			fi; \
		fi; \
	done; \
	if [ $$failed -eq 1 ]; then \
		echo "C code compilation with strict warnings failed"; \
		exit 1; \
	fi
	@echo ">> C code compilation with strict warnings OK"

# Run clang-tidy on C code
ctidy:
	@echo ">> Running clang-tidy on C code"
	@if ! command -v clang-tidy >/dev/null 2>&1; then \
		echo "Error: clang-tidy not found. Install with: sudo apt-get install -y clang-tidy"; \
		exit 1; \
	fi
	@failed=0; \
	for file in $(C_SOURCES); do \
		if [ -f "$$file" ]; then \
			output=$$(clang-tidy "$$file" --config-file=.clang-tidy -- $(C_STD) $(C_INCLUDES) 2>&1); \
			if echo "$$output" | grep -qE "(error|warning):"; then \
				echo "clang-tidy found issues in $$file:"; \
				echo "$$output"; \
				failed=1; \
			fi; \
		fi; \
	done; \
	if [ $$failed -eq 1 ]; then \
		echo "clang-tidy check failed"; \
		exit 1; \
	fi
	@echo ">> clang-tidy check OK"

# Run cppcheck on C code
cppcheck:
	@echo ">> Running cppcheck on C code"
	@if ! command -v cppcheck >/dev/null 2>&1; then \
		echo "Error: cppcheck not found. Install with: sudo apt-get install -y cppcheck"; \
		exit 1; \
	fi
	@if [ -z "$(C_SOURCES)" ]; then \
		echo "No C sources found"; \
		exit 0; \
	fi
	@cppcheck --enable=warning,style,performance,portability \
		--error-exitcode=1 \
		--inline-suppr \
		--suppress=missingIncludeSystem \
		--suppress=unusedFunction \
		--std=c11 \
		$(C_INCLUDES) \
		$(C_SOURCES) || exit 1
	@echo ">> cppcheck OK"

# C checks narrowed to a named list of files (C_CHANGED). This exists so the
# pre-commit hook can hold a C edit to cppcheck and clang-tidy, which Global
# Rule 6 has always required and which nothing enforced: the rule asked for the
# checks to be RECORDED, and a report can simply go unwritten.
#
# The list is narrowed on purpose. Whole-tree `make cppcheck` and `make ctidy`
# are red today on accumulated findings (RV2-DEBT-228), most of them against a
# GENERATED ABI header whose reserved identifier is the proof's requirement, so
# a whole-tree gate in the hook would refuse every C commit rather than the
# wrong ones. An edit answers for itself; the backlog is a separate row.
#
# clang-tidy findings are filtered by the PATH THEY POINT AT, not by the file
# handed to it: `.clang-tidy` sets HeaderFilterRegex to all of runtime/native,
# so the generated ABI header reports through every file that includes it, and
# clang-tidy 18 has no ExcludeHeaderFilterRegex to say otherwise.
.PHONY: c-check-changed
c-check-changed:
	@if [ -z "$(C_CHANGED)" ]; then \
		echo ">> No changed C files to check"; \
		exit 0; \
	fi
	@files=""; \
	for f in $(C_CHANGED); do \
		case "$$f" in *.generated.c|*.generated.h) continue;; esac; \
		[ -f "$$f" ] && files="$$files $$f"; \
	done; \
	if [ -z "$$files" ]; then \
		echo ">> No checkable C files after filtering generated sources"; \
		exit 0; \
	fi; \
	echo ">> Checking changed C files:$$files"; \
	failed=0; \
	for f in $$files; do \
		if ! $(CC) $(C_STD) $(C_WARN_FLAGS) $(C_INCLUDES) -fsyntax-only -x c "$$f"; then \
			echo "strict-warning compile failed for $$f"; failed=1; \
		fi; \
	done; \
	if ! cppcheck --enable=warning,style,performance,portability --error-exitcode=1 \
		--inline-suppr \
		--suppress=missingIncludeSystem --suppress=unusedFunction --std=c11 \
		$(C_INCLUDES) $$files; then \
		echo "cppcheck failed on changed files"; failed=1; \
	fi; \
	for f in $$files; do \
		output=$$(clang-tidy "$$f" --config-file=.clang-tidy -- $(C_STD) $(C_INCLUDES) 2>&1); \
		issues=$$(echo "$$output" | grep -E "(error|warning):" | grep -v '\.generated\.' || true); \
		if [ -n "$$issues" ]; then \
			echo "clang-tidy found issues in $$f:"; echo "$$output"; failed=1; \
		fi; \
	done; \
	if [ $$failed -eq 1 ]; then \
		echo "Changed-C checks FAILED"; \
		exit 1; \
	fi; \
	echo ">> Changed-C checks OK"

# Run all C code checks
c-check: cfmt-check c-warnings
	@echo ">> All C runtime checks passed"

# ===== Profiling helpers =====
pprof-cpu:
	$(GO) run ./cmd/surge diag --cpu-profile=cpu.pprof ./test.sg
	go tool pprof -http=:8081 cpu.pprof

pprof-mem:
	$(GO) run ./cmd/surge diag --mem-profile=mem.pprof ./test.sg
	go tool pprof -http=:8082 mem.pprof

trace:
	$(GO) run ./cmd/surge diag --trace=trace.out ./test.sg
	$(GO) tool trace trace.out

# ===== Statistics =====
stats:
	@./scripts/code_stats.sh

# ===== Git Hooks =====
install-hooks:
	@echo ">> Installing pre-commit hook"
	@hook_path="$$(git rev-parse --git-path hooks/pre-commit 2>/dev/null)"; \
	if [ -z "$$hook_path" ]; then \
		echo "Error: this target must be run inside a git repository"; \
		exit 1; \
	fi; \
	mkdir -p "$$(dirname "$$hook_path")"; \
	chmod +x "$(CURDIR)/scripts/pre-commit" "$(CURDIR)/scripts/ldflags.sh"; \
	ln -sf "$(CURDIR)/scripts/pre-commit" "$$hook_path"; \
	echo ">> Installed $$hook_path -> $(CURDIR)/scripts/pre-commit"

# ===== Install =====
# Установка в $GOBIN (обычно ~/go/bin или $GOPATH/bin)
# Не требует sudo, но нужно добавить $GOBIN в PATH если его там нет
install: build
	@echo ">> Installing surge to $(GOBIN)"
	@mkdir -p $(GOBIN)
	@cp surge $(GOBIN)/surge
	@echo ">> Installed to $(GOBIN)/surge"
	@echo ">> Make sure $(GOBIN) is in your PATH"

# Системная установка (требует sudo)
# Автоматически определяет правильные пути для macOS и Linux
install-system: build
	@echo ">> Detected OS: $(OS)"
	@echo ">> Installing surge to $(SYSTEM_BINDIR) (requires sudo)"
	@sudo mkdir -p $(SYSTEM_BINDIR)
	@sudo cp surge $(SYSTEM_BINDIR)/surge
	@echo ">> Installing standard library to $(SYSTEM_SHAREDIR) (requires sudo)"
	@sudo mkdir -p $(SYSTEM_SHAREDIR)
	@sudo rm -rf $(SYSTEM_SHAREDIR)/core $(SYSTEM_SHAREDIR)/stdlib
	@sudo mkdir -p $(SYSTEM_SHAREDIR)/core $(SYSTEM_SHAREDIR)/stdlib
	@sudo cp -r core/. $(SYSTEM_SHAREDIR)/core/
	@sudo cp -r stdlib/. $(SYSTEM_SHAREDIR)/stdlib/
	@sudo find $(SYSTEM_SHAREDIR)/core $(SYSTEM_SHAREDIR)/stdlib -type d -exec chmod 755 {} +
	@sudo find $(SYSTEM_SHAREDIR)/core $(SYSTEM_SHAREDIR)/stdlib -type f -exec chmod 644 {} +
ifeq ($(OS),darwin)
	@echo ">> On macOS, add to ~/.zshrc or ~/.bash_profile:"
	@echo ">>   export SURGE_STDLIB=$(SYSTEM_SHAREDIR)"
else
	@echo ">> Writing $(PROFILE_FILE) to export SURGE_STDLIB if unset"
	@sudo mkdir -p $(PROFILE_DIR)
	@sudo sh -c 'printf "# surge stdlib path\n: \$${SURGE_STDLIB:=$(SYSTEM_SHAREDIR)}\nexport SURGE_STDLIB\n" > $(PROFILE_FILE)'
endif
	@echo ">> Installed to $(SYSTEM_BINDIR)/surge"
	@echo ">> For current shell run: export SURGE_STDLIB=$(SYSTEM_SHAREDIR)"

# Удаление установленного бинарника из $GOBIN
uninstall:
	@echo ">> Removing surge from $(GOBIN)"
	@rm -f $(GOBIN)/surge
	@echo ">> Removed $(GOBIN)/surge"
	@echo ">> To remove system installation, run: make uninstall-system"

# Удаление системной установки (требует sudo)
# Автоматически определяет правильные пути для macOS и Linux
uninstall-system:
	@echo ">> Detected OS: $(OS)"
	@echo ">> Removing surge from $(SYSTEM_BINDIR) (requires sudo)"
	@sudo rm -f $(SYSTEM_BINDIR)/surge
	@echo ">> Removing standard library from $(SYSTEM_SHAREDIR) (requires sudo)"
	@sudo rm -rf $(SYSTEM_SHAREDIR)
ifeq ($(OS),darwin)
	@echo ">> On macOS, manually remove from ~/.zshrc or ~/.bash_profile:"
	@echo ">>   export SURGE_STDLIB=$(SYSTEM_SHAREDIR)"
else
	@echo ">> Removing $(PROFILE_FILE) (requires sudo)"
	@sudo rm -f $(PROFILE_FILE)
endif
	@echo ">> System installation removed"

# ===== Bash Completion =====
# Генерация bash completion скрипта
# Использует установленный surge если доступен, иначе собирает локально
completion:
	@echo ">> Generating bash completion script"
	@if command -v surge >/dev/null 2>&1; then \
		echo ">> Using installed surge"; \
		surge completion bash > s.sh; \
	else \
		echo ">> Building surge locally"; \
		$(MAKE) build >/dev/null 2>&1; \
		./surge completion bash > s.sh; \
	fi
	@echo ">> Generated s.sh"

# Установка bash completion для текущего пользователя (не требует sudo)
# Устанавливает в ~/.bash_completion.d/ и добавляет source в ~/.bashrc если нужно
completion-install: completion
	@echo ">> Installing bash completion for current user"
	@mkdir -p ~/.bash_completion.d
	@cp s.sh ~/.bash_completion.d/surge
	@if ! grep -q "bash_completion.d/surge" ~/.bashrc 2>/dev/null; then \
		echo "" >> ~/.bashrc; \
		echo "# Surge bash completion" >> ~/.bashrc; \
		echo "source ~/.bash_completion.d/surge" >> ~/.bashrc; \
		echo ">> Added source to ~/.bashrc"; \
	else \
		echo ">> Already configured in ~/.bashrc"; \
	fi
	@echo ">> Bash completion installed to ~/.bash_completion.d/surge"
	@echo ">> Reload shell: source ~/.bashrc or restart terminal"

# Системная установка bash completion (требует sudo)
# Устанавливает в /etc/bash_completion.d/
completion-install-system: completion
	@echo ">> Installing bash completion system-wide (requires sudo)"
	@sudo mkdir -p /etc/bash_completion.d
	@sudo cp s.sh /etc/bash_completion.d/surge
	@echo ">> Installed to /etc/bash_completion.d/surge"
	@echo ">> Completion will be available after restarting terminal"

# `make run prog.sg --backend llvm` passes its trailing words to the program, and
# make would otherwise try to BUILD each of them, so they need a rule that does
# nothing. That rule used to be unconditional, and the cost was total: every
# unknown goal matched it, so `make this-does-not-exist` exited 0 with no output
# and a typo in a gate name was indistinguishable from a green gate. It is how
# `runtime-v2-carrier-sanitizer-check` stayed "passing" in three documents while
# having no target at all (RV2-DEBT-199, RV2-DEBT-200).
#
# Defining it only when `run` is the FIRST goal keeps the argument-swallowing
# where it was needed and gives every other unknown name back to make, which
# fails loudly. `run` is the only goal that reads MAKECMDGOALS.
ifeq ($(firstword $(MAKECMDGOALS)),run)
%:
	@:
endif
