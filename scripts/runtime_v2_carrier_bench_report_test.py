"""Focused Runtime V2 carrier benchmark tests."""

from runtime_v2_carrier_bench_test_support import *

class ReportTests(unittest.TestCase):
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
            benchmark_phase="wave-a",
            liveness_records=(deferred_liveness(),),
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

    def test_report_separates_protocol_from_cross_row_endpoint_failure(self) -> None:
        manifest = make_manifest()
        scalar = replace(
            manifest.rows[0],
            row_id="task-scalar",
            workload_family="task-single",
            relative_performance=False,
            invariants=(),
        )
        composite = replace(
            scalar,
            row_id="task-composite",
            payload_role="composite",
            payload_bytes=64,
        )
        comparison = CrossRowInvariant(
            invariant_id="task-no-per-value-box",
            relation="paired_payload",
            metric="allocation_count",
            left_row=composite.row_id,
            left_reduction="max",
            operator="le",
            right_row=scalar.row_id,
            right_reduction="min",
            side="candidate",
        )
        manifest = replace(
            manifest,
            rows=(scalar, composite),
            cross_row_invariants=(comparison,),
        )
        records = {
            scalar.row_id: {
                "base": make_records("base", [1000] * 7),
                "candidate": make_records("candidate", [1000] * 7),
            },
            composite.row_id: {
                "base": make_records("base", [1000] * 7),
                "candidate": make_records(
                    "candidate", [1000] * 7, allocation_count=1
                ),
            },
        }
        report, failure = render_report(
            attempt_id="attempt-1",
            started_at="2026-08-04T00:00:00.000000Z",
            ended_at="2026-08-04T00:00:01.000000Z",
            manifest=manifest,
            manifest_sha256="a" * 64,
            base_commit="1" * 40,
            candidate_commit="2" * 40,
            actual_host=manifest.reference,
            records=records,
            benchmark_phase="wave-a",
            liveness_records=(deferred_liveness(),),
        )
        self.assertIsNotNone(failure)
        self.assertEqual(report["protocol_status"], "passed")
        self.assertEqual(report["endpoint_invariant_status"], "failed")
        comparison_report = report["cross_row_invariants"][0]
        self.assertEqual(comparison_report["status"], "failed")
        self.assertEqual(comparison_report["left"]["value"], 1)
        self.assertEqual(comparison_report["right"]["value"], 0)

    def test_payload_proportional_is_pointwise_not_extrema(self) -> None:
        manifest = make_manifest()
        small = replace(
            manifest.rows[0],
            row_id="capture-small",
            workload_family="far-capture",
            payload_role="composite",
            payload_bytes=16,
            relative_performance=False,
            invariants=(),
        )
        large = replace(small, row_id="capture-large", payload_bytes=8192)
        comparison = CrossRowInvariant(
            invariant_id="capture-byte-scaling",
            relation="payload_proportional",
            metric="bytes_moved",
            left_row=small.row_id,
            left_reduction="max",
            operator="eq",
            right_row=large.row_id,
            right_reduction="max",
            side="candidate",
        )
        manifest = replace(
            manifest,
            rows=(large, small),
            cross_row_invariants=(comparison,),
        )
        base = make_records("base", [1000] * 7)
        small_candidate = records_with_batch_metric(
            make_records("candidate", [1000] * 7), "bytes_moved", (8, 16)
        )
        large_candidate = records_with_batch_metric(
            make_records("candidate", [1000] * 7), "bytes_moved", (8192, 8192)
        )
        report, failure = render_report(
            attempt_id="attempt-pointwise",
            started_at="2026-08-04T00:00:00.000000Z",
            ended_at="2026-08-04T00:00:01.000000Z",
            manifest=manifest,
            manifest_sha256="a" * 64,
            base_commit="1" * 40,
            candidate_commit="2" * 40,
            actual_host=manifest.reference,
            records={
                small.row_id: {"base": base, "candidate": small_candidate},
                large.row_id: {"base": base, "candidate": large_candidate},
            },
            benchmark_phase="wave-a",
            liveness_records=(deferred_liveness(),),
        )
        self.assertIsNotNone(failure)
        invariant = report["cross_row_invariants"][0]
        self.assertEqual(invariant["status"], "failed")
        self.assertIn("pair=0 batch=0", invariant["failure"])
        self.assertEqual(invariant["pointwise_comparisons"][0]["status"], "failed")
        self.assertEqual(
            invariant["left"]["batch_values"][0],
            {"pair_index": 0, "batch_index": 0, "value": 8},
        )

    def test_liveness_record_enforces_exact_park_and_peak_boundaries(self) -> None:
        probe = make_manifest().liveness_probes[0]
        nonce = "b" * 32
        protocol = "a" * 64
        record = {
            "schema_version": 1,
            "status": "ok",
            "probe": probe.probe,
            "nonce": nonce,
            "protocol_sha256": protocol,
            "syncpoint": probe.syncpoint,
            "credit_balance": 0,
            "peak_transport_bytes": probe.min_peak_transport_bytes,
            "park_transitions": 1,
            "error": None,
        }
        for peak in (probe.min_peak_transport_bytes, probe.max_peak_transport_bytes):
            with self.subTest(peak=peak):
                record["peak_transport_bytes"] = peak
                parsed = _parse_liveness_record(
                    "",
                    LIVENESS_PREFIX + json.dumps(record),
                    probe,
                    expected_nonce=nonce,
                    expected_protocol_sha256=protocol,
                )
                self.assertEqual(parsed.peak_transport_bytes, peak)
        record["peak_transport_bytes"] = probe.max_peak_transport_bytes + 1
        with self.assertRaisesRegex(GateFailure, "outside"):
            _parse_liveness_record(
                "",
                LIVENESS_PREFIX + json.dumps(record),
                probe,
                expected_nonce=nonce,
                expected_protocol_sha256=protocol,
            )
        record["peak_transport_bytes"] = probe.max_peak_transport_bytes
        record["park_transitions"] = 2
        with self.assertRaisesRegex(GateFailure, "park transitions 2, want 1"):
            _parse_liveness_record(
                "",
                LIVENESS_PREFIX + json.dumps(record),
                probe,
                expected_nonce=nonce,
                expected_protocol_sha256=protocol,
            )

    def test_cross_row_raw_batch_failures_are_typed(self) -> None:
        manifest = make_manifest()
        row = manifest.rows[0]
        records = {
            row.row_id: {
                "base": make_records("base", [1000] * 7),
                "candidate": make_records("candidate", [1000] * 7),
            }
        }
        with self.assertRaisesRegex(
            GateFailure, "missing records for row=missing side=candidate"
        ):
            _batch_metric_values(records, "missing", "candidate", "allocation_count")
        records[row.row_id]["candidate"] = ()
        with self.assertRaisesRegex(
            GateFailure, "has no runs for row=scalar.channel side=candidate"
        ):
            _batch_metric_values(
                records, row.row_id, "candidate", "allocation_count"
            )
        records[row.row_id]["candidate"] = (
            replace(make_records("candidate", [1000])[0], batches=()),
        )
        with self.assertRaisesRegex(
            GateFailure, "has no batches for row=scalar.channel side=candidate run=0"
        ):
            _batch_metric_values(
                records, row.row_id, "candidate", "allocation_count"
            )
        record = make_records("candidate", [1000])[0]
        missing_metric = replace(
            record.batches[0],
            counters={
                name: value
                for name, value in record.batches[0].counters.items()
                if name != "allocation_count"
            },
        )
        records[row.row_id]["candidate"] = (
            replace(record, batches=(missing_metric, record.batches[1])),
        )
        with self.assertRaisesRegex(
            GateFailure,
            "metric allocation_count is missing from row=scalar.channel "
            "side=candidate run=0 batch=0",
        ):
            _batch_metric_values(
                records, row.row_id, "candidate", "allocation_count"
            )

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
