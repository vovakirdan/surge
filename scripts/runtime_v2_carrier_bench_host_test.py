"""Focused Runtime V2 carrier benchmark tests."""

from runtime_v2_carrier_bench_test_support import *
from runtime_v2_carrier_bench import _require_descendant
import runtime_v2_carrier_bench_host as host_module
from unittest import mock

class HostTests(unittest.TestCase):
    def test_host_cpuset_is_the_fixtures_cores_only_when_the_harness_sits_elsewhere(self) -> None:
        # 2026-09-04: with the harness on the fixtures' two cores its wait is a
        # fifth runnable thread against the fixture's four, and the batch reads
        # 25-35 % slower; the candidate lost more to that neighbour than the
        # base and a pairing read a regression a bare run could not reproduce.
        # So the run's cpuset is the fixtures' exactly when the harness's own
        # affinity is disjoint from it and the cores can be taken; any overlap
        # reports the harness's own affinity, which the reference check refuses.
        with mock.patch.object(host_module.os, "sched_getaffinity", return_value={0, 1, 2}), \
                mock.patch.object(host_module, "_fixture_cores_usable", return_value=True):
            self.assertEqual(host_module.host_cpuset("8,10"), "8,10")
        with mock.patch.object(host_module.os, "sched_getaffinity", return_value={8, 9}), \
                mock.patch.object(host_module, "_fixture_cores_usable", return_value=True):
            self.assertEqual(host_module.host_cpuset("8,10"), "8-9")
        with mock.patch.object(host_module.os, "sched_getaffinity", return_value={0, 1}), \
                mock.patch.object(host_module, "_fixture_cores_usable", return_value=False):
            self.assertEqual(host_module.host_cpuset("8,10"), "0-1")
        with mock.patch.object(host_module.os, "sched_getaffinity", return_value={8, 10}):
            self.assertEqual(host_module.host_cpuset(None), "8,10")

    def test_candidate_must_descend_from_and_differ_from_the_base(self) -> None:
        # RV2 Wave F, F4: `git merge-base --is-ancestor X X` answers yes, so a
        # benchmark of the epic base against itself passed every relative gate
        # by construction; the guard refuses the equality by name, refuses a
        # base that is not an ancestor during the run, and admits a descendant.
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
            (root / "one").write_text("one\n", encoding="utf-8")
            run_checked(["git", "add", "."], cwd=root, timeout_seconds=5)
            run_checked(["git", "commit", "-q", "-m", "base"], cwd=root, timeout_seconds=5)
            base = run_checked(
                ["git", "rev-parse", "HEAD"], cwd=root, timeout_seconds=5
            ).stdout.strip()
            (root / "two").write_text("two\n", encoding="utf-8")
            run_checked(["git", "add", "."], cwd=root, timeout_seconds=5)
            run_checked(
                ["git", "commit", "-q", "-m", "candidate"], cwd=root, timeout_seconds=5
            )
            candidate = run_checked(
                ["git", "rev-parse", "HEAD"], cwd=root, timeout_seconds=5
            ).stdout.strip()

            with self.assertRaises(GateFailure) as same:
                _require_descendant(root, base, base)
            self.assertIn("measures nothing", str(same.exception))
            with self.assertRaises(GateFailure) as reversed_roles:
                _require_descendant(root, candidate, base)
            self.assertIn("not an ancestor", str(reversed_roles.exception))
            _require_descendant(root, base, candidate)

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

    def test_success_parent_with_adopted_zombie_is_not_lingering(self) -> None:
        parent = """
import os
import time
from pathlib import Path

child_pid = os.fork()
if child_pid == 0:
    os._exit(0)
deadline = time.monotonic() + 1.0
while time.monotonic() < deadline:
    stat = Path(f"/proc/{child_pid}/stat").read_text(encoding="ascii")
    if stat[stat.rfind(")") + 2] == "Z":
        break
    time.sleep(0.001)
else:
    raise SystemExit("child did not become a zombie")
"""
        with tempfile.TemporaryDirectory() as temporary:
            result = run_checked(
                [sys.executable, "-c", parent],
                cwd=Path(temporary),
                timeout_seconds=5,
            )
        self.assertEqual(result, CommandResult(stdout="", stderr=""))

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
