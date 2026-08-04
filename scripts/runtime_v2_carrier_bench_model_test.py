"""Focused Runtime V2 carrier benchmark tests."""

from runtime_v2_carrier_bench_test_support import *

class ModelTests(unittest.TestCase):
    def test_wave_a_baseline_capture_preserves_red_and_protocol_boundary(self) -> None:
        endpoint_red = {
            "status": "failed",
            "execution_mode": "wave-a-baseline-capture",
            "measurement_status": "complete",
            "benchmark_phase": "wave-a",
            "row_count": 1,
            "expected_row_count": 1,
            "attempt_count": 1,
            "expected_attempt_count": 1,
            "allocation_control_status": "passed",
            "attempt_sequence_status": "passed",
            "protocol_status": "passed",
            "endpoint_invariant_status": "failed",
        }
        self.assertTrue(
            _baseline_capture_accepts(
                endpoint_red, benchmark_phase="wave-a", requested=True
            )
        )
        for phase, requested, protocol in (
            ("final", True, "passed"),
            ("wave-a", False, "passed"),
            ("wave-a", True, "failed"),
        ):
            report = {**endpoint_red, "protocol_status": protocol}
            self.assertFalse(
                _baseline_capture_accepts(
                    report, benchmark_phase=phase, requested=requested
                )
            )
        for field in (
            "execution_mode",
            "measurement_status",
            "allocation_control_status",
            "attempt_sequence_status",
        ):
            with self.subTest(field=field):
                report = {**endpoint_red, field: "failed"}
                self.assertFalse(
                    _baseline_capture_accepts(
                        report, benchmark_phase="wave-a", requested=True
                    )
                )
        for actual_field, expected_field in (
            ("row_count", "expected_row_count"),
            ("attempt_count", "expected_attempt_count"),
        ):
            with self.subTest(actual_field=actual_field):
                report = {**endpoint_red, actual_field: 0, expected_field: 1}
                self.assertFalse(
                    _baseline_capture_accepts(
                        report, benchmark_phase="wave-a", requested=True
                    )
                )

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
            counters=CounterSample(
                {**counter_values("candidate"), "credit_stalls": 1}
            ),
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

    def test_loader_rejects_malformed_cross_row_contracts(self) -> None:
        def with_comparison() -> dict[str, object]:
            raw = manifest_json()
            second = json.loads(json.dumps(raw["rows"][0]))
            second["id"] = "composite.channel"
            second["payload_role"] = "composite"
            second["payload_bytes"] = 8192
            raw["rows"][0]["payload_role"] = "composite"
            raw["rows"][0]["payload_bytes"] = 64
            raw["rows"].append(second)
            raw["cross_row_invariants"] = [
                {
                    "id": "channel-byte-scaling",
                    "relation": "payload_proportional",
                    "metric": "bytes_moved",
                    "left_row": "scalar.channel",
                    "left_reduction": "max",
                    "operator": "eq",
                    "right_row": "composite.channel",
                    "right_reduction": "max",
                    "side": "candidate",
                }
            ]
            return raw

        def same_row(raw: dict[str, object]) -> None:
            raw["cross_row_invariants"][0]["right_row"] = "scalar.channel"

        def unknown_row(raw: dict[str, object]) -> None:
            raw["cross_row_invariants"][0]["right_row"] = "missing.channel"

        def unknown_metric(raw: dict[str, object]) -> None:
            raw["cross_row_invariants"][0]["metric"] = "unknown"

        def unmatched_work(raw: dict[str, object]) -> None:
            raw["rows"][1]["operations_per_batch"] += 1

        cases = (
            ("same", same_row, "distinct rows"),
            ("row", unknown_row, "unknown row"),
            ("metric", unknown_metric, "unknown metric"),
            ("work", unmatched_work, "matched workloads"),
        )
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "manifest.json"
            for label, mutate, expected in cases:
                with self.subTest(label=label):
                    raw = with_comparison()
                    mutate(raw)
                    path.write_text(json.dumps(raw), encoding="utf-8")
                    with self.assertRaisesRegex(ManifestError, expected):
                        load_manifest(path)

    def test_loader_freezes_cross_row_relation_shapes(self) -> None:
        def proportional() -> dict[str, object]:
            raw = manifest_json()
            small = raw["rows"][0]
            small["id"] = "capture-small"
            small["payload_role"] = "composite"
            small["payload_bytes"] = 16
            large = json.loads(json.dumps(small))
            large["id"] = "capture-large"
            large["payload_bytes"] = 8192
            raw["rows"] = [large, small]
            raw["cross_row_invariants"] = [
                {
                    "id": "capture-byte-scaling",
                    "relation": "payload_proportional",
                    "metric": "bytes_moved",
                    "left_row": "capture-small",
                    "left_reduction": "max",
                    "operator": "eq",
                    "right_row": "capture-large",
                    "right_reduction": "max",
                    "side": "candidate",
                }
            ]
            return raw

        mutations = (
            (proportional, "left_reduction", "min", "payload_proportional"),
            (proportional, "operator", "le", "payload_proportional"),
            (proportional, "right_reduction", "min", "payload_proportional"),
            (proportional, "side", "base", "unsupported metric"),
        )
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "manifest.json"
            for factory, field, value, expected in mutations:
                with self.subTest(field=field, value=value):
                    raw = factory()
                    raw["cross_row_invariants"][0][field] = value
                    path.write_text(json.dumps(raw), encoding="utf-8")
                    with self.assertRaisesRegex(ManifestError, expected):
                        load_manifest(path)

    def test_loader_rejects_second_allocation_count_contract(self) -> None:
        def duplicate_row(raw: dict[str, object]) -> None:
            raw["rows"][0]["invariants"].append(
                {
                    "metric": "allocation_count",
                    "operator": "eq",
                    "value": raw["rows"][0][
                        "candidate_structural_allocations_per_batch"
                    ],
                    "side": "candidate",
                }
            )

        def duplicate_cross_row(raw: dict[str, object]) -> None:
            composite = json.loads(json.dumps(raw["rows"][0]))
            composite["id"] = "composite.channel"
            composite["payload_role"] = "composite"
            composite["payload_bytes"] = 64
            raw["rows"].append(composite)
            raw["cross_row_invariants"] = [
                {
                    "id": "duplicate-allocation-contract",
                    "relation": "paired_payload",
                    "metric": "allocation_count",
                    "left_row": "composite.channel",
                    "left_reduction": "max",
                    "operator": "le",
                    "right_row": "scalar.channel",
                    "right_reduction": "min",
                    "side": "candidate",
                }
            ]

        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "manifest.json"
            for label, mutation in (
                ("row", duplicate_row),
                ("cross-row", duplicate_cross_row),
            ):
                with self.subTest(label=label):
                    raw = manifest_json()
                    mutation(raw)
                    path.write_text(json.dumps(raw), encoding="utf-8")
                    with self.assertRaisesRegex(
                        ManifestError, "duplicates the exact allocation_count contract"
                    ):
                        load_manifest(path)

    def test_loader_freezes_transport_and_liveness_boundaries(self) -> None:
        mutations = (
            ("transport_budget", "max_inline_overhead_bytes", 255, "transport budget"),
            ("liveness_probes", "expected_credit_balance", 1, "zero credit"),
            ("liveness_probes", "min_peak_transport_bytes", 8191, "lower bound"),
            ("liveness_probes", "max_peak_transport_bytes", 9473, "upper bound"),
        )
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "manifest.json"
            for section, field, value, expected in mutations:
                with self.subTest(section=section, field=field):
                    raw = manifest_json()
                    target = (
                        raw[section]
                        if section == "transport_budget"
                        else raw[section][0]
                    )
                    target[field] = value
                    path.write_text(json.dumps(raw), encoding="utf-8")
                    with self.assertRaisesRegex(ManifestError, expected):
                        load_manifest(path)

    def test_loader_rejects_duplicate_json_keys(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "manifest.json"
            encoded = json.dumps(manifest_json())
            encoded = encoded.replace(
                '"schema_version": 2',
                '"schema_version": 2, "schema_version": 2',
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
