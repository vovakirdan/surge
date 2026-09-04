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


def validate_manifest(manifest: Manifest) -> None:
    if manifest.schema_version != 2:
        raise ManifestError(f"unsupported schema_version {manifest.schema_version}")
    protocol = manifest.protocol
    # Owner ruling 2026-09-04 (the file effect): six physically distinct
    # copies per side, each with one warmup and five measured pairs; the
    # side is scored on the fastest copy whose own batches agree. Six copies
    # leave a chance of about one in a thousand that every copy drew a slow
    # placement (a third of the copies did on the runner); five pairs are
    # the second half of the ruling, after three read a CV of 0.055-0.061 on
    # rows whose ratios were all in budget. The numbers are the ruling's,
    # not tunables.
    if protocol.warmups != 1 or protocol.measured_pairs != 5 or protocol.placements != 6:
        raise ManifestError(
            "protocol must freeze exactly 1 warmup, 5 measured pairs and 6 placements per side "
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
    expected_liveness = {"large-payload-park-cancel", "large-payload-park-shutdown"}
    actual_liveness = {probe.probe_id for probe in manifest.liveness_probes}
    if actual_liveness != expected_liveness:
        raise ManifestError(
            "liveness probes must freeze the large-payload park's cancel and shutdown rows"
        )
    for probe in manifest.liveness_probes:
        if probe.fixture not in fixture_set:
            raise ManifestError(
                f"liveness probe {probe.probe_id} references unknown fixture"
            )
        if probe.wave_a.status != "deferred":
            raise ManifestError(
                f"liveness probe {probe.probe_id} must be Wave-A deferred"
            )
        # THE FINAL PHASE MAY DEFER, AND ONLY WHILE THE POINT IS UNARMED. The
        # rule used to say final is always required, which is what a probe
        # SHOULD be; it is false today because nothing reaches the point these
        # two wait on -- admission parks a sender on an exhausted data-slot
        # budget and the tree still drain-and-retries -- and a probe waiting on
        # a point nothing reaches does not fail, it times out ten seconds at a
        # time in the middle of a benchmark.
        #
        # This pairs with the sync-point rule below and neither is redundant:
        # that one pins the NAME, this one ties the licence to defer to that
        # exact name. The day the far-carrier work arms the park and the probes
        # move to a point that is reached, the name changes, this rule stops
        # granting the licence, and final goes back to required by refusal
        # rather than by anyone remembering.
        unarmed = "SP_TRANSPORT_DATA_SLOT_TASK_PARKED"
        if probe.final.status == "deferred" and probe.syncpoint != unarmed:
            raise ManifestError(
                f"liveness probe {probe.probe_id} may defer its final phase only "
                f"while it waits on {unarmed}, which nothing arms"
            )
        for phase_name, availability in (
            ("wave_a", probe.wave_a),
            ("final", probe.final),
        ):
            if (
                availability.status == "deferred"
                and availability.provenance_commit != manifest.epic_base
            ):
                raise ManifestError(
                    f"liveness probe {probe.probe_id} {phase_name} deferred "
                    "provenance must equal epic_base"
                )
        if probe.expected_reply_reserved != 0:
            raise ManifestError(
                f"liveness probe {probe.probe_id} must leave every reply-slot reservation "
                "given back (expected_reply_reserved must be zero)"
            )
        if probe.syncpoint != unarmed:
            raise ManifestError(
                f"liveness probe {probe.probe_id} must wait on {unarmed}"
            )
