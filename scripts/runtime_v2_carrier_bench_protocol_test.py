"""Focused Runtime V2 carrier benchmark tests."""

from runtime_v2_carrier_bench_test_support import *

class ProtocolTests(unittest.TestCase):
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
