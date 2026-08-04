"""Frozen semantic validation for the carrier benchmark manifest."""

from __future__ import annotations

from runtime_v2_carrier_bench_model import (
    Aggregation,
    AvailabilityStatus,
    Manifest,
    ManifestError,
    MetricSource,
    TransportBudget,
)

FROZEN_METRIC_CONTRACT: dict[
    str, tuple[MetricSource, Aggregation, AvailabilityStatus]
] = {
    "allocation_count": ("fixture", "sum", "required"),
    "bytes_copied": ("runtime_exit", "sum", "unsupported"),
    "bytes_moved": ("runtime_exit", "sum", "unsupported"),
    "callback_count": ("runtime_exit", "sum", "unsupported"),
    "credit_stalls": ("runtime_exit", "sum", "unsupported"),
    "peak_transport_bytes": ("runtime_exit", "max", "unsupported"),
}


def validate_manifest(manifest: Manifest) -> None:
    if manifest.schema_version != 2:
        raise ManifestError(f"unsupported schema_version {manifest.schema_version}")
    protocol = manifest.protocol
    if protocol.warmups != 2 or protocol.measured_pairs != 7:
        raise ManifestError("protocol must freeze exactly 2 warmups and 7 measured pairs")
    if protocol.max_cv != 0.05:
        raise ManifestError("protocol.max_cv must be exactly 0.05")
    if protocol.throughput_min_ratio != 0.95 or protocol.p95_max_ratio != 1.10:
        raise ManifestError("protocol relative budgets must be exactly 0.95 throughput / 1.10 p95")
    if manifest.transport != TransportBudget(4096, 1024, 4096, 256):
        raise ManifestError(
            "transport budget must freeze data=4096 control=1024 "
            "jumbo_threshold=4096 max_inline_overhead=256"
        )
    if manifest.shards != 2 or manifest.threads != 2:
        raise ManifestError("benchmark topology must freeze shards=2 threads=2")
    if manifest.blocking_threads != 1:
        raise ManifestError("benchmark topology must freeze blocking_threads=1")
    if manifest.reference.cpuset != "0,2":
        raise ManifestError("benchmark reference cpuset must freeze distinct cores 0,2")
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
    expected_liveness = {"jumbo-credit-cancel", "jumbo-global-shutdown"}
    actual_liveness = {probe.probe_id for probe in manifest.liveness_probes}
    if actual_liveness != expected_liveness:
        raise ManifestError(
            "liveness probes must freeze cancel and global-shutdown jumbo rows"
        )
    for probe in manifest.liveness_probes:
        if probe.fixture not in fixture_set:
            raise ManifestError(
                f"liveness probe {probe.probe_id} references unknown fixture"
            )
        if probe.wave_a.status != "deferred" or probe.final.status != "required":
            raise ManifestError(
                f"liveness probe {probe.probe_id} must be Wave-A deferred and final required"
            )
        if probe.wave_a.provenance_commit != manifest.epic_base:
            raise ManifestError(
                f"liveness probe {probe.probe_id} deferred provenance must equal epic_base"
            )
        if probe.expected_credit_balance != 0:
            raise ManifestError(
                f"liveness probe {probe.probe_id} must restore exact zero credit balance"
            )
        if probe.syncpoint != "SP_CARRIER_CREDIT_PARKED":
            raise ManifestError(
                f"liveness probe {probe.probe_id} must wait on "
                "SP_CARRIER_CREDIT_PARKED"
            )
        if probe.min_peak_transport_bytes != probe.payload_bytes:
            raise ManifestError(
                f"liveness probe {probe.probe_id} peak lower bound must equal payload size"
            )
        expected_max = (
            probe.payload_bytes
            + manifest.transport.max_inline_overhead_bytes
            + manifest.transport.control_bytes
        )
        if probe.max_peak_transport_bytes != expected_max:
            raise ManifestError(
                f"liveness probe {probe.probe_id} peak upper bound must be {expected_max}"
            )
