"""Focused Runtime V2 carrier benchmark tests."""

from runtime_v2_carrier_bench_test_support import *
from runtime_v2_carrier_bench_model import (
    p95_cv_gated,
    p95_ratio_gated,
    score_side,
    validate_row_protocol,
)

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
                {**counter_values("candidate"), "data_slot_stalls": 1}
            ),
        )
        # The machinery, on a row that carries an invariant of its own: the
        # shared fake row carries none, because the loader refuses one on a
        # runtime-exit metric (owner ruling 2026-09-03).
        gated = replace(
            manifest.rows[0],
            invariants=(Invariant("data_slot_stalls", "le", 0, "candidate"),),
        )
        with self.assertRaisesRegex(GateFailure, "violates le 0"):
            validate_row_results(manifest, gated, base, bad_counter)


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

    def test_loader_refuses_a_gate_on_a_runtime_exit_metric(self) -> None:
        # OWNER RULING 2026-09-03 (E5): the five runtime-exit metrics are
        # reported telemetry and never a gate. The canonical manifest carries
        # no such invariant, so the shape test on it cannot fail when this
        # refusal is deleted from the loader; this one can. A row invariant
        # and a cross-row invariant on a telemetry metric are both refused,
        # each with the message that names the metric.
        def row_gate() -> dict[str, object]:
            raw = manifest_json()
            raw["rows"][0]["invariants"].append(
                {
                    "metric": "data_slot_stalls",
                    "operator": "eq",
                    "value": 0,
                    "side": "candidate",
                }
            )
            return raw

        def cross_row_gate() -> dict[str, object]:
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

        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "manifest.json"
            for label, build, metric in (
                ("row", row_gate, "data_slot_stalls"),
                ("cross-row", cross_row_gate, "bytes_moved"),
            ):
                with self.subTest(label=label):
                    path.write_text(json.dumps(build()), encoding="utf-8")
                    with self.assertRaisesRegex(
                        ManifestError, f"{metric}.*reported telemetry and not a gate"
                    ):
                        load_manifest(path)

    def test_loader_freezes_two_distinct_reference_cores(self) -> None:
        # The reference host's cpuset is two distinct cores, whichever host the
        # manifest names (the rule used to spell the workstation's "0,2"; the
        # host moved to the runner on 2026-09-03 and the rule says what it
        # always meant). One core, a repeated core and a range are refused.
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "manifest.json"
            for label, cpuset, ok in (
                ("two", "8,10", True),
                ("workstation", "0,2", True),
                ("one", "8", False),
                ("repeated", "8,8", False),
                ("range", "8-10", False),
            ):
                with self.subTest(label=label):
                    raw = manifest_json()
                    raw["reference_host"]["cpuset"] = cpuset
                    path.write_text(json.dumps(raw), encoding="utf-8")
                    if ok:
                        self.assertEqual(load_manifest(path).reference.cpuset, cpuset)
                    else:
                        with self.assertRaisesRegex(ManifestError, "two distinct cores"):
                            load_manifest(path)

    def test_loader_freezes_the_p95_cv_floor(self) -> None:
        # Owner ruling 2026-09-04 (variant "в"): the floor under which a p95
        # CV is a reading of the clock is 1000 ns, and the manifest cannot
        # move it or leave it out.
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "manifest.json"
            raw = manifest_json()
            path.write_text(json.dumps(raw), encoding="utf-8")
            self.assertEqual(load_manifest(path).protocol.p95_cv_floor_ns, 1000)
            for label, floor in (("lower", 999), ("higher", 1001), ("zero", 0)):
                with self.subTest(label=label):
                    raw = manifest_json()
                    raw["protocol"]["p95_cv_floor_ns"] = floor
                    path.write_text(json.dumps(raw), encoding="utf-8")
                    with self.assertRaisesRegex(ManifestError, "p95_cv_floor_ns"):
                        load_manifest(path)
            raw = manifest_json()
            del raw["protocol"]["p95_cv_floor_ns"]
            path.write_text(json.dumps(raw), encoding="utf-8")
            with self.assertRaises(ManifestError):
                load_manifest(path)

    @staticmethod
    def _runs_with_p95(side: str, tail_ns: list[int], *, elapsed_ns: list[int]) -> tuple[MeasuredRun, ...]:
        # Twenty operations per run: eighteen at the tail's floor and two at
        # the tail value, so nearest-rank p95 (rank 19 of 20) IS the tail,
        # while elapsed_ns is set independently so throughput can hold still
        # or move on its own.
        # A list shorter than the protocol's runs is one copy's shape, spread
        # over every copy (per_copy); eighteen values are taken as they are.
        return tuple(
            MeasuredRun(
                side=side,
                pair_index=index,
                timing=TimingSample(elapsed, (min(tail_ns),) * 18 + (tail, tail), 20),
                counters=CounterSample(counter_values(side)),
            )
            for index, (tail, elapsed) in enumerate(
                zip(per_copy(tail_ns), per_copy(elapsed_ns), strict=True)
            )
        )

    def test_side_score_is_the_median_clean_copy(self) -> None:
        # Owner rulings 2026-09-04: a fixture's speed is a property of the
        # physical pages its file landed on (byte-identical copies read 37..50
        # us per batch, and one base copy read 406 us where five read
        # 448-485), so a side is scored on the MEDIAN of its copies -- the
        # lower one of an even count -- and the CV that gates the row is that
        # copy's own.
        manifest = make_manifest()
        # One distinct elapsed per copy, in no particular order, so the
        # median is a definite copy: a pseudo-random permutation of
        # 1100, 1110, ..., 1100 + 10 * (PLACEMENTS - 1).
        by_copy = [1100 + 10 * ((7 * index + 3) % PLACEMENTS) for index in range(PLACEMENTS)]
        self.assertEqual(len(set(by_copy)), PLACEMENTS)

        def median_copy(elapsed_by_copy: list[int], exclude: set[int] = frozenset()) -> int:
            # The protocol's own reading of "median": the copies eligible,
            # ascending by throughput (descending by elapsed, ties by copy
            # index), the lower middle one.
            eligible = [index for index in range(PLACEMENTS) if index not in exclude]
            ascending = sorted(eligible, key=lambda index: (-elapsed_by_copy[index], index))
            return ascending[(len(ascending) - 1) // 2]

        elapsed = [value for value in by_copy for _ in range(MEASURED_PAIRS)]
        runs = self._runs_with_p95("base", [70] * MEASURED_RUNS, elapsed_ns=elapsed)
        score = score_side(runs, MEASURED_PAIRS)
        expected = median_copy(by_copy)
        self.assertEqual(score.placement, expected)
        self.assertEqual(score.throughput, runs[expected * MEASURED_PAIRS].timing.throughput())
        self.assertEqual(len(score.placement_throughputs), PLACEMENTS)
        self.assertEqual(score.throughput_cv, 0.0)
        self.assertNotEqual(expected, by_copy.index(min(by_copy)))  # not the fastest
        # The median copy wanders on its own (its last pair reads 25 %
        # slower, a throughput CV of about 0.09 over its five pairs): with
        # the protocol in hand it is not a reading, the median is taken over
        # the clean copies, and the report says which were.
        elapsed[(expected + 1) * MEASURED_PAIRS - 1] = int(by_copy[expected] * 1.25)
        wandering = self._runs_with_p95("base", [70] * MEASURED_RUNS, elapsed_ns=elapsed)
        clean = score_side(wandering, MEASURED_PAIRS, manifest.protocol)
        rechosen = median_copy(by_copy, exclude={expected})
        self.assertNotEqual(rechosen, expected)
        self.assertEqual(clean.placement, rechosen)
        self.assertEqual(clean.throughput, wandering[rechosen * MEASURED_PAIRS].timing.throughput())
        self.assertEqual(clean.throughput_cv, 0.0)
        self.assertEqual(
            clean.placement_clean, tuple(index != expected for index in range(PLACEMENTS))
        )
        # Without the protocol the wandering copy still counts, and the
        # median lands on it again (its median pair did not move).
        self.assertEqual(score_side(wandering, MEASURED_PAIRS).placement, expected)
        self.assertGreater(score_side(wandering, MEASURED_PAIRS).throughput_cv, 0.05)
        # Ungrouped (the pairs-per-copy left unsaid) the same runs are one
        # copy, scored as before this ruling.
        self.assertEqual(score_side(runs).placement, 0)
        self.assertEqual(len(score_side(runs).placement_throughputs), 1)

    def test_p95_cv_gates_only_at_or_above_the_timer_floor(self) -> None:
        # The runner's own shape: seven p95 readings one clock tick apart,
        # [60, 70, 70, 70, 70, 70, 70] ns, are a 5.5 % CV of a 10 ns tick. Below
        # the floor that is not a gate; the same seven readings scaled a
        # hundredfold, 6000/7000 ns, are the runtime's and still are.
        manifest = make_manifest()
        row = manifest.rows[0]
        steady = [1200] * 7
        candidate = self._runs_with_p95("candidate", [70] * 7, elapsed_ns=steady)
        # One copy's shape (MEASURED_PAIRS readings, the first a tick lower),
        # repeated on every copy so no copy is cleaner than another.
        tick = [60] + [70] * (MEASURED_PAIRS - 1)
        quantized = self._runs_with_p95("base", tick, elapsed_ns=steady)
        base_score, _ = validate_row_protocol(manifest, row, quantized, candidate)
        self.assertGreater(base_score.p95_cv, 0.05)
        self.assertFalse(p95_cv_gated(manifest.protocol, base_score))
        real = self._runs_with_p95("base", [100 * value for value in tick], elapsed_ns=steady)
        real_candidate = self._runs_with_p95("candidate", [7000] * 7, elapsed_ns=steady)
        with self.assertRaisesRegex(GateFailure, "base p95 CV .* at or above the 1000 ns floor"):
            validate_row_protocol(manifest, row, real, real_candidate)
        at_floor = self._runs_with_p95("base", [900] + [1000] * 6, elapsed_ns=steady)
        floor_score = score_side(at_floor)
        self.assertEqual(floor_score.p95_ns, 1000.0)
        self.assertTrue(p95_cv_gated(manifest.protocol, floor_score))

    def test_p95_ratio_gates_only_where_both_sides_reach_the_floor(self) -> None:
        # The runner's own reading (attempts 7 and 9): array-grow-scalar's p95
        # is 41 ns on the base and 50 on the candidate, one tick of the clock
        # apart, and their ratio 1.22 is not a reading (owner ruling
        # 2026-09-04, the third). The same shape at 4100 / 5000 ns is the
        # runtime's and still refuses.
        manifest = make_manifest()
        row = manifest.rows[0]
        steady = [1200] * 7
        base = self._runs_with_p95("base", [41] * 7, elapsed_ns=steady)
        candidate = self._runs_with_p95("candidate", [50] * 7, elapsed_ns=steady)
        base_score, candidate_score = validate_row_protocol(manifest, row, base, candidate)
        self.assertGreater(candidate_score.p95_ns / base_score.p95_ns, 1.1)
        self.assertFalse(p95_ratio_gated(manifest.protocol, base_score, candidate_score))
        real_base = self._runs_with_p95("base", [4100] * 7, elapsed_ns=steady)
        real_candidate = self._runs_with_p95("candidate", [5000] * 7, elapsed_ns=steady)
        with self.assertRaisesRegex(GateFailure, "p95 ratio 1.2195.. above"):
            validate_row_protocol(manifest, row, real_base, real_candidate)
        # One side at the floor and the other below it is still below it.
        mixed = self._runs_with_p95("candidate", [1200] * 7, elapsed_ns=steady)
        low = self._runs_with_p95("base", [900] * 7, elapsed_ns=steady)
        low_score, mixed_score = validate_row_protocol(manifest, row, low, mixed)
        self.assertFalse(p95_ratio_gated(manifest.protocol, low_score, mixed_score))

    def test_a_row_reads_its_own_throughput_budget_where_the_owner_set_one(self) -> None:
        # Owner ruling 2026-09-04 (the sixth): select-send-scalar carries a
        # 0.90 budget of its own; every other row reads the protocol's 0.95,
        # and the loader refuses a row budget anywhere else or at any other
        # number.
        manifest = make_manifest()
        row = manifest.rows[0]
        base = make_runs("base", latency=1_000)
        candidate = make_runs("candidate", latency=1_080)  # 0.926
        with self.assertRaisesRegex(GateFailure, "throughput ratio 0.92.* below 0.950000"):
            validate_row_protocol(manifest, row, base, candidate)
        budgeted = replace(row, row_id="select-send-scalar", throughput_min_ratio=0.90)
        validate_row_protocol(manifest, budgeted, base, candidate)
        slower = make_runs("candidate", latency=1_120)  # 0.893
        with self.assertRaisesRegex(GateFailure, "below 0.900000 \\(the row's own budget\\)"):
            validate_row_protocol(manifest, budgeted, base, slower)
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "manifest.json"
            for label, row_id, budget in (
                ("another row", "scalar.channel", 0.90),
                ("another number", "select-send-scalar", 0.85),
            ):
                with self.subTest(label=label):
                    raw = manifest_json()
                    raw["rows"][0]["id"] = row_id
                    raw["rows"][0]["throughput_min_ratio"] = budget
                    path.write_text(json.dumps(raw), encoding="utf-8")
                    with self.assertRaisesRegex(ManifestError, "throughput budget of its own"):
                        load_manifest(path)
            for row_id in ("select-send-scalar", "array-teardown-scalar"):
                raw = manifest_json()
                raw["rows"][0]["id"] = row_id
                raw["rows"][0]["throughput_min_ratio"] = 0.90
                path.write_text(json.dumps(raw), encoding="utf-8")
                self.assertEqual(load_manifest(path).rows[0].throughput_min_ratio, 0.90)

    def test_throughput_cv_still_gates_a_row_below_the_floor(self) -> None:
        # Rule 13 in the other direction: waiving the p95 CV below the floor
        # does not waive the row. A sub-microsecond row whose batch elapsed
        # wanders 6 % is red on throughput CV exactly as before.
        manifest = make_manifest()
        row = manifest.rows[0]
        candidate = self._runs_with_p95("candidate", [70] * 7, elapsed_ns=[1200] * 7)
        # Every copy wanders (one pair in five at 1400 against 1200), so no
        # copy is clean, the fastest is the row's reading, and the gate names
        # it (owner ruling 2026-09-04, both halves).
        one_copy = [1200] * (MEASURED_PAIRS - 1) + [1400]
        wandering = self._runs_with_p95("base", [70] * 7, elapsed_ns=one_copy)
        with self.assertRaisesRegex(GateFailure, r"base throughput CV .*copy \d"):
            validate_row_protocol(manifest, row, wandering, candidate)
        # The same wander in one copy alone is that copy's, not the row's:
        # the row is read from the median CLEAN copy (one of the seven at
        # 1300) and does not gate.
        elsewhere = self._runs_with_p95(
            "base", [70] * 7, elapsed_ns=one_copy + [1300] * (MEASURED_RUNS - MEASURED_PAIRS)
        )
        base_score, _ = validate_row_protocol(manifest, row, elsewhere, candidate)
        self.assertNotEqual(base_score.placement, 0)
        self.assertEqual(base_score.throughput, elsewhere[MEASURED_PAIRS].timing.throughput())
        self.assertEqual(base_score.throughput_cv, 0.0)
        self.assertEqual(base_score.placement_clean[0], False)

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

    def test_loader_refuses_a_liveness_probe_and_holds_a_probes_shape(self) -> None:
        # Owner ruling 2026-09-04: the manifest carries NO probe -- the two it
        # had waited on a point nothing armed and emitted a record nothing
        # wrote -- so ANY probe in the file is refused. The shape rules that
        # outlive them are exercised through the validator directly, on the
        # probe a future row would have to satisfy.
        probe_json = {
            "id": "park-probe",
            "fixture": "fixture.sg",
            "probe": "park-probe",
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
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "manifest.json"
            raw = manifest_json()
            raw["liveness_probes"] = [probe_json]
            path.write_text(json.dumps(raw), encoding="utf-8")
            with self.assertRaisesRegex(ManifestError, "liveness probes must be empty"):
                load_manifest(path)

            # Every probe is refused, whatever its shape: a well-formed one
            # and a malformed one read the same refusal, which is the point.
            raw["liveness_probes"] = [
                {**probe_json, "expected_reply_reserved": 1, "final": {"status": "deferred",
                 "reason": "not yet", "provenance_commit": "0" * 40}}
            ]
            path.write_text(json.dumps(raw), encoding="utf-8")
            with self.assertRaisesRegex(ManifestError, "liveness probes must be empty"):
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
