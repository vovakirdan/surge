"""Focused Runtime V2 carrier benchmark tests."""

from runtime_v2_carrier_bench_test_support import *

class BuildAndIRTests(unittest.TestCase):
    def test_liveness_fixture_waits_on_reached_counts_before_actions(self) -> None:
        source = (
            SCRIPT_DIR.parent
            / "testdata"
            / "runtime-v2-carrier-bench"
            / "scored"
            / "liveness"
            / "main.sg"
        ).read_text(encoding="utf-8")
        admitted = "while !rt_carrier_liveness_jumbo_admitted()"
        parked = "while !rt_carrier_liveness_credit_parked()"
        first_spawn = source.index("let first: far Task<uint64>")
        admitted_wait = source.index(admitted)
        second_spawn = source.index("let second: far Task<uint64>")
        parked_wait = source.index(parked)
        cancel = source.index("let _ = second.cancel()")
        shutdown = source.index("else if !rt_carrier_liveness_request_shutdown()")
        release = source.index("rt_carrier_liveness_release_jumbo();")
        self.assertLess(first_spawn, admitted_wait)
        self.assertLess(admitted_wait, second_spawn)
        self.assertLess(second_spawn, parked_wait)
        self.assertLess(parked_wait, cancel)
        self.assertLess(parked_wait, shutdown)
        self.assertLess(cancel, release)
        self.assertLess(shutdown, release)

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
            ) as checked, mock.patch(
                "runtime_v2_carrier_bench_runner._verify_fixture_source"
            ), mock.patch("runtime_v2_carrier_bench_runner._verify_fixture_ir"):
                build_fixtures(
                    side_root=side_root,
                    harness_root=harness,
                    surge=surge,
                    manifest=manifest,
                    build_root=build_root,
                )
            command = checked.call_args.args[0]
            self.assertEqual(
                command[0:7],
                [
                    str(surge),
                    "build",
                    "--release",
                    "--backend=llvm",
                    "--ui=off",
                    "--emit-llvm",
                    "--keep-tmp",
                ],
            )
            self.assertEqual(
                command[7], str(build_root / "fixture-00" / "package" / "main.sg")
            )
            self.assertEqual(
                checked.call_args.kwargs["cwd"],
                build_root / "fixture-00",
            )

    def test_liveness_build_is_armed_and_runner_blocks_first_jumbo(self) -> None:
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
            probe = replace(
                manifest.liveness_probes[0], fixture="fixture/main.sg"
            )
            manifest = replace(manifest, liveness_probes=(probe,))

            def fake_build(command: object, **kwargs: object) -> CommandResult:
                copied = build_root / "liveness-00"
                binary = copied / "target" / "release" / "fixture"
                binary.parent.mkdir(parents=True, exist_ok=True)
                binary.write_text("binary", encoding="utf-8")
                binary.chmod(0o700)
                return CommandResult("built target/release/fixture\n", "")

            with mock.patch(
                "runtime_v2_carrier_bench_runner.run_checked",
                side_effect=fake_build,
            ) as checked:
                fixtures = build_liveness_fixtures(
                    side_root=side_root,
                    harness_root=harness,
                    surge=surge,
                    manifest=manifest,
                    build_root=build_root,
                )
            self.assertEqual(
                checked.call_args.kwargs["environment"],
                {
                    "SURGE_INTERNAL_TEST_SYNC_POINTS": "1",
                    "SURGE_STDLIB": str(side_root),
                },
            )

            fixture = fixtures[probe.fixture]
            sentinel = LivenessRecord(
                probe.probe_id, "passed", probe.syncpoint, 0, 8192, 1, None, None
            )
            with mock.patch(
                "runtime_v2_carrier_bench_runner.run_checked",
                return_value=CommandResult("", ""),
            ) as checked, mock.patch(
                "runtime_v2_carrier_bench_runner._parse_liveness_record",
                return_value=sentinel,
            ):
                result = _run_liveness_probe(
                    manifest, probe, fixture, "a" * 64
                )
            self.assertIs(result, sentinel)
            environment = checked.call_args.kwargs["environment"]
            self.assertEqual(
                environment["SURGE_SYNC_POINT"],
                "SP_CARRIER_JUMBO_ADMITTED:block",
            )

    def test_fixture_source_requires_exact_marker_window_and_native_timer(self) -> None:
        source = """\
@intrinsic fn rt_array_debug_deferred_base_drops() -> uint64;
@intrinsic fn rt_monotonic_now() -> int64;
fn main() -> nothing {
    rt_array_debug_deferred_base_drops();
    rt_monotonic_now();
    rt_array_debug_deferred_base_drops();
}
"""
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "main.sg"
            path.write_text(source, encoding="utf-8")
            _verify_fixture_source(path, "fixture/main.sg")
            path.write_text(
                source.replace(
                    "    rt_monotonic_now();\n",
                    "    rt_array_debug_deferred_base_drops();\n",
                ),
                encoding="utf-8",
            )
            with self.assertRaisesRegex(GateFailure, "exactly two"):
                _verify_fixture_source(path, "fixture/main.sg")
            path.write_text(source + "type Bad = Duration;\n", encoding="utf-8")
            with self.assertRaisesRegex(GateFailure, "boxed Duration"):
                _verify_fixture_source(path, "fixture/main.sg")

    def test_emitted_ir_freezes_o3_markers_and_observer_graph(self) -> None:
        emitted = """\
declare i64 @rt_array_debug_deferred_base_drops()
declare i64 @rt_monotonic_now()
declare ptr @rt_heap_stats()
define i64 @observer() {
entry:
  %stats = call ptr @rt_heap_stats()
  call void @drop.dynamic(ptr %stats)
  ret i64 0
}
define void @drop.dynamic(ptr %stats) {
entry:
  call void @rt_free(ptr %stats, i64 1, i64 1)
  ret void
}
define i64 @fixture() {
entry:
  %a = call i64 @rt_array_debug_deferred_base_drops()
  %t = call i64 @rt_monotonic_now()
  %b = call i64 @rt_array_debug_deferred_base_drops()
  ret i64 %t
}
define void @__surge_start() {
entry:
  %result = call i64 @fixture()
  ret void
}
"""
        _verify_emitted_ir(emitted, emitted, "fixture/main.sg")
        with self.assertRaisesRegex(GateFailure, "O3 LLVM IR has 1 marker"):
            _verify_emitted_ir(
                emitted,
                emitted.replace(
                    "  %b = call i64 @rt_array_debug_deferred_base_drops()\n", ""
                ),
                "fixture/main.sg",
            )
        contaminated = emitted.replace(
            "  call void @rt_free(ptr %stats, i64 1, i64 1)\n",
            "  call void @rt_carrier_bench_record_copy(i64 1)\n",
        )
        with self.assertRaisesRegex(GateFailure, "observer graph calls forbidden"):
            _verify_emitted_ir(contaminated, emitted, "fixture/main.sg")
        repeated = emitted.replace(
            "  %result = call i64 @fixture()\n",
            "  %result = call i64 @fixture()\n  %again = call i64 @fixture()\n",
        )
        with self.assertRaisesRegex(GateFailure, "exactly once"):
            _verify_emitted_ir(repeated, repeated, "fixture/main.sg")

    def test_real_candidate_zero_fixture_contract(self) -> None:
        root = SCRIPT_DIR.parent
        manifest = make_manifest()
        row = replace(
            manifest.rows[0],
            row_id="zero",
            fixture="testdata/runtime-v2-carrier-bench/scored/local/main.sg",
            probe="zero",
            operations_per_batch=64,
            payload_bytes=0,
            relative_performance=False,
            expected_checksum="0",
        )
        manifest = replace(manifest, rows=(row,))
        protocol_sha256 = "a" * 64
        nonce = "b" * 32
        with tempfile.TemporaryDirectory() as temporary:
            build_root = Path(temporary)
            surge = build_root / "surge"
            build_surge(root, surge)
            fixtures = build_fixtures(
                side_root=root,
                harness_root=root,
                surge=surge,
                manifest=manifest,
                build_root=build_root / "fixtures",
            )
            fixture = fixtures[row.fixture]
            result = run_checked(
                [str(fixture.binary), "zero"],
                cwd=fixture.binary.parent,
                timeout_seconds=30,
                environment={
                    "SURGE_CARRIER_BENCH_COUNTERS": "1",
                    "SURGE_CARRIER_BENCH_PROBE": "zero",
                    "SURGE_CARRIER_BENCH_NONCE": nonce,
                    "SURGE_CARRIER_BENCH_PROTOCOL_SHA256": protocol_sha256,
                    "SURGE_SHARDS": "1",
                    "SURGE_THREADS": "1",
                    "SURGE_BLOCKING_THREADS": "1",
                },
            )
            parsed = _parse_result(result.stdout, row, {"allocation_count"})
            counters = _parse_runtime_counters(
                result.stderr,
                row,
                manifest,
                "candidate",
                expected_nonce=nonce,
                expected_protocol_sha256=protocol_sha256,
            )
        self.assertEqual(parsed.counters, {"allocation_count": 0})
        self.assertEqual(set(counters.values()), {0})
