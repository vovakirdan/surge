"""Shared imports and factories for carrier benchmark tests."""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import tempfile
import time
import unittest
from argparse import Namespace
from contextlib import contextmanager
from dataclasses import replace
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

from unittest import mock

from runtime_v2_carrier_bench import (
    _baseline_capture_accepts,
    _build_and_run,
    _commit_root,
    _require_tracked_entries,
    _verify_harness_inventory,
    main as bench_main,
)
from runtime_v2_carrier_bench_host import (
    CommandResult,
    _format_cpuset,
    git_commit,
    run_checked,
)
from runtime_v2_carrier_bench_manifest import load_manifest, verify_file_digests
from runtime_v2_carrier_bench_model import (
    Aggregation,
    AllocationControl,
    CounterSample,
    CrossRowInvariant,
    FileDigest,
    GateFailure,
    Invariant,
    LivenessAvailability,
    LivenessProbe,
    Manifest,
    ManifestError,
    MeasuredRun,
    Metric,
    MetricAvailability,
    MetricSource,
    Protocol,
    ReferenceHost,
    Row,
    Side,
    TimingSample,
    aggregate_counters,
    nearest_rank,
    paired_order,
    validate_row_results,
)
from runtime_v2_carrier_bench_report import (
    _batch_metric_values,
    render_report,
    write_report,
)
from runtime_v2_carrier_bench_runner import (
    AllocationMismatch,
    BatchResult,
    BuiltFixture,
    LIVENESS_PREFIX,
    LivenessRecord,
    RESULT_PREFIX,
    RUNTIME_COUNTER_PREFIX,
    RunRecord,
    _allocation_control_row,
    _built_binary,
    _capture_or_validate_structural_allocation,
    _verify_emitted_ir,
    _verify_carrier_binary,
    _verify_fixture_source,
    _parse_result,
    _parse_liveness_record,
    _parse_runtime_counters,
    _run_recorded_batch,
    _run_batch,
    _validate_allocation_control,
    _validate_attempt_sequence,
    _validate_timing_attempt_sequence,
    _expected_attempt_sequence,
    _expected_timing_attempt_sequence,
    _validate_structural_allocation,
    build_fixtures,
    build_liveness_fixtures,
    build_surge,
    execute_manifest,
    execute_resource_manifest,
    execute_timing_manifest,
    _run_liveness_probe,
)
from runtime_v2_carrier_bench_ir import verify_carrier_symbols as _verify_carrier_symbols


def metric(
    name: str,
    aggregation: Aggregation,
    source: MetricSource,
    *,
    base: MetricAvailability | None = None,
) -> Metric:
    required = MetricAvailability("required")
    return Metric(
        name=name,
        aggregation=aggregation,
        source=source,
        base=required if base is None else base,
        candidate=required,
    )


def make_manifest() -> Manifest:
    row = Row(
        row_id="scalar.channel",
        workload_family="channel-buffered",
        payload_role="scalar",
        fixture="fixture.sg",
        probe="ping",
        operations_per_batch=10,
        batches=2,
        payload_bytes=8,
        timeout_seconds=5,
        relative_performance=True,
        expected_checksum="42",
        candidate_structural_allocations_per_batch=0,
        required_metrics=(
            "allocation_count",
            "bytes_copied",
            "bytes_moved",
            "callback_count",
            "data_slot_stalls",
            "peak_transport_bytes",
        ),
        # No invariant on a runtime-exit metric: those are telemetry (owner
        # ruling 2026-09-03) and the loader refuses one. A test that wants to
        # exercise the invariant machinery attaches its own.
        invariants=(),
    )
    return Manifest(
        schema_version=2,
        epic_base="0" * 40,
        reference=ReferenceHost("Linux", "x86_64", "kernel", "cpu", 4, "0,2", "go", "clang"),
        protocol=Protocol(2, 7, 0.05, 0.95, 1.10, "nearest-rank", "sample-n-minus-1"),
        backend="llvm",
        profile="release",
        shards=2,
        threads=2,
        blocking_threads=1,
        metrics=(
            metric("allocation_count", "sum", "fixture"),
            metric(
                "bytes_copied",
                "sum",
                "runtime_exit",
                base=MetricAvailability(
                    "unsupported",
                    "EPIC_BASE has no typed-carrier byte counter",
                    "0" * 40,
                ),
            ),
            metric(
                "bytes_moved",
                "sum",
                "runtime_exit",
                base=MetricAvailability(
                    "unsupported", "EPIC_BASE has no typed move counter", "0" * 40
                ),
            ),
            metric(
                "callback_count",
                "sum",
                "runtime_exit",
                base=MetricAvailability(
                    "unsupported", "EPIC_BASE has no ValueOps counter", "0" * 40
                ),
            ),
            metric(
                "data_slot_stalls",
                "sum",
                "runtime_exit",
                base=MetricAvailability(
                    "unsupported", "EPIC_BASE credit counter is inert", "0" * 40
                ),
            ),
            metric(
                "peak_transport_bytes",
                "max",
                "runtime_exit",
                base=MetricAvailability(
                    "unsupported",
                    "EPIC_BASE has no physical transport byte counter",
                    "0" * 40,
                ),
            ),
        ),
        allocation_control=AllocationControl(
            "fixture.sg", "allocation-control", "1", 1
        ),
        harness_files=(),
        fixtures=(),
        rows=(row,),
        cross_row_invariants=(),
        liveness_probes=(
            LivenessProbe(
                probe_id="large-payload-park-cancel",
                fixture="fixture.sg",
                probe="large-payload-park-cancel",
                syncpoint="SP_TRANSPORT_DATA_SLOT_TASK_PARKED",
                timeout_seconds=5,
                expected_reply_reserved=0,
                expected_park_transitions=1,
                wave_a=LivenessAvailability(
                    "deferred", "requires Wave E shutdown", "0" * 40
                ),
                final=LivenessAvailability("required"),
            ),
            LivenessProbe(
                probe_id="large-payload-park-shutdown",
                fixture="fixture.sg",
                probe="large-payload-park-shutdown",
                syncpoint="SP_TRANSPORT_DATA_SLOT_TASK_PARKED",
                timeout_seconds=5,
                expected_reply_reserved=0,
                expected_park_transitions=1,
                wave_a=LivenessAvailability(
                    "deferred", "requires Wave E shutdown", "0" * 40
                ),
                final=LivenessAvailability("required"),
            ),
        ),
    )


def counter_values(
    side: str, *, allocation_count: int = 0
) -> dict[str, int | None]:
    physical = 0 if side == "candidate" else None
    return {
        "allocation_count": allocation_count,
        "bytes_copied": physical,
        "bytes_moved": physical,
        "callback_count": physical,
        "data_slot_stalls": physical,
        "peak_transport_bytes": physical,
    }


def make_runs(side: str, latency: int) -> tuple[MeasuredRun, ...]:
    return tuple(
        MeasuredRun(
            side=side,
            pair_index=index,
            timing=TimingSample(latency * 20, (latency,) * 20, 20),
            counters=CounterSample(counter_values(side)),
        )
        for index in range(7)
    )


def make_records(
    side: str, latencies: list[int], *, allocation_count: int = 0
) -> tuple[RunRecord, ...]:
    records: list[RunRecord] = []
    for index, latency in enumerate(latencies):
        measured = MeasuredRun(
            side=side,
            pair_index=index,
            timing=TimingSample(latency * 20, (latency,) * 20, 20),
            counters=CounterSample(
                counter_values(side, allocation_count=allocation_count * 2)
            ),
        )
        batches = (
            BatchResult(
                latency * 10,
                (latency,) * 10,
                "42",
                counter_values(side, allocation_count=allocation_count),
            ),
            BatchResult(
                latency * 10,
                (latency,) * 10,
                "42",
                counter_values(side, allocation_count=allocation_count),
            ),
        )
        records.append(RunRecord(measured, batches))
    return tuple(records)


def records_with_batch_metric(
    records: tuple[RunRecord, ...], metric_name: str, values: tuple[int, ...]
) -> tuple[RunRecord, ...]:
    updated: list[RunRecord] = []
    for record in records:
        if len(record.batches) != len(values):
            raise ValueError("batch value count mismatch")
        batches = tuple(
            replace(
                batch,
                counters={**batch.counters, metric_name: value},
            )
            for batch, value in zip(record.batches, values, strict=True)
        )
        counters = dict(record.measured.counters.values)
        counters[metric_name] = sum(values)
        updated.append(
            replace(
                record,
                measured=replace(
                    record.measured, counters=CounterSample(counters)
                ),
                batches=batches,
            )
        )
    return tuple(updated)


def deferred_liveness() -> LivenessRecord:
    return LivenessRecord(
        probe_id="large-payload-park-cancel",
        status="deferred",
        syncpoint=None,
        reply_reserved=None,
        peak_transport_bytes=None,
        park_transitions=None,
        reason="requires Wave E shutdown",
        provenance_commit="0" * 40,
    )


def manifest_json() -> dict[str, object]:
    return {
        "schema_version": 2,
        "epic_base": "0" * 40,
        "reference_host": {
            "system": "Linux",
            "machine": "x86_64",
            "kernel_contains": "kernel",
            "cpu_model": "cpu",
            "logical_cpus": 4,
            "cpuset": "0,2",
            "go_version": "go",
            "clang_version": "clang",
        },
        "protocol": {
            "warmups": 2,
            "measured_pairs": 7,
            "max_cv": 0.05,
            "throughput_min_ratio": 0.95,
            "p95_max_ratio": 1.10,
            "percentile_method": "nearest-rank",
            "cv_method": "sample-n-minus-1",
        },
        "backend": "llvm",
        "profile": "release",
        "shards": 2,
        "threads": 2,
        "blocking_threads": 1,
        "allocation_control": {
            "fixture": "fixture.sg",
            "probe": "allocation-control",
            "expected_checksum": "1",
            "expected_allocation_count": 1,
        },
        "metrics": [
            {
                "name": "allocation_count",
                "aggregation": "sum",
                "source": "fixture",
                "base": {"status": "required"},
                "candidate": {"status": "required"},
            },
            {
                "name": "bytes_copied",
                "aggregation": "sum",
                "source": "runtime_exit",
                "base": {
                    "status": "unsupported",
                    "reason": "EPIC_BASE has no typed-carrier byte counter",
                    "provenance_commit": "0" * 40,
                },
                "candidate": {"status": "required"},
            },
            {
                "name": "bytes_moved",
                "aggregation": "sum",
                "source": "runtime_exit",
                "base": {
                    "status": "unsupported",
                    "reason": "EPIC_BASE has no typed move counter",
                    "provenance_commit": "0" * 40,
                },
                "candidate": {"status": "required"},
            },
            {
                "name": "callback_count",
                "aggregation": "sum",
                "source": "runtime_exit",
                "base": {
                    "status": "unsupported",
                    "reason": "EPIC_BASE has no ValueOps counter",
                    "provenance_commit": "0" * 40,
                },
                "candidate": {"status": "required"},
            },
            {
                "name": "data_slot_stalls",
                "aggregation": "sum",
                "source": "runtime_exit",
                "base": {
                    "status": "unsupported",
                    "reason": "EPIC_BASE credit counter is inert",
                    "provenance_commit": "0" * 40,
                },
                "candidate": {"status": "required"},
            },
            {
                "name": "peak_transport_bytes",
                "aggregation": "max",
                "source": "runtime_exit",
                "base": {
                    "status": "unsupported",
                    "reason": "EPIC_BASE has no physical transport byte counter",
                    "provenance_commit": "0" * 40,
                },
                "candidate": {"status": "required"},
            },
        ],
        "harness_files": [{"path": "harness.py", "sha256": "0" * 64}],
        "fixtures": [{"path": "fixture.sg", "sha256": "0" * 64}],
        "rows": [
            {
                "id": "scalar.channel",
                "workload_family": "channel-buffered",
                "payload_role": "scalar",
                "fixture": "fixture.sg",
                "probe": "ping",
                "operations_per_batch": 10,
                "batches": 2,
                "payload_bytes": 8,
                "timeout_seconds": 5,
                "relative_performance": True,
                "expected_checksum": "42",
                "candidate_structural_allocations_per_batch": 0,
                "required_metrics": [
                    "allocation_count",
                    "bytes_copied",
                    "bytes_moved",
                    "callback_count",
                    "data_slot_stalls",
                    "peak_transport_bytes",
                ],
                "invariants": [],
            }
        ],
        "cross_row_invariants": [],
        "liveness_probes": [
            {
                "id": "large-payload-park-cancel",
                "fixture": "fixture.sg",
                "probe": "large-payload-park-cancel",
                "syncpoint": "SP_TRANSPORT_DATA_SLOT_TASK_PARKED",
                "timeout_seconds": 5,
                "expected_reply_reserved": 0,
                "expected_park_transitions": 1,
                "wave_a": {
                    "status": "deferred",
                    "reason": "requires Wave E shutdown",
                    "provenance_commit": "0" * 40,
                },
                "final": {"status": "required"},
            },
            {
                "id": "large-payload-park-shutdown",
                "fixture": "fixture.sg",
                "probe": "large-payload-park-shutdown",
                "syncpoint": "SP_TRANSPORT_DATA_SLOT_TASK_PARKED",
                "timeout_seconds": 5,
                "expected_reply_reserved": 0,
                "expected_park_transitions": 1,
                "wave_a": {
                    "status": "deferred",
                    "reason": "requires Wave E shutdown",
                    "provenance_commit": "0" * 40,
                },
                "final": {"status": "required"},
            },
        ],
    }

__all__ = [name for name in globals() if not name.startswith("__")]
