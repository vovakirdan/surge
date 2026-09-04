"""Frozen semantic validation for the carrier benchmark manifest."""

from __future__ import annotations

from runtime_v2_carrier_bench_model import (
    Aggregation,
    AvailabilityStatus,
    Manifest,
    ManifestError,
    MetricSource,
)

FROZEN_METRIC_CONTRACT: dict[
    str, tuple[MetricSource, Aggregation, AvailabilityStatus]
] = {
    "allocation_count": ("fixture", "sum", "required"),
    "bytes_copied": ("runtime_exit", "sum", "unsupported"),
    "bytes_moved": ("runtime_exit", "sum", "unsupported"),
    "callback_count": ("runtime_exit", "sum", "unsupported"),
    "data_slot_stalls": ("runtime_exit", "sum", "unsupported"),
    "peak_transport_bytes": ("runtime_exit", "max", "unsupported"),
}


ROWS_WITH_OWN_BUDGET = frozenset({"select-send-scalar", "array-teardown-scalar"})


def validate_manifest(manifest: Manifest) -> None:
    if manifest.schema_version != 2:
        raise ManifestError(f"unsupported schema_version {manifest.schema_version}")
    protocol = manifest.protocol
    # Owner rulings 2026-09-04 (the file effect): twenty-four physically
    # distinct copies per side, each with one warmup and five measured
    # pairs; the side is scored on the median copy among those whose own
    # batches agree. Five pairs came after three read a CV of 0.055-0.061
    # on rows whose ratios were all in budget; the median came after the
    # fastest copy read select-send-scalar 0.960 / 0.939 / 0.898 on three
    # runs of one SHA pair (a single unusually fast base copy); twenty-four
    # copies came after the median of eight still swung +-3-4 % between runs
    # (0.968 / 0.983 / 0.930 on the same rows, copies of one side spread
    # +-10 % within a run), which puts a row at the budget line on a coin
    # toss. The numbers are the ruling's, not tunables.
    if protocol.warmups != 1 or protocol.measured_pairs != 5 or protocol.placements != 24:
        raise ManifestError(
            "protocol must freeze exactly 1 warmup, 5 measured pairs and 24 placements per side "
            "(owner ruling 2026-09-04)"
        )
    if protocol.max_cv != 0.05:
        raise ManifestError("protocol.max_cv must be exactly 0.05")
    if protocol.throughput_min_ratio != 0.95 or protocol.p95_max_ratio != 1.10:
        raise ManifestError("protocol relative budgets must be exactly 0.95 throughput / 1.10 p95")
    # Owner ruling 2026-09-04, variant "в": the p95 CV gate applies only where
    # the side's median p95 is at least ten timer ticks (the runner's latencies
    # are quantized in ~10 ns steps; a 60 ns row's p95 moves one tick and reads
    # 6 % CV of the clock, not of the runtime). Sub-microsecond rows are gated
    # on throughput CV alone. The number is the ruling's, not a tunable.
    if protocol.p95_cv_floor_ns != 1000:
        raise ManifestError("protocol.p95_cv_floor_ns must be exactly 1000 (owner ruling 2026-09-04)")
    if manifest.shards != 2 or manifest.threads != 2:
        raise ManifestError("benchmark topology must freeze shards=2 threads=2")
    if manifest.blocking_threads != 1:
        raise ManifestError("benchmark topology must freeze blocking_threads=1")
    # Two distinct cores, whichever host the manifest names: the fixture and
    # its blocking thread each get a core of their own and share nothing.
    # The rule used to spell the first reference host's "0,2"; it says what
    # it always meant now that the host is the dedicated runner (2026-09-03).
    cores = manifest.reference.cpuset.split(",")
    if len(cores) != 2 or any(not core.isdigit() for core in cores) or cores[0] == cores[1]:
        raise ManifestError(
            "benchmark reference cpuset must freeze two distinct cores, "
            f"got {manifest.reference.cpuset!r}"
        )
    metric_set = {metric.name for metric in manifest.metrics}
    if metric_set != set(FROZEN_METRIC_CONTRACT):
        raise ManifestError(
            "metrics must match the frozen six-metric contract: "
            f"missing={sorted(set(FROZEN_METRIC_CONTRACT) - metric_set)} "
            f"extra={sorted(metric_set - set(FROZEN_METRIC_CONTRACT))}"
        )
    for metric in manifest.metrics:
        source, aggregation, base_status = FROZEN_METRIC_CONTRACT[metric.name]
        if metric.source != source or metric.aggregation != aggregation:
            raise ManifestError(
                f"metric {metric.name} must use source={source} aggregation={aggregation}"
            )
        if metric.base.status != base_status or metric.candidate.status != "required":
            raise ManifestError(
                f"metric {metric.name} availability must be "
                f"base={base_status} candidate=required"
            )
        if (
            metric.base.status == "unsupported"
            and metric.base.provenance_commit != manifest.epic_base
        ):
            raise ManifestError(
                f"metric {metric.name} unsupported provenance must equal epic_base"
            )
    harness_set = {entry.path for entry in manifest.harness_files}
    if len(harness_set) != len(manifest.harness_files):
        raise ManifestError("harness file paths must be unique")
    fixture_set = {entry.path for entry in manifest.fixtures}
    if len(fixture_set) != len(manifest.fixtures):
        raise ManifestError("fixture paths must be unique")
    if manifest.allocation_control.fixture not in fixture_set:
        raise ManifestError("allocation_control references an unknown fixture")
    if manifest.allocation_control.expected_allocation_count != 1:
        raise ManifestError("allocation_control must freeze exactly one deliberate allocation")
    row_ids: set[str] = set()
    for row in manifest.rows:
        if row.row_id in row_ids:
            raise ManifestError(f"duplicate row id {row.row_id}")
        row_ids.add(row.row_id)
        if row.payload_role == "zero" and row.payload_bytes != 0:
            raise ManifestError(f"row {row.row_id} zero payload role must have size 0")
        if row.payload_role in {"scalar", "composite"} and row.payload_bytes == 0:
            raise ManifestError(
                f"row {row.row_id} {row.payload_role} payload role must have positive size"
            )
        if row.fixture not in fixture_set:
            raise ManifestError(f"row {row.row_id} references unknown fixture {row.fixture}")
        if set(row.required_metrics) != metric_set:
            raise ManifestError(f"row {row.row_id} must require the complete metric schema")
        for invariant in row.invariants:
            if invariant.metric == "allocation_count":
                raise ManifestError(
                    f"row {row.row_id} duplicates the exact allocation_count contract; "
                    "candidate_structural_allocations_per_batch is authoritative"
                )
            if invariant.metric not in metric_set:
                raise ManifestError(
                    f"row {row.row_id} invariant references unknown metric {invariant.metric}"
                )
            metric = next(item for item in manifest.metrics if item.name == invariant.metric)
            availability = metric.base if invariant.side == "base" else metric.candidate
            if availability.status == "unsupported":
                raise ManifestError(
                    f"row {row.row_id} invariant references unsupported "
                    f"{invariant.side} metric {invariant.metric}"
                )
        # Owner rulings 2026-09-04 (the sixth and seventh): two rows carry a
        # throughput budget of their own, select-send-scalar and
        # array-teardown-scalar at exactly 0.90 -- the front end's residual
        # and the file placement of a 40 us batch at -O0 (RV2-DEBT-333), the
        # arrays row equal to the base by instructions and cache simulation
        # and read at 0.94-1.03 across six attempts; every other row reads
        # the protocol's 0.95. The rows and the number are the rulings'.
        if row.throughput_min_ratio is not None and (
            row.row_id not in ROWS_WITH_OWN_BUDGET or row.throughput_min_ratio != 0.90
        ):
            raise ManifestError(
                f"row {row.row_id} carries a throughput budget of its own; only "
                "select-send-scalar and array-teardown-scalar may, at exactly 0.90 "
                "(owner rulings 2026-09-04)"
            )
    rows_by_id = {row.row_id: row for row in manifest.rows}
    for invariant in manifest.cross_row_invariants:
        if invariant.metric == "allocation_count":
            raise ManifestError(
                f"cross-row invariant {invariant.invariant_id} duplicates the exact "
                "allocation_count contract; per-row structural budgets are authoritative"
            )
        if invariant.left_row == invariant.right_row:
            raise ManifestError(
                f"cross-row invariant {invariant.invariant_id} must compare distinct rows"
            )
        if invariant.left_row not in rows_by_id or invariant.right_row not in rows_by_id:
            raise ManifestError(
                f"cross-row invariant {invariant.invariant_id} references unknown row"
            )
        if invariant.metric not in metric_set:
            raise ManifestError(
                f"cross-row invariant {invariant.invariant_id} references unknown metric"
            )
        metric = next(item for item in manifest.metrics if item.name == invariant.metric)
        availability = metric.base if invariant.side == "base" else metric.candidate
        if availability.status == "unsupported":
            raise ManifestError(
                f"cross-row invariant {invariant.invariant_id} references unsupported metric"
            )
        left = rows_by_id[invariant.left_row]
        right = rows_by_id[invariant.right_row]
        if (
            invariant.metric not in left.required_metrics
            or invariant.metric not in right.required_metrics
        ):
            raise ManifestError(
                f"cross-row invariant {invariant.invariant_id} metric must be required "
                "by both rows"
            )
        if (
            left.operations_per_batch != right.operations_per_batch
            or left.batches != right.batches
        ):
            raise ManifestError(
                f"cross-row invariant {invariant.invariant_id} requires matched workloads"
            )
        if left.workload_family != right.workload_family:
            raise ManifestError(
                f"cross-row invariant {invariant.invariant_id} requires one workload family"
            )
        if invariant.relation == "paired_payload":
            if (
                invariant.metric != "allocation_count"
                or left.payload_role != "composite"
                or right.payload_role != "scalar"
                or invariant.side != "candidate"
                or invariant.left_reduction != "max"
                or invariant.operator != "le"
                or invariant.right_reduction != "min"
            ):
                raise ManifestError(
                    f"cross-row invariant {invariant.invariant_id} paired_payload must "
                    "compare candidate max(composite allocation_count) <= min(scalar)"
                )
        elif (
            invariant.metric not in {"bytes_copied", "bytes_moved"}
            or left.payload_role != "composite"
            or right.payload_role != "composite"
            or left.payload_bytes == right.payload_bytes
            or invariant.side != "candidate"
            or invariant.operator != "eq"
            or invariant.left_reduction != "max"
            or invariant.right_reduction != "max"
        ):
            raise ManifestError(
                f"cross-row invariant {invariant.invariant_id} payload_proportional "
                "must pointwise-eq unequal candidate composite byte payloads"
            )
    # OWNER RULING 2026-09-03: the runtime-exit metrics -- bytes copied and
    # moved, callback counts, data-slot stalls, the peak of transport bytes --
    # are reported telemetry, not gates. A payload-derived byte window or a
    # stall count asserts the byte-credit model the 2026-08-29 ruling withdrew,
    # and base and candidate share this manifest, so an assertion here must mean
    # the same thing on both sides. Contention is proven by dedicated
    # deterministic stands, not by a number in a throughput benchmark.
    telemetry = {metric.name for metric in manifest.metrics if metric.source == "runtime_exit"}
    for row in manifest.rows:
        for invariant in row.invariants:
            if invariant.metric in telemetry:
                raise ManifestError(
                    f"row {row.row_id} asserts {invariant.metric}, which is reported "
                    "telemetry and not a gate (owner ruling 2026-09-03)"
                )
    for invariant in manifest.cross_row_invariants:
        if invariant.metric in telemetry:
            raise ManifestError(
                f"cross-row invariant {invariant.invariant_id} asserts {invariant.metric}, "
                "which is reported telemetry and not a gate (owner ruling 2026-09-03)"
            )
    # Owner ruling 2026-09-04: the manifest carries NO liveness probe, and the
    # two it used to freeze are withdrawn. They waited on
    # SP_CARRIER_JUMBO_ADMITTED -- a point of the byte-credit model the
    # 2026-08-29 ruling withdrew, which no RT_SYNC_POINT ever reached -- so
    # their fixture spun on a flag that could not turn and timed out ten
    # seconds at a time; and nothing anywhere emitted the SURGE_CARRIER_LIVENESS
    # record their parser reads, so no probe of that shape could ever have
    # passed. Meanwhile the report refuses a final phase that carries a
    # deferred probe, which together made a green final benchmark impossible.
    # The property they were to show -- a producer parks on an exhausted data
    # lane and a freed slot wakes it -- is held by E4's behaviour row
    # anchored-saturation-parks-the-producer-and-a-freed-slot-wakes-it
    # (parks=1, wakes=1) with its Rule-13 control. A probe naming a point the
    # runtime reaches may be added back; this refusal is what keeps a
    # never-armed one from coming back by inertia.
    if manifest.liveness_probes:
        raise ManifestError(
            "liveness probes must be empty: the withdrawn large-payload park rows "
            "waited on a point nothing arms and no code emits their record "
            "(owner ruling 2026-09-04)"
        )
    # The rules that used to shape a probe -- its fixture, its Wave-A
    # deferral, its provenance, its reply-slot balance, the point it waits on
    # -- are gone with the probes: a rule that can never be reached is not a
    # rule, and the one above refuses every probe. A row added back brings its
    # own, written against the point it actually waits on.
