"""Typed manifest and scoring model for the Runtime V2 carrier benchmark."""

from __future__ import annotations

import json
import math
import statistics
from dataclasses import dataclass
from typing import Literal, Mapping, Sequence

Side = Literal["base", "candidate"]
Operator = Literal["eq", "le", "ge"]
Reduction = Literal["min", "max"]
CrossRelation = Literal["paired_payload", "payload_proportional"]
PayloadRole = Literal["zero", "scalar", "composite", "control"]
Aggregation = Literal["sum", "max", "last"]
AvailabilityStatus = Literal["required", "unsupported"]
LivenessStatus = Literal["required", "deferred"]
MetricSource = Literal["fixture", "runtime_exit"]


class ManifestError(ValueError):
    """The frozen benchmark manifest is malformed or stale."""


class GateFailure(RuntimeError):
    """A measured benchmark session violates its frozen contract."""


class StrictJSONError(ValueError):
    """JSON input is syntactically valid but not canonical enough for the gate."""


def strict_json_loads(text: str) -> object:
    def reject_duplicate_keys(pairs: list[tuple[str, object]]) -> dict[str, object]:
        result: dict[str, object] = {}
        for key, value in pairs:
            if key in result:
                raise StrictJSONError(f"duplicate JSON key {key!r}")
            result[key] = value
        return result

    def reject_non_finite(value: str) -> object:
        raise StrictJSONError(f"non-finite JSON number {value}")

    return json.loads(
        text,
        object_pairs_hook=reject_duplicate_keys,
        parse_constant=reject_non_finite,
    )


@dataclass(frozen=True, slots=True)
class FileDigest:
    path: str
    sha256: str


@dataclass(frozen=True, slots=True)
class ReferenceHost:
    system: str
    machine: str
    kernel_contains: str
    cpu_model: str
    logical_cpus: int
    cpuset: str
    go_version: str
    clang_version: str


@dataclass(frozen=True, slots=True)
class Protocol:
    warmups: int
    measured_pairs: int
    max_cv: float
    throughput_min_ratio: float
    p95_max_ratio: float
    percentile_method: str
    cv_method: str


@dataclass(frozen=True, slots=True)
class TransportBudget:
    data_bytes: int
    control_bytes: int
    jumbo_threshold_bytes: int
    max_inline_overhead_bytes: int


@dataclass(frozen=True, slots=True)
class MetricAvailability:
    status: AvailabilityStatus
    reason: str | None = None
    provenance_commit: str | None = None


@dataclass(frozen=True, slots=True)
class Metric:
    name: str
    aggregation: Aggregation
    source: MetricSource
    base: MetricAvailability
    candidate: MetricAvailability


@dataclass(frozen=True, slots=True)
class Invariant:
    metric: str
    operator: Operator
    value: int
    side: Side


@dataclass(frozen=True, slots=True)
class Row:
    row_id: str
    workload_family: str
    payload_role: PayloadRole
    fixture: str
    probe: str
    operations_per_batch: int
    batches: int
    payload_bytes: int
    timeout_seconds: int
    relative_performance: bool
    expected_checksum: str
    candidate_structural_allocations_per_batch: int
    required_metrics: tuple[str, ...]
    invariants: tuple[Invariant, ...]


@dataclass(frozen=True, slots=True)
class AllocationControl:
    fixture: str
    probe: str
    expected_checksum: str
    expected_allocation_count: int


@dataclass(frozen=True, slots=True)
class CrossRowInvariant:
    invariant_id: str
    relation: CrossRelation
    metric: str
    left_row: str
    left_reduction: Reduction
    operator: Operator
    right_row: str
    right_reduction: Reduction
    side: Side


@dataclass(frozen=True, slots=True)
class LivenessAvailability:
    status: LivenessStatus
    reason: str | None = None
    provenance_commit: str | None = None


@dataclass(frozen=True, slots=True)
class LivenessProbe:
    probe_id: str
    fixture: str
    probe: str
    syncpoint: str
    payload_bytes: int
    timeout_seconds: int
    expected_credit_balance: int
    min_peak_transport_bytes: int
    max_peak_transport_bytes: int
    expected_park_transitions: int
    wave_a: LivenessAvailability
    final: LivenessAvailability


@dataclass(frozen=True, slots=True)
class Manifest:
    schema_version: int
    epic_base: str
    reference: ReferenceHost
    protocol: Protocol
    transport: TransportBudget
    backend: str
    profile: str
    shards: int
    threads: int
    blocking_threads: int
    metrics: tuple[Metric, ...]
    allocation_control: AllocationControl
    harness_files: tuple[FileDigest, ...]
    fixtures: tuple[FileDigest, ...]
    rows: tuple[Row, ...]
    cross_row_invariants: tuple[CrossRowInvariant, ...]
    liveness_probes: tuple[LivenessProbe, ...]


@dataclass(frozen=True, slots=True)
class CounterSample:
    values: Mapping[str, int | None]


@dataclass(frozen=True, slots=True)
class TimingSample:
    elapsed_ns: int
    operation_latencies_ns: tuple[int, ...]
    operations: int

    def throughput(self) -> float:
        if self.elapsed_ns <= 0:
            raise GateFailure("timing sample has non-positive elapsed time")
        if self.operations <= 0 or len(self.operation_latencies_ns) != self.operations:
            raise GateFailure("timing sample operation/latency count mismatch")
        if any(value <= 0 for value in self.operation_latencies_ns):
            raise GateFailure("timing sample has non-positive operation latency")
        return self.operations * 1_000_000_000.0 / self.elapsed_ns

    def p50_ns(self) -> float:
        return nearest_rank(self.operation_latencies_ns, 0.50)

    def p95_ns(self) -> float:
        return nearest_rank(self.operation_latencies_ns, 0.95)


@dataclass(frozen=True, slots=True)
class MeasuredRun:
    side: Side
    pair_index: int
    timing: TimingSample
    counters: CounterSample


@dataclass(frozen=True, slots=True)
class SideScore:
    throughput: float
    p50_ns: float
    p95_ns: float
    throughput_cv: float
    p95_cv: float


def paired_order(row_index: int, pair_index: int) -> tuple[Side, Side]:
    if row_index < 0 or pair_index < 0:
        raise ValueError("row and pair indexes must be non-negative")
    base_first = (row_index + pair_index) % 2 == 0
    return ("base", "candidate") if base_first else ("candidate", "base")


def nearest_rank(values: Sequence[int | float], percentile: float) -> float:
    if not values:
        raise GateFailure("cannot score an empty sample")
    if not 0.0 < percentile <= 1.0:
        raise ValueError(f"percentile must be in (0, 1], got {percentile}")
    ordered = sorted(values)
    rank = max(1, math.ceil(percentile * len(ordered)))
    return float(ordered[rank - 1])


def sample_cv(values: Sequence[float]) -> float:
    if len(values) < 2:
        raise GateFailure("sample CV requires at least two measurements")
    mean = statistics.fmean(values)
    if mean <= 0:
        raise GateFailure("sample CV mean must be positive")
    return statistics.stdev(values) / mean


def score_side(runs: Sequence[MeasuredRun]) -> SideScore:
    if not runs:
        raise GateFailure("cannot score an empty side")
    side = runs[0].side
    if any(run.side != side for run in runs):
        raise GateFailure("side score mixes base and candidate runs")
    throughputs = [run.timing.throughput() for run in runs]
    p50s = [run.timing.p50_ns() for run in runs]
    p95s = [run.timing.p95_ns() for run in runs]
    return SideScore(
        throughput=float(statistics.median(throughputs)),
        p50_ns=float(statistics.median(p50s)),
        p95_ns=float(statistics.median(p95s)),
        throughput_cv=sample_cv(throughputs),
        p95_cv=sample_cv(p95s),
    )


def validate_row_results(
    manifest: Manifest, row: Row, base: Sequence[MeasuredRun], candidate: Sequence[MeasuredRun]
) -> tuple[SideScore, SideScore]:
    scores = validate_row_protocol(manifest, row, base, candidate)
    failures = row_invariant_failures(row, base, candidate)
    if failures:
        raise GateFailure(failures[0])
    return scores


def validate_row_protocol(
    manifest: Manifest, row: Row, base: Sequence[MeasuredRun], candidate: Sequence[MeasuredRun]
) -> tuple[SideScore, SideScore]:
    expected = manifest.protocol.measured_pairs
    _validate_run_set(row, "base", base, expected)
    _validate_run_set(row, "candidate", candidate, expected)
    base_score = score_side(base)
    candidate_score = score_side(candidate)
    for label, score in (("base", base_score), ("candidate", candidate_score)):
        if score.throughput_cv > manifest.protocol.max_cv:
            raise GateFailure(
                f"{row.row_id} {label} throughput CV {score.throughput_cv:.6f} "
                f"exceeds {manifest.protocol.max_cv:.6f}"
            )
        if score.p95_cv > manifest.protocol.max_cv:
            raise GateFailure(
                f"{row.row_id} {label} p95 CV {score.p95_cv:.6f} "
                f"exceeds {manifest.protocol.max_cv:.6f}"
            )
    if row.relative_performance:
        throughput_ratio = candidate_score.throughput / base_score.throughput
        latency_ratio = candidate_score.p95_ns / base_score.p95_ns
        if throughput_ratio < manifest.protocol.throughput_min_ratio:
            raise GateFailure(
                f"{row.row_id} throughput ratio {throughput_ratio:.6f} below "
                f"{manifest.protocol.throughput_min_ratio:.6f}"
            )
        if latency_ratio > manifest.protocol.p95_max_ratio:
            raise GateFailure(
                f"{row.row_id} p95 ratio {latency_ratio:.6f} above "
                f"{manifest.protocol.p95_max_ratio:.6f}"
            )
    return base_score, candidate_score


def row_invariant_failures(
    row: Row, base: Sequence[MeasuredRun], candidate: Sequence[MeasuredRun]
) -> tuple[str, ...]:
    failures: list[str] = []
    for run in (*base, *candidate):
        for invariant in row.invariants:
            if invariant.side != run.side:
                continue
            try:
                _check_invariant(row, run, invariant)
            except GateFailure as err:
                failures.append(str(err))
    return tuple(failures)


def aggregate_counters(
    metrics: Sequence[Metric], samples: Sequence[Mapping[str, int | None]], side: Side
) -> CounterSample:
    if not samples:
        raise GateFailure("cannot aggregate an empty counter sample")
    expected = {metric.name for metric in metrics}
    for index, sample in enumerate(samples):
        actual = set(sample)
        if actual != expected:
            raise GateFailure(
                f"counter batch {index} metrics mismatch: missing={sorted(expected - actual)} "
                f"extra={sorted(actual - expected)}"
            )
        for name, value in sample.items():
            if value is not None and (
                isinstance(value, bool) or not isinstance(value, int) or value < 0
            ):
                raise GateFailure(f"counter batch {index} {name} must be a non-negative integer")
    values: dict[str, int | None] = {}
    for metric in metrics:
        observed = [sample[metric.name] for sample in samples]
        availability = metric.base if side == "base" else metric.candidate
        if availability.status == "unsupported":
            if any(value is not None for value in observed):
                raise GateFailure(
                    f"{side} metric {metric.name} is unsupported and must be reported as null"
                )
            values[metric.name] = None
            continue
        if any(value is None for value in observed):
            raise GateFailure(f"{side} metric {metric.name} is required but reported as null")
        numeric = [value for value in observed if value is not None]
        values[metric.name] = {
            "sum": sum(numeric),
            "max": max(numeric),
            "last": numeric[-1],
        }[metric.aggregation]
    return CounterSample(values)


def _validate_run_set(row: Row, side: Side, runs: Sequence[MeasuredRun], expected: int) -> None:
    if len(runs) != expected:
        raise GateFailure(f"{row.row_id} {side} has {len(runs)} runs, want {expected}")
    if sorted(run.pair_index for run in runs) != list(range(expected)):
        raise GateFailure(f"{row.row_id} {side} pair indexes are incomplete or duplicated")
    required = set(row.required_metrics)
    for run in runs:
        if run.side != side:
            raise GateFailure(f"{row.row_id} expected {side} run, got {run.side}")
        expected_operations = row.operations_per_batch * row.batches
        if run.timing.operations != expected_operations:
            raise GateFailure(f"{row.row_id} {side} measured operation count drifted")
        if len(run.timing.operation_latencies_ns) != expected_operations:
            raise GateFailure(f"{row.row_id} {side} operation latency count drifted")
        actual = set(run.counters.values)
        if actual != required:
            raise GateFailure(
                f"{row.row_id} {side} metrics mismatch: missing={sorted(required - actual)} "
                f"extra={sorted(actual - required)}"
            )
def _check_invariant(row: Row, run: MeasuredRun, invariant: Invariant) -> None:
    actual = run.counters.values[invariant.metric]
    if actual is None:
        raise GateFailure(
            f"{row.row_id} {run.side} pair {run.pair_index}: "
            f"{invariant.metric} is unsupported but has an invariant"
        )
    passed = {
        "eq": actual == invariant.value,
        "le": actual <= invariant.value,
        "ge": actual >= invariant.value,
    }[invariant.operator]
    if not passed:
        raise GateFailure(
            f"{row.row_id} {run.side} pair {run.pair_index}: {invariant.metric}={actual} "
            f"violates {invariant.operator} {invariant.value}"
        )
