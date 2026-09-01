"""Focused Runtime V2 carrier benchmark tests."""

from runtime_v2_carrier_bench_test_support import *

class HostTests(unittest.TestCase):
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
