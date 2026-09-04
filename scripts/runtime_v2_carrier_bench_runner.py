"""Build and execute frozen Runtime V2 carrier benchmark fixtures."""

from __future__ import annotations

import hashlib
import secrets
import shutil
from dataclasses import dataclass, replace
from pathlib import Path
from typing import Mapping, Sequence

from runtime_v2_carrier_bench_host import run_checked
from runtime_v2_carrier_bench_ir import (
    verify_carrier_binary as _verify_carrier_binary,
    verify_emitted_ir as _verify_emitted_ir,
    verify_fixture_ir as _verify_fixture_ir,
    verify_fixture_source as _verify_fixture_source,
)
from runtime_v2_carrier_bench_model import (
    CounterSample,
    GateFailure,
    LivenessProbe,
    Manifest,
    MeasuredRun,
    Row,
    Side,
    TimingSample,
    aggregate_counters,
    paired_order,
)
from runtime_v2_carrier_bench_protocol import (
    BatchResult,
    LIVENESS_PREFIX,
    LivenessRecord,
    RESULT_PREFIX,
    RUNTIME_COUNTER_PREFIX,
    _built_binary,
    _parse_liveness_record,
    _parse_result,
    _parse_runtime_counters,
)


@dataclass(frozen=True, slots=True)
class RunRecord:
    measured: MeasuredRun
    batches: tuple[BatchResult, ...]
    resource_batches: tuple[BatchResult, ...] = ()


@dataclass(frozen=True, slots=True)
class BuiltFixture:
    binary: Path
    source_path: str


@dataclass(frozen=True, slots=True)
class AllocationMismatch:
    row_id: str
    phase: str
    warmup_index: int | None
    pair_index: int | None
    batch_index: int
    actual: int
    expected: int

    def message(self) -> str:
        run_label = (
            f"warmup {self.warmup_index}"
            if self.phase == "warmup"
            else f"pair {self.pair_index}"
        )
        return (
            f"{self.row_id} candidate {run_label} batch {self.batch_index}: "
            f"allocation_count={self.actual}, want exact structural budget {self.expected}"
        )


@dataclass(frozen=True, slots=True)
class TimingExecution:
    records: dict[str, dict[Side, tuple[RunRecord, ...]]]
    allocation_controls: dict[Side, BatchResult]
    allocation_mismatches: tuple[AllocationMismatch, ...]
    event_start: int
    next_ordinal: int


def build_surge(root: Path, output: Path) -> None:
    output.parent.mkdir(parents=True, exist_ok=True)
    run_checked(
        ["go", "build", "-trimpath", "-o", str(output), "./cmd/surge"],
        cwd=root,
        timeout_seconds=600,
    )


def build_fixtures(
    *,
    side_root: Path,
    harness_root: Path,
    surge: Path,
    manifest: Manifest,
    build_root: Path,
    capture_kind: str = "timing",
    include_allocation_control: bool = False,
) -> dict[str, BuiltFixture]:
    fixtures: dict[str, BuiltFixture] = {}
    source_paths = {row.fixture for row in manifest.rows}
    if include_allocation_control:
        source_paths.add(manifest.allocation_control.fixture)
    source_paths = sorted(source_paths)
    for index, source_path in enumerate(source_paths):
        source = harness_root / source_path
        package_copy = build_root / f"fixture-{index:02d}"
        copied_source, copied_package = _copy_fixture_package(source, package_copy)
        if not copied_source.is_file():
            raise GateFailure(f"fixture source is missing after copy: {source_path}")
        _verify_fixture_source(copied_source, source_path)
        result = run_checked(
            [
                str(surge),
                "build",
                "--release",
                f"--backend={manifest.backend}",
                "--ui=off",
                "--emit-llvm",
                "--keep-tmp",
                str(copied_source),
            ],
            cwd=package_copy,
            timeout_seconds=600,
            environment={
                **(
                    {"SURGE_INTERNAL_CARRIER_BENCH_COUNTERS": "1"}
                    if capture_kind == "resource"
                    else {}
                ),
                "SURGE_STDLIB": str(side_root),
            },
        )
        binary = _built_binary(result.stdout, package_copy)
        _verify_fixture_ir(binary, source_path)
        _verify_carrier_binary(binary, source_path, capture_kind)
        fixtures[source_path] = BuiltFixture(binary=binary, source_path=source_path)
    return fixtures


def build_liveness_fixtures(
    *,
    side_root: Path,
    harness_root: Path,
    surge: Path,
    manifest: Manifest,
    build_root: Path,
) -> dict[str, BuiltFixture]:
    fixtures: dict[str, BuiltFixture] = {}
    source_paths = sorted({probe.fixture for probe in manifest.liveness_probes})
    for index, source_path in enumerate(source_paths):
        source = harness_root / source_path
        package_copy = build_root / f"liveness-{index:02d}"
        copied_source, copied_package = _copy_fixture_package(source, package_copy)
        if not copied_source.is_file():
            raise GateFailure(
                f"liveness fixture source is missing after copy: {source_path}"
            )
        result = run_checked(
            [
                str(surge),
                "build",
                "--release",
                f"--backend={manifest.backend}",
                "--ui=off",
                str(copied_source),
            ],
            cwd=package_copy,
            timeout_seconds=600,
            environment={
                "SURGE_INTERNAL_TEST_SYNC_POINTS": "1",
                "SURGE_STDLIB": str(side_root),
            },
        )
        fixtures[source_path] = BuiltFixture(
            binary=_built_binary(result.stdout, package_copy),
            source_path=source_path,
        )
    return fixtures


def _copy_fixture_package(source: Path, destination: Path) -> tuple[Path, Path]:
    package = destination / "package"
    shutil.copytree(source.parent, package)
    shared_source = source.parent.parent / "shared"
    if shared_source.is_dir():
        shutil.copytree(shared_source, package / "shared")
    return package / source.name, package


def execute_manifest(
    manifest: Manifest,
    timing_binaries: Mapping[Side, Mapping[str, BuiltFixture]],
    candidate_resource_binaries: Mapping[str, BuiltFixture],
    events: list[dict[str, object]],
    protocol_sha256: str,
) -> tuple[
    dict[str, dict[Side, tuple[RunRecord, ...]]],
    dict[Side, BatchResult],
]:
    timing = execute_timing_manifest(
        manifest,
        timing_binaries,
        events,
        protocol_sha256,
    )
    records = execute_resource_manifest(
        manifest,
        timing,
        candidate_resource_binaries,
        events,
        protocol_sha256,
    )
    return records, timing.allocation_controls


def execute_timing_manifest(
    manifest: Manifest,
    timing_binaries: Mapping[Side, Mapping[str, BuiltFixture]],
    events: list[dict[str, object]],
    protocol_sha256: str,
    *,
    capture_expected_endpoint_red: bool = False,
) -> TimingExecution:
    event_start = len(events)
    ordinal = 0
    allocation_mismatches: list[AllocationMismatch] = []
    control_row = _allocation_control_row(manifest)
    controls: dict[Side, BatchResult] = {}
    for side in ("base", "candidate"):
        controls[side] = _run_recorded_batch(
            manifest,
            control_row,
            side,
            timing_binaries[side][control_row.fixture],
            events,
            phase="control",
            run_index=0,
            batch_index=0,
            protocol_sha256=protocol_sha256,
            capture_kind="timing",
            ordinal=ordinal,
        )
        _validate_allocation_control(manifest, side, controls[side])
        ordinal += 1

    placed = place_fixture_copies(timing_binaries, manifest.protocol.placements)
    records: dict[str, dict[Side, tuple[RunRecord, ...]]] = {}
    for row_index, row in enumerate(manifest.rows):
        per_side: dict[Side, list[RunRecord]] = {"base": [], "candidate": []}
        for placement in range(manifest.protocol.placements):
            fixtures: dict[Side, BuiltFixture] = {
                side: placed[side][row.fixture][placement] for side in ("base", "candidate")
            }
            ordinal = _run_warmups(
                manifest,
                row_index,
                row,
                fixtures,
                placement,
                events,
                protocol_sha256,
                ordinal,
                capture_expected_endpoint_red,
                allocation_mismatches,
            )
            for local_pair in range(manifest.protocol.measured_pairs):
                pair_index = placement * manifest.protocol.measured_pairs + local_pair
                for side in paired_order(row_index, pair_index):
                    per_side[side].append(
                        _run_measured_timing(
                            manifest,
                            row,
                            side,
                            pair_index,
                            placement,
                            fixtures[side],
                            events,
                            protocol_sha256,
                            ordinal,
                            capture_expected_endpoint_red,
                            allocation_mismatches,
                        )
                    )
                    ordinal += row.batches
        records[row.row_id] = {
            "base": tuple(
                _finalize_timing_record(manifest, record, "base")
                for record in per_side["base"]
            ),
            "candidate": tuple(per_side["candidate"]),
        }

    _validate_timing_attempt_sequence(manifest, events[event_start:])
    return TimingExecution(
        records=records,
        allocation_controls=controls,
        allocation_mismatches=tuple(allocation_mismatches),
        event_start=event_start,
        next_ordinal=ordinal,
    )


def execute_resource_manifest(
    manifest: Manifest,
    timing: TimingExecution,
    candidate_resource_binaries: Mapping[str, BuiltFixture],
    events: list[dict[str, object]],
    protocol_sha256: str,
) -> dict[str, dict[Side, tuple[RunRecord, ...]]]:
    expected_event_count = timing.event_start + len(
        _expected_timing_attempt_sequence(manifest)
    )
    if len(events) != expected_event_count:
        raise GateFailure("attempt events changed between timing and resource capture")
    ordinal = timing.next_ordinal
    records = {
        row_id: {"base": sides["base"], "candidate": sides["candidate"]}
        for row_id, sides in timing.records.items()
    }

    for row in manifest.rows:
        merged: list[RunRecord] = []
        for pair_index, timing_record in enumerate(records[row.row_id]["candidate"]):
            resource_batches: list[BatchResult] = []
            for batch_index in range(row.batches):
                resource_batches.append(
                    _run_recorded_batch(
                        manifest,
                        row,
                        "candidate",
                        candidate_resource_binaries[row.fixture],
                        events,
                        phase="measured",
                        run_index=pair_index,
                        batch_index=batch_index,
                        protocol_sha256=protocol_sha256,
                        capture_kind="resource",
                        ordinal=ordinal,
                        placement=pair_index // manifest.protocol.measured_pairs,
                    )
                )
                ordinal += 1
            merged.append(
                _merge_resource_record(
                    manifest, row, timing_record, tuple(resource_batches)
                )
            )
        records[row.row_id]["candidate"] = tuple(merged)

    _validate_attempt_sequence(manifest, events[timing.event_start:])
    return records


def _allocation_control_row(manifest: Manifest) -> Row:
    control = manifest.allocation_control
    return Row(
        row_id="allocation-control",
        workload_family="allocation-control",
        payload_role="control",
        fixture=control.fixture,
        probe=control.probe,
        # The control fixture runs the same 512-operation batch as every
        # scored row (owner ruling 2026-09-04); its one deliberate allocation
        # and checksum "1" do not depend on the count.
        operations_per_batch=512,
        batches=1,
        payload_bytes=0,
        timeout_seconds=30,
        relative_performance=False,
        expected_checksum=control.expected_checksum,
        candidate_structural_allocations_per_batch=control.expected_allocation_count,
        required_metrics=tuple(metric.name for metric in manifest.metrics),
        invariants=(),
    )


def _validate_allocation_control(
    manifest: Manifest, side: Side, result: BatchResult
) -> None:
    actual = result.counters.get("allocation_count")
    expected = manifest.allocation_control.expected_allocation_count
    if actual != expected:
        raise GateFailure(
            f"allocation control {side} allocation_count={actual}, want exact {expected}"
        )


def execute_liveness_probes(
    manifest: Manifest,
    candidate_binaries: Mapping[str, BuiltFixture],
    events: list[dict[str, object]],
    protocol_sha256: str,
    phase: str,
) -> tuple[LivenessRecord, ...]:
    records: list[LivenessRecord] = []
    for probe in manifest.liveness_probes:
        availability = probe.wave_a if phase == "wave-a" else probe.final
        if availability.status == "deferred":
            records.append(
                LivenessRecord(
                    probe_id=probe.probe_id,
                    status="deferred",
                    syncpoint=None,
                    reply_reserved=None,
                    peak_transport_bytes=None,
                    park_transitions=None,
                    reason=availability.reason,
                    provenance_commit=availability.provenance_commit,
                )
            )
            continue
        fixture = candidate_binaries.get(probe.fixture)
        if fixture is None:
            raise GateFailure(
                f"liveness probe {probe.probe_id} has no candidate fixture binary"
            )
        event: dict[str, object] = {
            "probe": probe.probe_id,
            "phase": "liveness",
            "side": "candidate",
            "status": "started",
        }
        events.append(event)
        try:
            record = _run_liveness_probe(
                manifest, probe, fixture, protocol_sha256
            )
        except GateFailure as err:
            event["status"] = "failed"
            event["failure"] = str(err)
            raise
        event["status"] = "passed"
        records.append(record)
    return tuple(records)


def _run_liveness_probe(
    manifest: Manifest,
    probe: LivenessProbe,
    fixture: BuiltFixture,
    protocol_sha256: str,
) -> LivenessRecord:
    nonce = secrets.token_hex(16)
    result = run_checked(
        [
            "taskset",
            "-c",
            manifest.reference.cpuset,
            str(fixture.binary),
            probe.probe,
        ],
        cwd=fixture.binary.parent,
        timeout_seconds=probe.timeout_seconds,
        environment={
            "SURGE_CARRIER_LIVENESS": "1",
            "SURGE_CARRIER_LIVENESS_PROBE": probe.probe,
            "SURGE_CARRIER_BENCH_NONCE": nonce,
            "SURGE_CARRIER_BENCH_PROTOCOL_SHA256": protocol_sha256,
            # No SURGE_SYNC_POINT. It used to hold a thread at
            # SP_CARRIER_JUMBO_ADMITTED, a point of the withdrawn byte-credit
            # model that nothing ever reached; the point is gone with the two
            # probes that waited on it (owner ruling 2026-09-04). A probe that
            # needs a thread held at a window names the window itself.
            "SURGE_SHARDS": str(manifest.shards),
            "SURGE_THREADS": str(manifest.threads),
            "SURGE_BLOCKING_THREADS": str(manifest.blocking_threads),
        },
    )
    return _parse_liveness_record(
        result.stdout,
        result.stderr,
        probe,
        expected_nonce=nonce,
        expected_protocol_sha256=protocol_sha256,
    )


def _run_warmups(
    manifest: Manifest,
    row_index: int,
    row: Row,
    fixtures: Mapping[Side, BuiltFixture],
    placement: int,
    events: list[dict[str, object]],
    protocol_sha256: str,
    ordinal: int,
    capture_expected_endpoint_red: bool,
    allocation_mismatches: list[AllocationMismatch],
) -> int:
    for local_warmup in range(manifest.protocol.warmups):
        warmup_index = placement * manifest.protocol.warmups + local_warmup
        for side in paired_order(row_index, warmup_index):
            fixture = fixtures[side]
            for batch_index in range(row.batches):
                batch = _run_recorded_batch(
                    manifest,
                    row,
                    side,
                    fixture,
                    events,
                    phase="warmup",
                    run_index=warmup_index,
                    batch_index=batch_index,
                    protocol_sha256=protocol_sha256,
                    capture_kind="timing",
                    ordinal=ordinal,
                    placement=placement,
                )
                if side == "candidate":
                    _capture_or_validate_structural_allocation(
                        row,
                        batch,
                        phase="warmup",
                        run_index=warmup_index,
                        batch_index=batch_index,
                        capture_expected_endpoint_red=capture_expected_endpoint_red,
                        allocation_mismatches=allocation_mismatches,
                    )
                ordinal += 1
    return ordinal


def _run_measured_timing(
    manifest: Manifest,
    row: Row,
    side: Side,
    pair_index: int,
    placement: int,
    fixture: BuiltFixture,
    events: list[dict[str, object]],
    protocol_sha256: str,
    ordinal: int,
    capture_expected_endpoint_red: bool,
    allocation_mismatches: list[AllocationMismatch],
) -> RunRecord:
    batches = tuple(
        _run_recorded_batch(
            manifest,
            row,
            side,
            fixture,
            events,
            phase="measured",
            run_index=pair_index,
            batch_index=batch_index,
            protocol_sha256=protocol_sha256,
            capture_kind="timing",
            ordinal=ordinal + batch_index,
            placement=placement,
        )
        for batch_index in range(row.batches)
    )
    if side == "candidate":
        for batch_index, batch in enumerate(batches):
            _capture_or_validate_structural_allocation(
                row,
                batch,
                phase="measured",
                run_index=pair_index,
                batch_index=batch_index,
                capture_expected_endpoint_red=capture_expected_endpoint_red,
                allocation_mismatches=allocation_mismatches,
            )
    measured = MeasuredRun(
        side=side,
        pair_index=pair_index,
        timing=TimingSample(
            elapsed_ns=sum(batch.elapsed_ns for batch in batches),
            operation_latencies_ns=tuple(
                latency
                for batch in batches
                for latency in batch.operation_latencies_ns
            ),
            operations=row.operations_per_batch * row.batches,
        ),
        counters=CounterSample({}),
    )
    return RunRecord(measured=measured, batches=batches)


def place_fixture_copies(
    timing_binaries: Mapping[Side, Mapping[str, BuiltFixture]],
    placements: int,
) -> dict[Side, dict[str, tuple[BuiltFixture, ...]]]:
    """Give every timing fixture `placements` physically distinct files.

    A fixture's speed is a property of the physical pages its file landed on
    (owner ruling 2026-09-04: byte-identical copies of one binary read up to
    a third apart, each copy flat), so each side is measured on this many
    copies of its binary and scored on the fastest. Copy 0 is the built
    binary itself; the others are fresh files beside it, written through
    the page cache so they take pages of their own, and each is checked to
    be the same bytes as the original.
    """
    if placements < 1:
        raise GateFailure("protocol.placements must be at least 1")
    placed: dict[Side, dict[str, tuple[BuiltFixture, ...]]] = {}
    for side, fixtures in timing_binaries.items():
        placed[side] = {}
        for source_path, fixture in fixtures.items():
            if not fixture.binary.is_file():
                # A stand that fakes the batch runner hands in a binary that
                # was never built; there is nothing to copy, and every
                # placement is that same fixture. A real run cannot get here:
                # the build step handed the path over after linking it.
                placed[side][source_path] = (fixture,) * placements
                continue
            original = hashlib.sha256(fixture.binary.read_bytes()).hexdigest()
            copies = [fixture]
            for index in range(1, placements):
                target_dir = fixture.binary.parent.parent / f"placement-{index:02d}"
                if target_dir.exists():
                    shutil.rmtree(target_dir)
                shutil.copytree(fixture.binary.parent, target_dir)
                copy = target_dir / fixture.binary.name
                if hashlib.sha256(copy.read_bytes()).hexdigest() != original:
                    raise GateFailure(
                        f"{side} {source_path} placement {index} is not the built binary's bytes"
                    )
                copies.append(replace(fixture, binary=copy))
            placed[side][source_path] = tuple(copies)
    return placed


def _finalize_timing_record(
    manifest: Manifest, record: RunRecord, side: Side
) -> RunRecord:
    counters = aggregate_counters(
        manifest.metrics, [batch.counters for batch in record.batches], side
    )
    return replace(
        record,
        measured=replace(record.measured, counters=counters),
    )


def _merge_resource_record(
    manifest: Manifest,
    row: Row,
    timing_record: RunRecord,
    resource_batches: tuple[BatchResult, ...],
) -> RunRecord:
    if len(timing_record.batches) != row.batches or len(resource_batches) != row.batches:
        raise GateFailure(f"{row.row_id} timing/resource batch cardinality mismatch")
    merged_counters: list[Mapping[str, int | None]] = []
    for timing, resource in zip(
        timing_record.batches, resource_batches, strict=True
    ):
        counters = {
            metric.name: (
                timing.counters[metric.name]
                if metric.source == "fixture"
                else resource.counters[metric.name]
            )
            for metric in manifest.metrics
        }
        merged_counters.append(counters)
    counters = aggregate_counters(
        manifest.metrics,
        merged_counters,
        "candidate",
    )
    return RunRecord(
        measured=replace(timing_record.measured, counters=counters),
        batches=timing_record.batches,
        resource_batches=resource_batches,
    )


def _validate_structural_allocation(
    row: Row, timing: BatchResult, batch_index: int
) -> None:
    mismatches: list[AllocationMismatch] = []
    _capture_or_validate_structural_allocation(
        row,
        timing,
        phase="measured",
        run_index=0,
        batch_index=batch_index,
        capture_expected_endpoint_red=False,
        allocation_mismatches=mismatches,
    )


def _capture_or_validate_structural_allocation(
    row: Row,
    timing: BatchResult,
    *,
    phase: str,
    run_index: int,
    batch_index: int,
    capture_expected_endpoint_red: bool,
    allocation_mismatches: list[AllocationMismatch],
) -> None:
    actual = timing.counters.get("allocation_count")
    expected = row.candidate_structural_allocations_per_batch
    if type(actual) is not int or actual < 0:
        raise GateFailure(
            f"{row.row_id} candidate {phase} batch {batch_index}: required "
            "allocation_count must be a non-negative integer"
        )
    if actual == expected:
        return
    mismatch = AllocationMismatch(
        row_id=row.row_id,
        phase=phase,
        warmup_index=run_index if phase == "warmup" else None,
        pair_index=run_index if phase == "measured" else None,
        batch_index=batch_index,
        actual=actual,
        expected=expected,
    )
    if not capture_expected_endpoint_red:
        raise GateFailure(mismatch.message())
    allocation_mismatches.append(mismatch)


def _run_recorded_batch(
    manifest: Manifest,
    row: Row,
    side: Side,
    fixture: BuiltFixture,
    events: list[dict[str, object]],
    *,
    phase: str,
    run_index: int,
    batch_index: int,
    protocol_sha256: str,
    capture_kind: str = "timing",
    ordinal: int = 0,
    placement: int = 0,
) -> BatchResult:
    attempt = {
        "capture_kind": capture_kind,
        "row": row.row_id,
        "side": side,
        "phase": phase,
        "warmup_index": run_index if phase == "warmup" else None,
        "pair_index": run_index if phase == "measured" else None,
        "batch_index": batch_index,
        "ordinal": ordinal,
        "placement": placement,
    }
    event: dict[str, object] = {
        "row": row.row_id,
        "phase": phase,
        "side": side,
        "run_index": run_index,
        "batch_index": batch_index,
        "capture_kind": capture_kind,
        "ordinal": ordinal,
        "attempt": attempt,
        "status": "started",
    }
    events.append(event)
    context = (
        f"capture={capture_kind} row={row.row_id} phase={phase} side={side} "
        f"run={run_index} batch={batch_index}"
    )
    try:
        result = _run_batch(
            manifest, row, side, fixture, protocol_sha256, capture_kind
        )
    except GateFailure as err:
        event["status"] = "failed"
        event["failure"] = str(err)
        raise GateFailure(f"{context}: {err}") from err
    event["status"] = "passed"
    event["result"] = {
        "elapsed_ns": result.elapsed_ns,
        "operation_latencies_ns": list(result.operation_latencies_ns),
        "checksum": result.checksum,
        "nonce": result.nonce,
        "counters": dict(sorted(result.counters.items())),
    }
    return result


def _run_batch(
    manifest: Manifest,
    row: Row,
    side: Side,
    fixture: BuiltFixture,
    protocol_sha256: str,
    capture_kind: str = "timing",
) -> BatchResult:
    nonce = secrets.token_hex(16) if capture_kind == "resource" else ""
    command = [
        "taskset",
        "-c",
        manifest.reference.cpuset,
        str(fixture.binary),
        row.probe,
    ]
    environment = {
        "SURGE_SHARDS": str(manifest.shards),
        "SURGE_THREADS": str(manifest.threads),
        "SURGE_BLOCKING_THREADS": str(manifest.blocking_threads),
    }
    if capture_kind == "resource":
        if side != "candidate":
            raise GateFailure("resource capture is candidate-only")
        environment.update(
            {
                "SURGE_CARRIER_BENCH_COUNTERS": "1",
                "SURGE_CARRIER_BENCH_PROBE": row.probe,
                "SURGE_CARRIER_BENCH_NONCE": nonce,
                "SURGE_CARRIER_BENCH_PROTOCOL_SHA256": protocol_sha256,
            }
        )
    elif capture_kind != "timing":
        raise GateFailure(f"unknown capture kind {capture_kind!r}")
    result = run_checked(
        command,
        cwd=fixture.binary.parent,
        timeout_seconds=row.timeout_seconds,
        environment=environment,
    )
    fixture_metrics = {
        metric.name for metric in manifest.metrics if metric.source == "fixture"
    }
    parsed = _parse_result(result.stdout, row, fixture_metrics)
    if capture_kind == "timing":
        if result.stderr:
            raise GateFailure(
                f"{row.row_id} timing binary emitted unexpected stderr:\n{result.stderr}"
            )
        runtime_metrics = {
            metric.name: None
            for metric in manifest.metrics
            if metric.source == "runtime_exit"
        }
    else:
        runtime_metrics = _parse_runtime_counters(
            result.stderr,
            row,
            manifest,
            side,
            expected_nonce=nonce,
            expected_protocol_sha256=protocol_sha256,
        )
    counters = {**parsed.counters, **runtime_metrics}
    if set(counters) != set(row.required_metrics):
        raise GateFailure(f"{row.row_id} combined metric schema drifted")
    if parsed.checksum != row.expected_checksum:
        raise GateFailure(
            f"{row.row_id} checksum {parsed.checksum!r}, want {row.expected_checksum!r}"
        )
    return BatchResult(
        nonce=nonce,
        elapsed_ns=parsed.elapsed_ns,
        operation_latencies_ns=parsed.operation_latencies_ns,
        checksum=parsed.checksum,
        counters=counters,
    )


def _validate_attempt_sequence(
    manifest: Manifest, events: Sequence[Mapping[str, object]]
) -> None:
    _validate_attempts(events, _expected_attempt_sequence(manifest))


def _validate_timing_attempt_sequence(
    manifest: Manifest, events: Sequence[Mapping[str, object]]
) -> None:
    _validate_attempts(events, _expected_timing_attempt_sequence(manifest))


def _validate_attempts(
    events: Sequence[Mapping[str, object]],
    expected: Sequence[Mapping[str, object]],
) -> None:
    actual = [event.get("attempt") for event in events]
    expected_list = list(expected)
    if actual != expected_list:
        mismatch = next(
            (
                index
                for index, (left, right) in enumerate(zip(actual, expected_list))
                if left != right
            ),
            min(len(actual), len(expected_list)),
        )
        raise GateFailure(
            f"attempt sequence mismatch at ordinal {mismatch}: "
            f"actual={actual[mismatch:mismatch + 1]} "
            f"expected={expected_list[mismatch:mismatch + 1]}"
        )


def _expected_attempt_sequence(manifest: Manifest) -> list[dict[str, object]]:
    expected: list[dict[str, object]] = []
    ordinal = 0

    def add(
        capture_kind: str,
        row: str,
        side: Side,
        phase: str,
        run_index: int,
        batch_index: int,
        placement: int = 0,
    ) -> None:
        nonlocal ordinal
        expected.append(
            {
                "capture_kind": capture_kind,
                "row": row,
                "side": side,
                "phase": phase,
                "warmup_index": run_index if phase == "warmup" else None,
                "pair_index": run_index if phase == "measured" else None,
                "batch_index": batch_index,
                "ordinal": ordinal,
                "placement": placement,
            }
        )
        ordinal += 1

    protocol = manifest.protocol
    add("timing", "allocation-control", "base", "control", 0, 0)
    add("timing", "allocation-control", "candidate", "control", 0, 0)
    for row_index, row in enumerate(manifest.rows):
        for placement in range(protocol.placements):
            for local_warmup in range(protocol.warmups):
                warmup_index = placement * protocol.warmups + local_warmup
                for side in paired_order(row_index, warmup_index):
                    for batch_index in range(row.batches):
                        add("timing", row.row_id, side, "warmup", warmup_index, batch_index, placement)
            for local_pair in range(protocol.measured_pairs):
                pair_index = placement * protocol.measured_pairs + local_pair
                for side in paired_order(row_index, pair_index):
                    for batch_index in range(row.batches):
                        add("timing", row.row_id, side, "measured", pair_index, batch_index, placement)
    for row in manifest.rows:
        for pair_index in range(protocol.measured_runs):
            placement = pair_index // protocol.measured_pairs
            for batch_index in range(row.batches):
                add("resource", row.row_id, "candidate", "measured", pair_index, batch_index, placement)
    return expected


def _expected_timing_attempt_sequence(
    manifest: Manifest,
) -> list[dict[str, object]]:
    return [
        attempt
        for attempt in _expected_attempt_sequence(manifest)
        if attempt["capture_kind"] == "timing"
    ]

