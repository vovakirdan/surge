"""Build and execute frozen Runtime V2 carrier benchmark fixtures."""

from __future__ import annotations

import secrets
import shutil
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping, Sequence

from runtime_v2_carrier_bench_host import run_checked
from runtime_v2_carrier_bench_ir import (
    verify_emitted_ir as _verify_emitted_ir,
    verify_fixture_ir as _verify_fixture_ir,
    verify_fixture_source as _verify_fixture_source,
)
from runtime_v2_carrier_bench_model import (
    GateFailure,
    Manifest,
    LivenessProbe,
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


@dataclass(frozen=True, slots=True)
class BuiltFixture:
    binary: Path
    source_path: str


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
) -> dict[str, BuiltFixture]:
    fixtures: dict[str, BuiltFixture] = {}
    source_paths = sorted({row.fixture for row in manifest.rows})
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
            environment={"SURGE_STDLIB": str(side_root)},
        )
        binary = _built_binary(result.stdout, package_copy)
        _verify_fixture_ir(binary, source_path)
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
    binaries: Mapping[Side, Mapping[str, BuiltFixture]],
    events: list[dict[str, object]],
    protocol_sha256: str,
) -> dict[str, dict[Side, tuple[RunRecord, ...]]]:
    records: dict[str, dict[Side, tuple[RunRecord, ...]]] = {}
    for row_index, row in enumerate(manifest.rows):
        _run_warmups(
            manifest, row_index, row, binaries, events, protocol_sha256
        )
        per_side: dict[Side, list[RunRecord]] = {"base": [], "candidate": []}
        for pair_index in range(manifest.protocol.measured_pairs):
            for side in paired_order(row_index, pair_index):
                fixture = binaries[side][row.fixture]
                per_side[side].append(
                    _run_measured(
                        manifest,
                        row,
                        side,
                        pair_index,
                        fixture,
                        events,
                        protocol_sha256,
                    )
                )
        records[row.row_id] = {
            "base": tuple(per_side["base"]),
            "candidate": tuple(per_side["candidate"]),
        }
    return records


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
                    credit_balance=None,
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
            **_transport_environment(manifest),
            "SURGE_CARRIER_LIVENESS": "1",
            "SURGE_CARRIER_LIVENESS_PROBE": probe.probe,
            "SURGE_CARRIER_BENCH_NONCE": nonce,
            "SURGE_CARRIER_BENCH_PROTOCOL_SHA256": protocol_sha256,
            "SURGE_SYNC_POINT": "SP_CARRIER_JUMBO_ADMITTED:block",
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
    binaries: Mapping[Side, Mapping[str, BuiltFixture]],
    events: list[dict[str, object]],
    protocol_sha256: str,
) -> None:
    for warmup_index in range(manifest.protocol.warmups):
        for side in paired_order(row_index, warmup_index):
            fixture = binaries[side][row.fixture]
            for batch_index in range(row.batches):
                _run_recorded_batch(
                    manifest,
                    row,
                    side,
                    fixture,
                    events,
                    phase="warmup",
                    run_index=warmup_index,
                    batch_index=batch_index,
                    protocol_sha256=protocol_sha256,
                )


def _run_measured(
    manifest: Manifest,
    row: Row,
    side: Side,
    pair_index: int,
    fixture: BuiltFixture,
    events: list[dict[str, object]],
    protocol_sha256: str,
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
        )
        for batch_index in range(row.batches)
    )
    counters = aggregate_counters(
        manifest.metrics, [batch.counters for batch in batches], side
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
        counters=counters,
    )
    return RunRecord(measured=measured, batches=batches)


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
) -> BatchResult:
    event: dict[str, object] = {
        "row": row.row_id,
        "phase": phase,
        "side": side,
        "run_index": run_index,
        "batch_index": batch_index,
        "status": "started",
    }
    events.append(event)
    context = (
        f"row={row.row_id} phase={phase} side={side} "
        f"run={run_index} batch={batch_index}"
    )
    try:
        result = _run_batch(
            manifest, row, side, fixture, protocol_sha256
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
) -> BatchResult:
    nonce = secrets.token_hex(16)
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
    if side == "candidate":
        environment.update(
            {
                **_transport_environment(manifest),
                "SURGE_CARRIER_BENCH_COUNTERS": "1",
                "SURGE_CARRIER_BENCH_PROBE": row.probe,
                "SURGE_CARRIER_BENCH_NONCE": nonce,
                "SURGE_CARRIER_BENCH_PROTOCOL_SHA256": protocol_sha256,
            }
        )
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


def _transport_environment(manifest: Manifest) -> dict[str, str]:
    return {
        "SURGE_CARRIER_BENCH_DATA_BUDGET_BYTES": str(
            manifest.transport.data_bytes
        ),
        "SURGE_CARRIER_BENCH_CONTROL_BUDGET_BYTES": str(
            manifest.transport.control_bytes
        ),
        "SURGE_CARRIER_BENCH_JUMBO_THRESHOLD_BYTES": str(
            manifest.transport.jumbo_threshold_bytes
        ),
        "SURGE_CARRIER_BENCH_MAX_INLINE_OVERHEAD_BYTES": str(
            manifest.transport.max_inline_overhead_bytes
        ),
    }
