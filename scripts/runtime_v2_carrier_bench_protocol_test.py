"""Focused Runtime V2 carrier benchmark tests."""

from runtime_v2_carrier_bench_test_support import *

class ProtocolTests(unittest.TestCase):
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
                "credit_stalls": 0,
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
