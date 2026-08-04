"""Unit tests for the fail-closed Runtime V2 carrier benchmark harness."""

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
from dataclasses import replace
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

from unittest import mock

from runtime_v2_carrier_bench import (
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
    CounterSample,
    FileDigest,
    GateFailure,
    Invariant,
    Manifest,
    ManifestError,
    MeasuredRun,
    Metric,
    MetricAvailability,
    MetricSource,
    Protocol,
    ReferenceHost,
    Row,
    TimingSample,
    aggregate_counters,
    nearest_rank,
    paired_order,
    validate_row_results,
)
from runtime_v2_carrier_bench_report import render_report, write_report
from runtime_v2_carrier_bench_runner import (
    BatchResult,
    BuiltFixture,
    RUNTIME_COUNTER_PREFIX,
    RunRecord,
    _built_binary,
    _parse_result,
    _parse_runtime_counters,
    _run_recorded_batch,
    build_fixtures,
)


class ModelTests(unittest.TestCase):
    def test_pair_order_alternates_across_rows_and_pairs(self) -> None:
        self.assertEqual(paired_order(0, 0), ("base", "candidate"))
        self.assertEqual(paired_order(0, 1), ("candidate", "base"))
        self.assertEqual(paired_order(1, 0), ("candidate", "base"))

    def test_nearest_rank_is_not_interpolated(self) -> None:
        self.assertEqual(nearest_rank([10.0, 20.0, 30.0, 40.0], 0.50), 20.0)
        self.assertEqual(nearest_rank([10.0, 20.0, 30.0, 40.0], 0.95), 40.0)

    def test_counter_aggregation_is_explicit_and_exact(self) -> None:
        metrics = (
            metric("allocation_count", "sum", "fixture"),
            metric("bytes_copied", "sum", "runtime_exit"),
        )
        sample = aggregate_counters(
            metrics,
            [
                {"allocation_count": 2, "bytes_copied": 8},
                {"allocation_count": 3, "bytes_copied": 5},
            ],
            "base",
        )
        self.assertEqual(sample.values, {"allocation_count": 5, "bytes_copied": 13})
        with self.assertRaisesRegex(GateFailure, "metrics mismatch"):
            aggregate_counters(metrics, [{"allocation_count": 2}], "base")
        with self.assertRaisesRegex(GateFailure, "non-negative integer"):
            aggregate_counters(
                metrics, [{"allocation_count": True, "bytes_copied": 1}], "base"
            )
        unsupported = (
            metric(
                "bytes_copied",
                "sum",
                "runtime_exit",
                base=MetricAvailability("unsupported", "legacy", "EPIC_BASE"),
            ),
        )
        self.assertEqual(
            aggregate_counters(unsupported, [{"bytes_copied": None}], "base").values,
            {"bytes_copied": None},
        )
        with self.assertRaisesRegex(GateFailure, "must be reported as null"):
            aggregate_counters(unsupported, [{"bytes_copied": 0}], "base")

    def test_row_gate_checks_every_run_and_relative_budget(self) -> None:
        manifest = make_manifest()
        base = make_runs("base", latency=1_000)
        candidate = make_runs("candidate", latency=1_040)
        base_score, candidate_score = validate_row_results(
            manifest, manifest.rows[0], base, candidate
        )
        self.assertGreater(candidate_score.p95_ns, base_score.p95_ns)
        slow = make_runs("candidate", latency=1_200)
        with self.assertRaisesRegex(GateFailure, "throughput ratio"):
            validate_row_results(manifest, manifest.rows[0], base, slow)
        bad_counter = list(candidate)
        bad_counter[3] = replace(
            bad_counter[3],
            counters=CounterSample(counter_values("candidate", allocation_count=1)),
        )
        with self.assertRaisesRegex(GateFailure, "violates le 0"):
            validate_row_results(manifest, manifest.rows[0], base, bad_counter)


class ManifestTests(unittest.TestCase):
    def test_loader_rejects_unknown_fields_and_stale_digests(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "fixture.sg").write_text("fixture\n", encoding="utf-8")
            (root / "harness.py").write_text("harness\n", encoding="utf-8")
            raw = manifest_json()
            path = root / "manifest.json"
            path.write_text(json.dumps(raw), encoding="utf-8")
            manifest = load_manifest(path)
            with self.assertRaisesRegex(ManifestError, "stale fixture"):
                verify_file_digests(root, manifest.fixtures, "fixture")
            raw["unknown"] = True
            path.write_text(json.dumps(raw), encoding="utf-8")
            with self.assertRaisesRegex(ManifestError, "fields mismatch"):
                load_manifest(path)

    def test_loader_requires_complete_metric_schema(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "manifest.json"
            raw = manifest_json()
            raw["rows"][0]["required_metrics"] = ["allocations"]
            path.write_text(json.dumps(raw), encoding="utf-8")
            with self.assertRaisesRegex(ManifestError, "complete metric schema"):
                load_manifest(path)

    def test_loader_rejects_duplicate_json_keys(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "manifest.json"
            encoded = json.dumps(manifest_json())
            encoded = encoded.replace(
                '"schema_version": 1',
                '"schema_version": 1, "schema_version": 1',
                1,
            )
            path.write_text(encoded, encoding="utf-8")
            with self.assertRaisesRegex(ManifestError, "duplicate JSON key"):
                load_manifest(path)
            raw = manifest_json()
            raw["protocol"]["max_cv"] = float("nan")
            path.write_text(json.dumps(raw), encoding="utf-8")
            with self.assertRaisesRegex(ManifestError, "non-finite JSON number"):
                load_manifest(path)

    def test_loader_freezes_every_metric_source_aggregation_and_availability(self) -> None:
        def missing_metric(raw: dict[str, object]) -> None:
            raw["metrics"].pop()

        def wrong_source(raw: dict[str, object]) -> None:
            raw["metrics"][1]["source"] = "fixture"

        def wrong_aggregation(raw: dict[str, object]) -> None:
            raw["metrics"][-1]["aggregation"] = "sum"

        def fake_base_value(raw: dict[str, object]) -> None:
            raw["metrics"][1]["base"] = {"status": "required"}

        def wrong_provenance(raw: dict[str, object]) -> None:
            raw["metrics"][1]["base"]["provenance_commit"] = "1" * 40

        cases = (
            ("missing", missing_metric, "frozen six-metric contract"),
            ("source", wrong_source, "source=runtime_exit"),
            ("aggregation", wrong_aggregation, "aggregation=max"),
            ("base", fake_base_value, "base=unsupported"),
            ("provenance", wrong_provenance, "provenance must equal epic_base"),
        )
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "manifest.json"
            for label, mutate, expected in cases:
                with self.subTest(label=label):
                    raw = manifest_json()
                    mutate(raw)
                    path.write_text(json.dumps(raw), encoding="utf-8")
                    with self.assertRaisesRegex(ManifestError, expected):
                        load_manifest(path)

    def test_harness_inventory_is_exact_and_entries_must_be_tracked(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            scripts = root / "scripts"
            scripts.mkdir()
            main = scripts / "runtime_v2_carrier_bench.py"
            helper = scripts / "runtime_v2_carrier_bench_host.py"
            main.write_text("# main\n", encoding="utf-8")
            helper.write_text("# helper\n", encoding="utf-8")
            entries = (
                FileDigest("scripts/runtime_v2_carrier_bench.py", "0" * 64),
                FileDigest("scripts/runtime_v2_carrier_bench_host.py", "0" * 64),
            )
            manifest = replace(make_manifest(), harness_files=entries)
            _verify_harness_inventory(root, manifest)
            run_checked(["git", "init", "-q"], cwd=root, timeout_seconds=5)
            run_checked(
                ["git", "add", "--", entries[0].path],
                cwd=root,
                timeout_seconds=5,
            )
            with self.assertRaisesRegex(ManifestError, "is not tracked"):
                _require_tracked_entries(root, entries, "harness file")
            run_checked(
                ["git", "add", "--", entries[1].path],
                cwd=root,
                timeout_seconds=5,
            )
            _require_tracked_entries(root, entries, "harness file")
            (scripts / "runtime_v2_carrier_bench_hidden.py").write_text(
                "# hidden\n", encoding="utf-8"
            )
            with self.assertRaisesRegex(ManifestError, "harness inventory mismatch"):
                _verify_harness_inventory(root, manifest)
            (scripts / "runtime_v2_carrier_bench_hidden.py").unlink()
            (scripts / "carrier_shared.py").write_text("# shared\n", encoding="utf-8")
            main.write_text("import carrier_shared\n", encoding="utf-8")
            with self.assertRaisesRegex(ManifestError, "carrier_shared.py"):
                _verify_harness_inventory(root, manifest)


class ResultAndReportTests(unittest.TestCase):
    def test_detached_commit_root_excludes_ignored_live_inputs(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            run_checked(["git", "init", "-q"], cwd=root, timeout_seconds=5)
            run_checked(
                ["git", "config", "user.name", "Carrier Test"],
                cwd=root,
                timeout_seconds=5,
            )
            run_checked(
                ["git", "config", "user.email", "carrier@example.invalid"],
                cwd=root,
                timeout_seconds=5,
            )
            (root / ".gitignore").write_text("test*.sg\n", encoding="utf-8")
            stdlib = root / "stdlib"
            stdlib.mkdir()
            (stdlib / "module.sg").write_text("tracked\n", encoding="utf-8")
            (stdlib / "test_poison.sg").write_text("ignored\n", encoding="utf-8")
            run_checked(["git", "add", "."], cwd=root, timeout_seconds=5)
            run_checked(
                ["git", "commit", "-q", "-m", "fixture"],
                cwd=root,
                timeout_seconds=5,
            )
            commit = git_commit(root)
            detached_path: Path | None = None
            with _commit_root(root, commit, "candidate-test") as detached:
                detached_path = detached
                self.assertEqual(git_commit(detached), commit)
                self.assertTrue((detached / "stdlib" / "module.sg").is_file())
                self.assertFalse((detached / "stdlib" / "test_poison.sg").exists())
            self.assertIsNotNone(detached_path)
            self.assertFalse(detached_path.exists())

    def test_subprocess_supervision_rejects_preexisting_children(self) -> None:
        child = subprocess.Popen(
            [sys.executable, "-c", "import time; time.sleep(60)"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            start_new_session=True,
        )
        try:
            with tempfile.TemporaryDirectory() as temporary:
                with self.assertRaisesRegex(GateFailure, "pre-existing child"):
                    run_checked(
                        [sys.executable, "-c", "print('must not run')"],
                        cwd=Path(temporary),
                        timeout_seconds=5,
                    )
            self.assertIsNone(child.poll())
        finally:
            child.kill()
            child.wait(timeout=5)

    def test_subprocess_environment_drops_ambient_surge_settings(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            ambient = {
                "GOENV": "/tmp/poison-go-env",
                "GOWORK": "/tmp/poison-go-work",
                "SURGE_CHANNEL_WAKE_INJECT": "poison",
            }
            with mock.patch.dict(os.environ, ambient):
                result = run_checked(
                    [
                        sys.executable,
                        "-c",
                        "import os; "
                        "print(os.getenv('SURGE_CHANNEL_WAKE_INJECT', 'clean')); "
                        "print(os.environ['GOWORK']); print(os.environ['GOENV'])",
                    ],
                    cwd=Path(temporary),
                    timeout_seconds=5,
                    environment={"GOENV": "caller-poison", "GOWORK": "caller-poison"},
                )
        self.assertEqual(result.stdout, "clean\noff\noff\n")

    def test_subprocess_invalid_utf8_is_a_gate_failure_with_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaisesRegex(GateFailure, "invalid UTF-8") as caught:
                run_checked(
                    [sys.executable, "-c", "import os; os.write(1, b'\\xff\\n')"],
                    cwd=Path(temporary),
                    timeout_seconds=5,
                )
        self.assertIn("\\xff", str(caught.exception))

    def test_timeout_kills_sigterm_ignoring_grandchild(self) -> None:
        child = (
            "import signal,time; "
            "signal.signal(signal.SIGTERM, signal.SIG_IGN); time.sleep(60)"
        )
        parent = (
            "import signal,subprocess,sys,time; "
            "signal.signal(signal.SIGTERM, signal.SIG_IGN); "
            f"p=subprocess.Popen([sys.executable,'-c',{child!r}]); "
            "print(p.pid, flush=True); time.sleep(60)"
        )
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaises(GateFailure) as caught:
                run_checked(
                    [sys.executable, "-c", parent],
                    cwd=Path(temporary),
                    timeout_seconds=1,
                )
        match = re.search(r"stdout:\n(\d+)", str(caught.exception))
        self.assertIsNotNone(match)
        child_pid = int(match.group(1))
        deadline = time.monotonic() + 2.0
        while Path(f"/proc/{child_pid}").exists() and time.monotonic() < deadline:
            time.sleep(0.02)
        self.assertFalse(Path(f"/proc/{child_pid}").exists())

    def test_timeout_kills_escaped_session_holding_capture_pipes(self) -> None:
        child = (
            "import signal,time; "
            "signal.signal(signal.SIGTERM, signal.SIG_IGN); time.sleep(60)"
        )
        parent = (
            "import subprocess,sys; "
            f"p=subprocess.Popen([sys.executable,'-c',{child!r}], "
            "start_new_session=True); print(p.pid, flush=True)"
        )
        started = time.monotonic()
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaisesRegex(GateFailure, "timed out") as caught:
                run_checked(
                    [sys.executable, "-c", parent],
                    cwd=Path(temporary),
                    timeout_seconds=1,
                )
        self.assertLess(time.monotonic() - started, 4.0)
        match = re.search(r"stdout:\n(\d+)", str(caught.exception))
        self.assertIsNotNone(match)
        child_pid = int(match.group(1))
        self.assertFalse(Path(f"/proc/{child_pid}").exists())

    def test_nonzero_parent_cannot_leave_redirected_grandchild(self) -> None:
        child = (
            "import signal,time; "
            "signal.signal(signal.SIGTERM, signal.SIG_IGN); time.sleep(60)"
        )
        parent = (
            "import subprocess,sys; "
            f"p=subprocess.Popen([sys.executable,'-c',{child!r}], "
            "stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,"
            "stderr=subprocess.DEVNULL); "
            "print(p.pid, flush=True); sys.exit(7)"
        )
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaises(GateFailure) as caught:
                run_checked(
                    [sys.executable, "-c", parent],
                    cwd=Path(temporary),
                    timeout_seconds=5,
                )
        match = re.search(r"stdout:\n(\d+)", str(caught.exception))
        self.assertIsNotNone(match)
        child_pid = int(match.group(1))
        deadline = time.monotonic() + 2.0
        while Path(f"/proc/{child_pid}").exists() and time.monotonic() < deadline:
            time.sleep(0.02)
        self.assertFalse(Path(f"/proc/{child_pid}").exists())

    def test_success_parent_with_redirected_grandchild_is_a_failure(self) -> None:
        child = (
            "import signal,time; "
            "signal.signal(signal.SIGTERM, signal.SIG_IGN); time.sleep(60)"
        )
        parent = (
            "import subprocess,sys; "
            f"p=subprocess.Popen([sys.executable,'-c',{child!r}], "
            "stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,"
            "stderr=subprocess.DEVNULL); "
            "print(p.pid, flush=True)"
        )
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaisesRegex(
                GateFailure, "left a live process group"
            ) as caught:
                run_checked(
                    [sys.executable, "-c", parent],
                    cwd=Path(temporary),
                    timeout_seconds=5,
                )
        match = re.search(r"stdout:\n(\d+)", str(caught.exception))
        self.assertIsNotNone(match)
        child_pid = int(match.group(1))
        deadline = time.monotonic() + 2.0
        while Path(f"/proc/{child_pid}").exists() and time.monotonic() < deadline:
            time.sleep(0.02)
        self.assertFalse(Path(f"/proc/{child_pid}").exists())

    def test_build_log_ignores_timing_line_named_built(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            package = Path(temporary) / "fixture"
            binary = package / "target" / "release" / "fixture"
            binary.parent.mkdir(parents=True)
            binary.write_text("binary", encoding="utf-8")
            binary.chmod(0o700)
            output = "built 4114.3 ms\nbuilt target/release/fixture\n"
            self.assertEqual(_built_binary(output, package), binary)

    def test_build_log_rejects_stale_side_root_binary(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            package = root / "copied" / "fixture"
            package.mkdir(parents=True)
            stale = root / "target" / "release" / "fixture"
            stale.parent.mkdir(parents=True)
            stale.write_text("stale", encoding="utf-8")
            stale.chmod(0o700)
            with self.assertRaisesRegex(GateFailure, "release directory is unavailable"):
                _built_binary("built target/release/fixture\n", package)

    def test_build_log_rejects_symlinked_fixture_binary(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            package = Path(temporary) / "fixture"
            release = package / "target" / "release"
            release.mkdir(parents=True)
            real = release / "real-fixture"
            real.write_text("binary", encoding="utf-8")
            real.chmod(0o700)
            (release / "fixture").symlink_to(real)
            with self.assertRaisesRegex(GateFailure, "found=\\[\\]"):
                _built_binary("built target/release/fixture\n", package)

    def test_build_log_rejects_symlinked_output_parent(self) -> None:
        for component in ("target", "release"):
            with self.subTest(component=component), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                package = root / "fixture"
                package.mkdir()
                external_target = root / "external-target"
                external_release = external_target / "release"
                external_release.mkdir(parents=True)
                binary = external_release / "fixture"
                binary.write_text("outside", encoding="utf-8")
                binary.chmod(0o700)
                if component == "target":
                    (package / "target").symlink_to(
                        external_target, target_is_directory=True
                    )
                else:
                    target = package / "target"
                    target.mkdir()
                    (target / "release").symlink_to(
                        external_release, target_is_directory=True
                    )
                with self.assertRaisesRegex(GateFailure, "must not contain symlinks"):
                    _built_binary("built target/release/fixture\n", package)

    def test_fixture_build_freezes_backend_and_ui_flags(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            harness = root / "harness"
            package = harness / "fixture"
            package.mkdir(parents=True)
            (package / "main.sg").write_text("pragma module;\n", encoding="utf-8")
            side_root = root / "side"
            side_root.mkdir()
            build_root = root / "build"
            surge = root / "surge"
            manifest = make_manifest()
            row = replace(manifest.rows[0], fixture="fixture/main.sg")
            manifest = replace(manifest, rows=(row,))

            def fake_run_checked(command: object, **kwargs: object) -> CommandResult:
                copied = build_root / "fixture-00"
                binary = copied / "target" / "release" / "fixture"
                binary.parent.mkdir(parents=True, exist_ok=True)
                binary.write_text("binary", encoding="utf-8")
                binary.chmod(0o700)
                return CommandResult("built target/release/fixture\n", "")

            with mock.patch(
                "runtime_v2_carrier_bench_runner.run_checked",
                side_effect=fake_run_checked,
            ) as checked:
                build_fixtures(
                    side_root=side_root,
                    harness_root=harness,
                    surge=surge,
                    manifest=manifest,
                    build_root=build_root,
                )
            command = checked.call_args.args[0]
            self.assertEqual(command[0:5], [
                str(surge),
                "build",
                "--release",
                "--backend=llvm",
                "--ui=off",
            ])

    def test_result_parser_requires_one_exact_record(self) -> None:
        row = make_manifest().rows[0]
        payload = {
            "schema_version": 1,
            "probe": row.probe,
            "operations": row.operations_per_batch,
            "elapsed_ns": 123,
            "operation_latencies_ns": [12] * row.operations_per_batch,
            "checksum": row.expected_checksum,
            "metrics": {"allocation_count": 0},
        }
        result = _parse_result(
            "SURGE_CARRIER_BENCH " + json.dumps(payload),
            row,
            {"allocation_count"},
        )
        self.assertEqual(result.elapsed_ns, 123)
        with self.assertRaisesRegex(GateFailure, "want exactly one"):
            _parse_result("", row, {"allocation_count"})
        with self.assertRaisesRegex(GateFailure, "want exactly one"):
            _parse_result(
                "noise\nSURGE_CARRIER_BENCH " + json.dumps(payload),
                row,
                {"allocation_count"},
            )
        payload["schema_version"] = True
        with self.assertRaisesRegex(GateFailure, "non-negative integer"):
            _parse_result(
                "SURGE_CARRIER_BENCH " + json.dumps(payload),
                row,
                {"allocation_count"},
            )
        payload["schema_version"] = 1
        one_operation = replace(row, operations_per_batch=1)
        payload["operations"] = True
        payload["operation_latencies_ns"] = [12]
        with self.assertRaisesRegex(GateFailure, "non-negative integer"):
            _parse_result(
                "SURGE_CARRIER_BENCH " + json.dumps(payload),
                one_operation,
                {"allocation_count"},
            )
        payload["operations"] = row.operations_per_batch
        payload["operation_latencies_ns"] = [12] * row.operations_per_batch
        duplicate = json.dumps(payload).replace(
            '"schema_version": 1',
            '"schema_version": 1, "schema_version": 1',
            1,
        )
        with self.assertRaisesRegex(GateFailure, "duplicate JSON key"):
            _parse_result(
                "SURGE_CARRIER_BENCH " + duplicate,
                row,
                {"allocation_count"},
            )
        payload["extra"] = 1
        with self.assertRaisesRegex(GateFailure, "fields mismatch"):
            _parse_result(
                "SURGE_CARRIER_BENCH " + json.dumps(payload),
                row,
                {"allocation_count"},
            )

    def test_result_parser_rejects_missing_metric(self) -> None:
        row = make_manifest().rows[0]
        payload = {
            "schema_version": 1,
            "probe": row.probe,
            "operations": row.operations_per_batch,
            "elapsed_ns": 123,
            "operation_latencies_ns": [12] * row.operations_per_batch,
            "checksum": row.expected_checksum,
            "metrics": {},
        }
        with self.assertRaisesRegex(GateFailure, "result metrics mismatch"):
            _parse_result(
                "SURGE_CARRIER_BENCH " + json.dumps(payload),
                row,
                {"allocation_count"},
            )

    def test_runtime_exit_metrics_are_typed_by_side(self) -> None:
        manifest = make_manifest()
        row = manifest.rows[0]
        nonce = "b" * 32
        protocol_sha256 = "a" * 64
        identity = {
            "expected_nonce": nonce,
            "expected_protocol_sha256": protocol_sha256,
        }
        self.assertEqual(
            _parse_runtime_counters("", row, manifest, "base", **identity),
            {
                "bytes_copied": None,
                "bytes_moved": None,
                "callback_count": None,
                "credit_stalls": None,
                "peak_transport_bytes": None,
            },
        )
        record = {
            "schema_version": 1,
            "status": "ok",
            "probe": row.probe,
            "nonce": nonce,
            "protocol_sha256": protocol_sha256,
            "metrics": {
                "bytes_copied": 42,
                "bytes_moved": 41,
                "callback_count": 7,
                "credit_stalls": 0,
                "peak_transport_bytes": 4096,
            },
            "error": None,
        }
        self.assertEqual(
            _parse_runtime_counters(
                RUNTIME_COUNTER_PREFIX + json.dumps(record),
                row,
                manifest,
                "candidate",
                **identity,
            ),
            record["metrics"],
        )
        with self.assertRaisesRegex(GateFailure, "no required runtime counter"):
            _parse_runtime_counters("", row, manifest, "candidate", **identity)
        with self.assertRaisesRegex(GateFailure, "unexpected runtime counter"):
            _parse_runtime_counters(
                RUNTIME_COUNTER_PREFIX + json.dumps(record),
                row,
                manifest,
                "base",
                **identity,
            )
        record["schema_version"] = True
        with self.assertRaisesRegex(GateFailure, "non-negative integer"):
            _parse_runtime_counters(
                RUNTIME_COUNTER_PREFIX + json.dumps(record),
                row,
                manifest,
                "candidate",
                **identity,
            )
        record["schema_version"] = 1
        duplicate = json.dumps(record).replace(
            '"schema_version": 1',
            '"schema_version": 1, "schema_version": 1',
            1,
        )
        with self.assertRaisesRegex(GateFailure, "duplicate JSON key"):
            _parse_runtime_counters(
                RUNTIME_COUNTER_PREFIX + duplicate,
                row,
                manifest,
                "candidate",
                **identity,
            )
        with self.assertRaisesRegex(GateFailure, "runtime metrics mismatch"):
            record["metrics"] = {}
            _parse_runtime_counters(
                RUNTIME_COUNTER_PREFIX + json.dumps(record),
                row,
                manifest,
                "candidate",
                **identity,
            )

        record["metrics"] = {
            "bytes_copied": 42,
            "bytes_moved": 41,
            "callback_count": 7,
            "credit_stalls": 0,
            "peak_transport_bytes": 4096,
        }
        record["nonce"] = "c" * 32
        with self.assertRaisesRegex(GateFailure, "nonce mismatch"):
            _parse_runtime_counters(
                RUNTIME_COUNTER_PREFIX + json.dumps(record),
                row,
                manifest,
                "candidate",
                **identity,
            )
        record["nonce"] = nonce
        record["protocol_sha256"] = "d" * 64
        with self.assertRaisesRegex(GateFailure, "protocol hash mismatch"):
            _parse_runtime_counters(
                RUNTIME_COUNTER_PREFIX + json.dumps(record),
                row,
                manifest,
                "candidate",
                **identity,
            )

    def test_batch_failure_records_exact_attempt_context(self) -> None:
        manifest = make_manifest()
        row = manifest.rows[0]
        events: list[dict[str, object]] = []
        fixture = BuiltFixture(Path("/nonexistent"), row.fixture)
        with mock.patch(
            "runtime_v2_carrier_bench_runner._run_batch",
            side_effect=GateFailure("command timed out"),
        ):
            with self.assertRaisesRegex(
                GateFailure,
                "row=scalar.channel phase=measured side=candidate run=3 batch=1",
            ):
                _run_recorded_batch(
                    manifest,
                    row,
                    "candidate",
                    fixture,
                    events,
                    phase="measured",
                    run_index=3,
                    batch_index=1,
                    protocol_sha256="a" * 64,
                )
        self.assertEqual(events[0]["status"], "failed")
        self.assertIn("command timed out", events[0]["failure"])

    def test_failed_cv_session_still_renders_all_raw_runs(self) -> None:
        manifest = make_manifest()
        row = manifest.rows[0]
        base = make_records("base", [800, 900, 1000, 1100, 1200, 1300, 3000])
        candidate = make_records("candidate", [1000] * 7)
        report, failure = render_report(
            attempt_id="attempt-1",
            started_at="2026-08-04T00:00:00.000000Z",
            ended_at="2026-08-04T00:00:01.000000Z",
            manifest=manifest,
            manifest_sha256="a" * 64,
            base_commit="1" * 40,
            candidate_commit="2" * 40,
            actual_host=manifest.reference,
            records={row.row_id: {"base": base, "candidate": candidate}},
        )
        self.assertIsNotNone(failure)
        self.assertEqual(report["status"], "failed")
        self.assertIsNotNone(report["rows"][0]["scores"])
        self.assertEqual(len(report["rows"][0]["base_runs"]), 7)
        self.assertEqual(len(report["rows"][0]["candidate_runs"]), 7)
        availability = report["metrics"][1]["base"]
        self.assertEqual(availability["status"], "unsupported")
        self.assertIn("reason", availability)
        self.assertEqual(availability["provenance_commit"], manifest.epic_base)

    def test_report_write_refuses_to_overwrite_an_attempt(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "attempt.json"
            write_report(path, {"status": "first"})
            with self.assertRaises(FileExistsError):
                write_report(path, {"status": "second"})
            self.assertEqual(
                json.loads(path.read_text(encoding="utf-8")), {"status": "first"}
            )

    def test_early_failure_writes_machine_readable_aborted_report(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            report_path = root / "report.json"
            arguments = Namespace(
                candidate_root=root,
                manifest=root / "testdata" / "runtime-v2-carrier-bench.json",
                report=report_path,
                base_root=None,
            )
            with mock.patch(
                "runtime_v2_carrier_bench._arguments", return_value=arguments
            ), mock.patch(
                "runtime_v2_carrier_bench._require_canonical_manifest",
                side_effect=ManifestError("bad manifest identity"),
            ):
                self.assertEqual(bench_main(), 1)
            report = json.loads(report_path.read_text(encoding="utf-8"))
            self.assertEqual(report["status"], "aborted")
            self.assertEqual(report["phase"], "manifest_identity")
            self.assertIn("bad manifest identity", report["failure"])

    def test_cpuset_format_is_canonical(self) -> None:
        self.assertEqual(_format_cpuset([0, 1, 2, 4, 7, 8]), "0-2,4,7-8")


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
        fixture="fixture.sg",
        probe="ping",
        operations_per_batch=10,
        batches=2,
        payload_bytes=8,
        timeout_seconds=5,
        relative_performance=True,
        expected_checksum="42",
        required_metrics=(
            "allocation_count",
            "bytes_copied",
            "bytes_moved",
            "callback_count",
            "credit_stalls",
            "peak_transport_bytes",
        ),
        invariants=(Invariant("allocation_count", "le", 0, "candidate"),),
    )
    return Manifest(
        schema_version=1,
        epic_base="0" * 40,
        reference=ReferenceHost("Linux", "x86_64", "kernel", "cpu", 2, "0-1", "go", "clang"),
        protocol=Protocol(2, 7, 0.05, 0.95, 1.10, "nearest-rank", "sample-n-minus-1"),
        backend="llvm",
        profile="release",
        shards=1,
        threads=1,
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
                "credit_stalls",
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
        harness_files=(),
        fixtures=(),
        rows=(row,),
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
        "credit_stalls": physical,
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


def make_records(side: str, latencies: list[int]) -> tuple[RunRecord, ...]:
    records: list[RunRecord] = []
    for index, latency in enumerate(latencies):
        measured = MeasuredRun(
            side=side,
            pair_index=index,
            timing=TimingSample(latency * 20, (latency,) * 20, 20),
            counters=CounterSample(counter_values(side)),
        )
        batches = (
            BatchResult(
                latency * 10,
                (latency,) * 10,
                "42",
                counter_values(side),
            ),
            BatchResult(
                latency * 10,
                (latency,) * 10,
                "42",
                counter_values(side),
            ),
        )
        records.append(RunRecord(measured, batches))
    return tuple(records)


def manifest_json() -> dict[str, object]:
    return {
        "schema_version": 1,
        "epic_base": "0" * 40,
        "reference_host": {
            "system": "Linux",
            "machine": "x86_64",
            "kernel_contains": "kernel",
            "cpu_model": "cpu",
            "logical_cpus": 2,
            "cpuset": "0-1",
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
        "shards": 1,
        "threads": 1,
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
                "name": "credit_stalls",
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
                "fixture": "fixture.sg",
                "probe": "ping",
                "operations_per_batch": 10,
                "batches": 2,
                "payload_bytes": 8,
                "timeout_seconds": 5,
                "relative_performance": True,
                "expected_checksum": "42",
                "required_metrics": [
                    "allocation_count",
                    "bytes_copied",
                    "bytes_moved",
                    "callback_count",
                    "credit_stalls",
                    "peak_transport_bytes",
                ],
                "invariants": [
                    {
                        "metric": "allocation_count",
                        "operator": "le",
                        "value": 0,
                        "side": "candidate",
                    }
                ],
            }
        ],
    }


if __name__ == "__main__":
    unittest.main()
