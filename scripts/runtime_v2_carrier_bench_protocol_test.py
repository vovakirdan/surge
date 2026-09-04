"""Focused Runtime V2 carrier benchmark tests."""

from runtime_v2_carrier_bench_test_support import *

class ProtocolTests(unittest.TestCase):
    def test_main_capture_mode_completes_real_red_but_strict_mode_aborts(self) -> None:
        manifest = load_manifest(
            SCRIPT_DIR.parent / "testdata" / "runtime-v2-carrier-bench.json"
        )
        candidate_commit = "c" * 40

        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            candidate_root = root / "candidate"
            base_root = root / "base"
            detached_candidate = root / "detached-candidate"
            for path in (candidate_root, base_root, detached_candidate):
                path.mkdir()
            manifest_path = (
                candidate_root / "testdata" / "runtime-v2-carrier-bench.json"
            )
            timeline: list[tuple[str, str, str]] = []

            @contextmanager
            def fake_commit_root(
                unused_repository: Path, unused_commit: str, label: str
            ):
                yield base_root if label == "base" else detached_candidate

            def fake_git_commit(path: Path) -> str:
                return manifest.epic_base if path == base_root else candidate_commit

            def fake_build_fixtures(**kwargs: object) -> dict[str, BuiltFixture]:
                capture_kind = str(kwargs["capture_kind"])
                side_root = Path(kwargs["side_root"])
                side = "base" if side_root == base_root else "candidate"
                timeline.append(("build", capture_kind, side))
                paths = {row.fixture for row in manifest.rows}
                if kwargs.get("include_allocation_control"):
                    paths.add(manifest.allocation_control.fixture)
                return {
                    path: BuiltFixture(
                        root / f"{capture_kind}-{side}-{index}", path
                    )
                    for index, path in enumerate(sorted(paths))
                }

            def fake_run_batch(
                unused_manifest: Manifest,
                row: Row,
                side: Side,
                unused_fixture: BuiltFixture,
                unused_protocol_sha256: str,
                capture_kind: str,
            ) -> BatchResult:
                timeline.append(("run", capture_kind, side))
                is_control = row.row_id == "allocation-control"
                if is_control:
                    allocation_count = 1
                elif capture_kind == "timing" and side == "candidate":
                    allocation_count = (
                        row.candidate_structural_allocations_per_batch + 1
                    )
                else:
                    allocation_count = 0
                counters = {
                    metric.name: (
                        allocation_count
                        if metric.source == "fixture"
                        else (None if capture_kind == "timing" else 0)
                    )
                    for metric in manifest.metrics
                }
                return BatchResult(
                    elapsed_ns=row.operations_per_batch * 1_000,
                    operation_latencies_ns=(1_000,) * row.operations_per_batch,
                    checksum=row.expected_checksum,
                    counters=counters,
                    nonce="f" * 32 if capture_kind == "resource" else "",
                )

            def run(capture: bool) -> tuple[int, dict[str, object], list[tuple[str, str, str]]]:
                timeline.clear()
                report_path = root / ("capture.json" if capture else "strict.json")
                argv = [
                    "runtime_v2_carrier_bench.py",
                    "--candidate-root",
                    str(candidate_root),
                    "--manifest",
                    str(manifest_path),
                    "--report",
                    str(report_path),
                    "--phase",
                    "wave-a",
                ]
                if capture:
                    argv.append("--capture-wave-a-baseline")
                with (
                    mock.patch.object(sys, "argv", argv),
                    mock.patch(
                        "runtime_v2_carrier_bench._require_canonical_manifest"
                    ),
                    mock.patch(
                        "runtime_v2_carrier_bench.load_manifest",
                        return_value=manifest,
                    ),
                    mock.patch(
                        "runtime_v2_carrier_bench.manifest_digest",
                        return_value="a" * 64,
                    ),
                    mock.patch(
                        "runtime_v2_carrier_bench.detect_host",
                        return_value=manifest.reference,
                    ),
                    mock.patch(
                        "runtime_v2_carrier_bench.require_reference_host"
                    ),
                    mock.patch(
                        "runtime_v2_carrier_bench.require_clean_worktree"
                    ),
                    mock.patch(
                        "runtime_v2_carrier_bench.git_commit",
                        side_effect=fake_git_commit,
                    ),
                    mock.patch("runtime_v2_carrier_bench._require_descendant"),
                    mock.patch("runtime_v2_carrier_bench._require_tracked_entries"),
                    mock.patch("runtime_v2_carrier_bench.verify_file_digests"),
                    mock.patch("runtime_v2_carrier_bench._verify_harness_inventory"),
                    mock.patch("runtime_v2_carrier_bench._verify_fixture_inventory"),
                    mock.patch(
                        "runtime_v2_carrier_bench._commit_root",
                        side_effect=fake_commit_root,
                    ),
                    mock.patch("runtime_v2_carrier_bench.build_surge"),
                    mock.patch(
                        "runtime_v2_carrier_bench.build_fixtures",
                        side_effect=fake_build_fixtures,
                    ),
                    mock.patch(
                        "runtime_v2_carrier_bench_runner._run_batch",
                        side_effect=fake_run_batch,
                    ),
                ):
                    result = bench_main()
                return (
                    result,
                    json.loads(report_path.read_text(encoding="utf-8")),
                    list(timeline),
                )

            capture_result, capture_report, capture_timeline = run(True)
            self.assertEqual(capture_result, 0)
            self.assertEqual(capture_report["status"], "failed")
            self.assertEqual(capture_report["execution_mode"], "wave-a-baseline-capture")
            self.assertEqual(capture_report["measurement_status"], "complete")
            self.assertEqual(capture_report["protocol_status"], "passed")
            self.assertEqual(capture_report["endpoint_invariant_status"], "failed")
            self.assertEqual(capture_report["allocation_budget_status"], "failed")
            self.assertEqual(capture_report["allocation_control_status"], "passed")
            self.assertEqual(capture_report["attempt_sequence_status"], "passed")
            self.assertEqual(len(capture_report["rows"]), len(manifest.rows))
            self.assertEqual(
                capture_report["row_count"], capture_report["expected_row_count"]
            )
            self.assertEqual(
                capture_report["attempt_count"],
                capture_report["expected_attempt_count"],
            )
            self.assertTrue(
                all(
                    len(row["base_runs"]) == manifest.protocol.measured_runs
                    and len(row["candidate_runs"])
                    == manifest.protocol.measured_runs
                    for row in capture_report["rows"]
                )
            )
            self.assertEqual(
                [event["attempt"] for event in capture_report["attempts"]],
                _expected_attempt_sequence(manifest),
            )
            expected_mismatches = sum(
                manifest.protocol.placements
                * (manifest.protocol.warmups + manifest.protocol.measured_pairs)
                * row.batches
                for row in manifest.rows
            )
            self.assertEqual(
                len(capture_report["allocation_mismatches"]),
                expected_mismatches,
            )

            first_timing_run = next(
                index
                for index, item in enumerate(capture_timeline)
                if item[:2] == ("run", "timing")
            )
            last_timing_run = max(
                index
                for index, item in enumerate(capture_timeline)
                if item[:2] == ("run", "timing")
            )
            resource_build = next(
                index
                for index, item in enumerate(capture_timeline)
                if item[:2] == ("build", "resource")
            )
            first_resource_run = next(
                index
                for index, item in enumerate(capture_timeline)
                if item[:2] == ("run", "resource")
            )
            self.assertTrue(
                all(
                    index < first_timing_run
                    for index, item in enumerate(capture_timeline)
                    if item[:2] == ("build", "timing")
                )
            )
            self.assertLess(last_timing_run, resource_build)
            self.assertLess(resource_build, first_resource_run)

            strict_result, strict_report, strict_timeline = run(False)
            self.assertEqual(strict_result, 1)
            self.assertEqual(strict_report["status"], "aborted")
            self.assertIn("want exact structural budget", strict_report["failure"])
            self.assertFalse(
                any(item[:2] == ("build", "resource") for item in strict_timeline)
            )

    def test_every_placement_is_a_distinct_file_of_the_same_bytes(self) -> None:
        # Owner ruling 2026-09-04: the copies a side is measured on must be
        # physically distinct files (a fresh file takes pages of its own) and
        # the built binary's exact bytes; a stand whose binary was never built
        # gets the same fixture back for every placement.
        from runtime_v2_carrier_bench_runner import place_fixture_copies

        with tempfile.TemporaryDirectory() as temporary:
            release = Path(temporary) / "target" / "release"
            release.mkdir(parents=True)
            binary = release / "main"
            binary.write_bytes(b"\x7fELF fixture bytes")
            built = BuiltFixture(binary, "fixture.sg")
            placed = place_fixture_copies({"base": {"fixture.sg": built}}, 3)
            copies = placed["base"]["fixture.sg"]
            self.assertEqual(len(copies), 3)
            self.assertIs(copies[0], built)
            paths = {copy.binary.resolve() for copy in copies}
            self.assertEqual(len(paths), 3)
            for copy in copies:
                self.assertTrue(copy.binary.is_file())
                self.assertEqual(copy.binary.read_bytes(), binary.read_bytes())
                self.assertEqual(copy.source_path, "fixture.sg")
            self.assertEqual(
                sorted(copy.binary.parent.name for copy in copies[1:]),
                ["placement-01", "placement-02"],
            )
            with self.assertRaisesRegex(GateFailure, "placements must be at least 1"):
                place_fixture_copies({"base": {"fixture.sg": built}}, 0)
        fake = BuiltFixture(Path("/never-built"), "fixture.sg")
        self.assertEqual(
            place_fixture_copies({"candidate": {"fixture.sg": fake}}, 6)["candidate"]["fixture.sg"],
            (fake,) * 6,
        )

    def test_execution_finishes_all_timing_before_candidate_resource_capture(self) -> None:
        manifest = make_manifest()
        row = manifest.rows[0]
        timing_fixture = BuiltFixture(Path("/timing"), row.fixture)
        resource_fixture = BuiltFixture(Path("/resource"), row.fixture)
        events: list[dict[str, object]] = []

        def run_batch(
            unused_manifest: Manifest,
            current_row: Row,
            side: Side,
            unused_fixture: BuiltFixture,
            unused_protocol_sha256: str,
            capture_kind: str,
        ) -> BatchResult:
            if current_row.row_id == "allocation-control":
                counters = counter_values(side, allocation_count=1)
                checksum = "1"
            else:
                counters = counter_values(side)
                checksum = current_row.expected_checksum
            if capture_kind == "timing":
                counters = {
                    name: value if name == "allocation_count" else None
                    for name, value in counters.items()
                }
            return BatchResult(
                elapsed_ns=10,
                operation_latencies_ns=(1,) * current_row.operations_per_batch,
                checksum=checksum,
                counters=counters,
                nonce="f" * 32 if capture_kind == "resource" else "",
            )

        with mock.patch(
            "runtime_v2_carrier_bench_runner._run_batch", side_effect=run_batch
        ):
            records, controls = execute_manifest(
                manifest,
                {
                    "base": {row.fixture: timing_fixture},
                    "candidate": {row.fixture: timing_fixture},
                },
                {row.fixture: resource_fixture},
                events,
                "a" * 64,
            )

        self.assertEqual(
            {side: result.counters["allocation_count"] for side, result in controls.items()},
            {"base": 1, "candidate": 1},
        )
        first_resource = next(
            index
            for index, event in enumerate(events)
            if event["capture_kind"] == "resource"
        )
        self.assertTrue(
            all(event["capture_kind"] == "timing" for event in events[:first_resource])
        )
        self.assertTrue(
            all(event["capture_kind"] == "resource" for event in events[first_resource:])
        )
        candidate = records[row.row_id]["candidate"][0]
        self.assertIsNone(candidate.batches[0].counters["bytes_copied"])
        self.assertEqual(candidate.resource_batches[0].counters["bytes_copied"], 0)
        self.assertEqual(candidate.measured.counters.values["bytes_copied"], 0)

    def test_candidate_warmup_allocation_drift_fails_before_resource_capture(self) -> None:
        manifest = make_manifest()
        row = manifest.rows[0]
        fixture = BuiltFixture(Path("/fixture"), row.fixture)
        events: list[dict[str, object]] = []

        def run_batch(
            unused_manifest: Manifest,
            current_row: Row,
            side: Side,
            unused_fixture: BuiltFixture,
            unused_protocol_sha256: str,
            capture_kind: str,
        ) -> BatchResult:
            control = current_row.row_id == "allocation-control"
            allocation_count = 1 if control or side == "candidate" else 0
            return BatchResult(
                elapsed_ns=10,
                operation_latencies_ns=(1,) * current_row.operations_per_batch,
                checksum="1" if control else current_row.expected_checksum,
                counters=counter_values(side, allocation_count=allocation_count),
            )

        with mock.patch(
            "runtime_v2_carrier_bench_runner._run_batch", side_effect=run_batch
        ), self.assertRaisesRegex(GateFailure, "want exact structural budget 0"):
            execute_manifest(
                manifest,
                {
                    "base": {row.fixture: fixture},
                    "candidate": {row.fixture: fixture},
                },
                {row.fixture: fixture},
                events,
                "a" * 64,
            )
        self.assertFalse(
            any(event["capture_kind"] == "resource" for event in events)
        )

    def test_allocation_oracle_rejects_stuck_zero_and_uniform_boxing(self) -> None:
        manifest = make_manifest()
        stuck_zero = BatchResult(1, (1,), "1", {"allocation_count": 0})
        with self.assertRaisesRegex(GateFailure, "want exact 1"):
            _validate_allocation_control(manifest, "candidate", stuck_zero)

        scalar = replace(
            manifest.rows[0], candidate_structural_allocations_per_batch=0
        )
        composite = replace(
            scalar,
            row_id="composite.channel",
            payload_role="composite",
            payload_bytes=64,
        )
        uniformly_boxed = BatchResult(
            1,
            (1,),
            "42",
            {"allocation_count": 1},
        )
        for row in (scalar, composite):
            with self.subTest(row=row.row_id), self.assertRaisesRegex(
                GateFailure, "want exact structural budget 0"
            ):
                _validate_structural_allocation(row, uniformly_boxed, 0)

    def test_capture_mode_rejects_missing_or_null_required_allocation_metric(self) -> None:
        row = make_manifest().rows[0]
        for counters in ({}, {"allocation_count": None}):
            with self.subTest(counters=counters):
                mismatches: list[AllocationMismatch] = []
                with self.assertRaisesRegex(
                    GateFailure,
                    "required allocation_count must be a non-negative integer",
                ):
                    _capture_or_validate_structural_allocation(
                        row,
                        BatchResult(1, (1,), row.expected_checksum, counters),
                        phase="warmup",
                        run_index=0,
                        batch_index=0,
                        capture_expected_endpoint_red=True,
                        allocation_mismatches=mismatches,
                    )
                self.assertEqual(mismatches, [])

    def test_attempt_identity_rejects_missing_duplicate_and_early_resource(self) -> None:
        manifest = make_manifest()
        expected = _expected_attempt_sequence(manifest)
        events = [{"attempt": item} for item in expected]
        _validate_attempt_sequence(manifest, events)
        mutations = (
            events[:-1],
            [events[0], *events],
            [events[-1], *events[:-1]],
        )
        for mutated in mutations:
            with self.assertRaisesRegex(GateFailure, "attempt sequence mismatch"):
                _validate_attempt_sequence(manifest, mutated)
        timing_events = [
            {"attempt": item}
            for item in _expected_timing_attempt_sequence(manifest)
        ]
        _validate_timing_attempt_sequence(manifest, timing_events)
        for mutated in (
            timing_events[:-1],
            [timing_events[0], *timing_events],
            [*timing_events[1:], timing_events[0]],
        ):
            with self.assertRaisesRegex(GateFailure, "attempt sequence mismatch"):
                _validate_timing_attempt_sequence(manifest, mutated)

    def test_timing_execution_cannot_enable_resource_counters(self) -> None:
        manifest = make_manifest()
        row = manifest.rows[0]
        fixture = BuiltFixture(Path("/fixture"), row.fixture)
        result_record = {
            "schema_version": 1,
            "probe": row.probe,
            "operations": row.operations_per_batch,
            "elapsed_ns": 100,
            "operation_latencies_ns": [10] * row.operations_per_batch,
            "checksum": row.expected_checksum,
            "metrics": {"allocation_count": 0},
        }
        nonce = "b" * 32
        counter_record = {
            "schema_version": 1,
            "status": "ok",
            "probe": row.probe,
            "nonce": nonce,
            "protocol_sha256": "a" * 64,
            "metrics": {
                "bytes_copied": 0,
                "bytes_moved": 0,
                "callback_count": 0,
                "data_slot_stalls": 0,
                "peak_transport_bytes": 0,
            },
            "error": None,
        }
        stdout = RESULT_PREFIX + json.dumps(result_record)
        with mock.patch(
            "runtime_v2_carrier_bench_runner.run_checked",
            side_effect=(
                CommandResult(stdout, ""),
                CommandResult(
                    stdout,
                    RUNTIME_COUNTER_PREFIX + json.dumps(counter_record),
                ),
            ),
        ) as checked, mock.patch(
            "runtime_v2_carrier_bench_runner.secrets.token_hex", return_value=nonce
        ):
            _run_batch(manifest, row, "candidate", fixture, "a" * 64, "timing")
            _run_batch(manifest, row, "candidate", fixture, "a" * 64, "resource")
        timing_environment = checked.call_args_list[0].kwargs["environment"]
        resource_environment = checked.call_args_list[1].kwargs["environment"]
        self.assertNotIn("SURGE_CARRIER_BENCH_COUNTERS", timing_environment)
        self.assertNotIn("SURGE_CARRIER_BENCH_NONCE", timing_environment)
        self.assertEqual(resource_environment["SURGE_CARRIER_BENCH_COUNTERS"], "1")
        self.assertEqual(resource_environment["SURGE_CARRIER_BENCH_NONCE"], nonce)

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
                "data_slot_stalls": None,
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
                "data_slot_stalls": 0,
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
            "data_slot_stalls": 0,
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
