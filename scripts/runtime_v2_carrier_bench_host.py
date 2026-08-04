"""Host identity and fail-closed subprocess helpers for carrier benchmarks."""

from __future__ import annotations

import ctypes
import os
import platform
import signal
import subprocess
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping, Sequence

from runtime_v2_carrier_bench_model import GateFailure, ReferenceHost

INHERITED_ENVIRONMENT = (
    "GOCACHE",
    "GOMODCACHE",
    "GOPATH",
    "HOME",
    "LOGNAME",
    "TMPDIR",
    "USER",
    "XDG_CACHE_HOME",
)
PR_SET_CHILD_SUBREAPER = 36
_SUBREAPER_ENABLED = False


@dataclass(frozen=True, slots=True)
class CommandResult:
    stdout: str
    stderr: str


def run_checked(
    command: Sequence[str],
    *,
    cwd: Path,
    timeout_seconds: int,
    environment: Mapping[str, str] | None = None,
) -> CommandResult:
    if _native_thread_count() != 1:
        raise GateFailure("carrier benchmark subprocess supervision must be single-threaded")
    _ensure_child_subreaper()
    existing_children = _direct_children(os.getpid())
    if existing_children:
        raise GateFailure(
            "carrier benchmark subprocess supervision requires no pre-existing "
            f"child processes; found={sorted(existing_children)}"
        )
    env = {
        name: os.environ[name]
        for name in INHERITED_ENVIRONMENT
        if name in os.environ
    }
    if environment is not None:
        env.update(environment)
    env.update(
        {
            "GOENV": "off",
            "GOWORK": "off",
            "LANG": "C",
            "LC_ALL": "C",
            "PATH": os.environ.get("PATH", os.defpath),
            "TZ": "UTC",
        }
    )
    process = subprocess.Popen(
        list(command),
        cwd=cwd,
        env=env,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        start_new_session=True,
    )
    process_start_time = _process_start_time(process.pid)
    timeout_error: subprocess.TimeoutExpired | None = None
    drain_error: subprocess.TimeoutExpired | None = None
    lingering_group = False
    stdout_bytes = b""
    stderr_bytes = b""
    try:
        try:
            stdout_bytes, stderr_bytes = process.communicate(timeout=timeout_seconds)
        except subprocess.TimeoutExpired as err:
            timeout_error = err
            lingering_group = _terminate_process_tree(process, process_start_time)
            try:
                stdout_bytes, stderr_bytes = process.communicate(timeout=3)
            except subprocess.TimeoutExpired as cleanup_err:
                drain_error = cleanup_err
                stdout_bytes = _captured_bytes(cleanup_err.output, err.output)
                stderr_bytes = _captured_bytes(cleanup_err.stderr, err.stderr)
                _close_capture_pipes(process)
    finally:
        # A benchmark command is not allowed to daemonize.  The process leader
        # may exit while a redirected descendant keeps its private session
        # alive, so clean the whole group on every completion path, not only on
        # timeout.
        lingering_group = (
            _terminate_process_tree(process, process_start_time) or lingering_group
        )
    stdout, stdout_valid = _decode_output(stdout_bytes)
    stderr, stderr_valid = _decode_output(stderr_bytes)
    if not stdout_valid or not stderr_valid:
        rendered = " ".join(command)
        invalid = ", ".join(
            name
            for name, valid in (("stdout", stdout_valid), ("stderr", stderr_valid))
            if not valid
        )
        raise GateFailure(
            f"command emitted invalid UTF-8 on {invalid}: {rendered}\n"
            f"stdout:\n{stdout}\nstderr:\n{stderr}"
        )
    if timeout_error is not None:
        rendered = " ".join(command)
        detail = (
            " and output pipes remained open after bounded cleanup"
            if drain_error is not None
            else ""
        )
        raise GateFailure(
            f"command timed out after {timeout_seconds}s{detail}: {rendered}\n"
            f"stdout:\n{stdout}\nstderr:\n{stderr}"
        ) from timeout_error
    if lingering_group:
        rendered = " ".join(command)
        raise GateFailure(
            f"command exited with status {process.returncode} but left a live process group: "
            f"{rendered}\nstdout:\n{stdout}\nstderr:\n{stderr}"
        )
    if process.returncode != 0:
        rendered = " ".join(command)
        raise GateFailure(
            f"command failed with status {process.returncode}: {rendered}\n"
            f"stdout:\n{stdout}\nstderr:\n{stderr}"
        )
    return CommandResult(stdout=stdout, stderr=stderr)


def git_commit(root: Path) -> str:
    result = run_checked(
        ["git", "rev-parse", "HEAD"], cwd=root, timeout_seconds=30
    )
    commit = result.stdout.strip()
    if len(commit) != 40:
        raise GateFailure(f"unexpected git commit at {root}: {commit!r}")
    return commit


def require_clean_worktree(root: Path) -> None:
    result = run_checked(
        ["git", "status", "--porcelain=v1", "--untracked-files=all"],
        cwd=root,
        timeout_seconds=30,
    )
    if result.stdout:
        raise GateFailure(f"benchmark worktree is dirty: {root}\n{result.stdout}")


def detect_host() -> ReferenceHost:
    affinity = sorted(os.sched_getaffinity(0))
    return ReferenceHost(
        system=platform.system(),
        machine=platform.machine(),
        kernel_contains=platform.release(),
        cpu_model=_cpu_model(),
        logical_cpus=os.cpu_count() or 0,
        cpuset=_format_cpuset(affinity),
        go_version=_first_line(["go", "version"]),
        clang_version=_first_line(["clang", "--version"]),
    )


def require_reference_host(
    expected: ReferenceHost, actual: ReferenceHost | None = None
) -> ReferenceHost:
    if actual is None:
        actual = detect_host()
    exact_fields = (
        "system",
        "machine",
        "cpu_model",
        "logical_cpus",
        "cpuset",
        "go_version",
        "clang_version",
    )
    differences = [
        f"{field}: actual={getattr(actual, field)!r}, expected={getattr(expected, field)!r}"
        for field in exact_fields
        if getattr(actual, field) != getattr(expected, field)
    ]
    if expected.kernel_contains not in actual.kernel_contains:
        differences.append(
            f"kernel: actual={actual.kernel_contains!r}, "
            f"required-substring={expected.kernel_contains!r}"
        )
    if differences:
        raise GateFailure("reference host mismatch:\n- " + "\n- ".join(differences))
    return actual


def _ensure_child_subreaper() -> None:
    global _SUBREAPER_ENABLED
    if _SUBREAPER_ENABLED:
        return
    if platform.system() != "Linux":
        raise GateFailure("carrier benchmark process supervision requires Linux")
    libc = ctypes.CDLL(None, use_errno=True)
    result = libc.prctl(PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
    if result != 0:
        error_number = ctypes.get_errno()
        raise GateFailure(
            f"cannot enable child-subreaper supervision: {os.strerror(error_number)}"
        )
    _SUBREAPER_ENABLED = True


def _terminate_process_tree(
    process: subprocess.Popen[bytes], process_start_time: int
) -> bool:
    touched = _signal_process_group(
        process.pid, process_start_time, signal.SIGTERM
    )
    deadline = time.monotonic() + 2.0
    while time.monotonic() < deadline:
        process.poll()
        owned = _owned_processes(process.pid, process_start_time)
        if owned:
            touched = True
            _signal_processes(owned, signal.SIGTERM)
        _reap_adopted(owned, process.pid)
        if not owned and not _process_group_exists(
            process.pid, process_start_time
        ):
            return touched
        time.sleep(0.02)
    _signal_process_group(process.pid, process_start_time, signal.SIGKILL)
    owned = _owned_processes(process.pid, process_start_time)
    _signal_processes(owned, signal.SIGKILL)
    deadline = time.monotonic() + 2.0
    while time.monotonic() < deadline:
        process.poll()
        owned = _owned_processes(process.pid, process_start_time)
        _signal_processes(owned, signal.SIGKILL)
        _reap_adopted(owned, process.pid)
        if not owned and not _process_group_exists(
            process.pid, process_start_time
        ):
            return True
        time.sleep(0.02)
    raise GateFailure(
        "timed-out process tree survived SIGKILL: "
        f"pids={sorted(_owned_processes(process.pid, process_start_time))}"
    )


def _owned_processes(root_pid: int, process_start_time: int) -> set[int]:
    roots = _direct_children(os.getpid())
    if _process_identity_matches(root_pid, process_start_time):
        roots.add(root_pid)
    return roots


def _native_thread_count() -> int:
    task_root = Path(f"/proc/{os.getpid()}/task")
    try:
        return sum(1 for _ in task_root.iterdir())
    except OSError as err:
        raise GateFailure(f"cannot inspect native threads via {task_root}: {err}") from err


def _direct_children(pid: int) -> set[int]:
    path = Path(f"/proc/{pid}/task/{pid}/children")
    try:
        raw = path.read_text(encoding="ascii").strip()
    except FileNotFoundError:
        return set()
    except OSError as err:
        raise GateFailure(f"cannot inspect child processes via {path}: {err}") from err
    if not raw:
        return set()
    try:
        return {int(value) for value in raw.split()}
    except ValueError as err:
        raise GateFailure(f"malformed child process list in {path}: {raw!r}") from err


def _process_start_time(pid: int) -> int:
    start_time = _read_process_start_time(pid)
    if start_time is None:
        raise GateFailure(f"process {pid} disappeared before supervision started")
    return start_time


def _read_process_start_time(pid: int) -> int | None:
    path = Path(f"/proc/{pid}/stat")
    try:
        value = path.read_text(encoding="ascii")
    except FileNotFoundError:
        return None
    except OSError as err:
        raise GateFailure(f"cannot read process identity from {path}: {err}") from err
    closing_paren = value.rfind(")")
    fields = value[closing_paren + 2 :].split() if closing_paren >= 0 else []
    if len(fields) <= 19:
        raise GateFailure(f"malformed process identity in {path}")
    try:
        return int(fields[19])
    except ValueError as err:
        raise GateFailure(f"malformed process start time in {path}") from err


def _process_identity_matches(pid: int, start_time: int) -> bool:
    return _read_process_start_time(pid) == start_time


def _signal_process_group(
    process_group: int, process_start_time: int, sig: signal.Signals
) -> bool:
    if not _process_identity_matches(process_group, process_start_time):
        return False
    try:
        os.killpg(process_group, sig)
        return True
    except ProcessLookupError:
        return False


def _process_group_exists(process_group: int, process_start_time: int) -> bool:
    if not _process_identity_matches(process_group, process_start_time):
        return False
    try:
        os.killpg(process_group, 0)
        return True
    except ProcessLookupError:
        return False


def _signal_processes(pids: set[int], sig: signal.Signals) -> None:
    for pid in pids:
        try:
            os.kill(pid, sig)
        except ProcessLookupError:
            continue


def _reap_adopted(pids: set[int], root_pid: int) -> None:
    for pid in pids - {root_pid}:
        try:
            os.waitpid(pid, os.WNOHANG)
        except ChildProcessError:
            continue


def _captured_bytes(primary: bytes | None, fallback: bytes | None) -> bytes:
    return primary if primary is not None else fallback or b""


def _close_capture_pipes(process: subprocess.Popen[bytes]) -> None:
    if process.stdout is not None:
        process.stdout.close()
    if process.stderr is not None:
        process.stderr.close()


def _decode_output(value: bytes) -> tuple[str, bool]:
    try:
        return value.decode("utf-8"), True
    except UnicodeDecodeError:
        return value.decode("utf-8", errors="backslashreplace"), False


def _cpu_model() -> str:
    try:
        lines = Path("/proc/cpuinfo").read_text(encoding="utf-8").splitlines()
    except OSError as err:
        raise GateFailure(f"cannot read CPU model: {err}") from err
    for line in lines:
        if line.startswith("model name"):
            return line.split(":", 1)[1].strip()
    raise GateFailure("cannot find CPU model in /proc/cpuinfo")


def _format_cpuset(cpus: Sequence[int]) -> str:
    if not cpus:
        raise GateFailure("process CPU affinity is empty")
    ranges: list[str] = []
    start = previous = cpus[0]
    for cpu in cpus[1:]:
        if cpu == previous + 1:
            previous = cpu
            continue
        ranges.append(str(start) if start == previous else f"{start}-{previous}")
        start = previous = cpu
    ranges.append(str(start) if start == previous else f"{start}-{previous}")
    return ",".join(ranges)


def _first_line(command: Sequence[str]) -> str:
    result = run_checked(command, cwd=Path.cwd(), timeout_seconds=30)
    lines = result.stdout.splitlines()
    if not lines:
        raise GateFailure(f"command produced no identity line: {' '.join(command)}")
    return lines[0].strip()
