#!/usr/bin/env python3
"""Run the frozen base/candidate Runtime V2 carrier benchmark protocol."""

from __future__ import annotations

import argparse
import ast
import json
import sys
import tempfile
import uuid
from contextlib import contextmanager
from datetime import datetime, timezone
from pathlib import Path
from typing import Iterator, Sequence

sys.dont_write_bytecode = True

SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

from runtime_v2_carrier_bench_host import (  # noqa: E402
    detect_host,
    git_commit,
    require_clean_worktree,
    require_reference_host,
    run_checked,
)
from runtime_v2_carrier_bench_manifest import (  # noqa: E402
    load_manifest,
    verify_file_digests,
)
from runtime_v2_carrier_bench_model import (  # noqa: E402
    FileDigest,
    GateFailure,
    Manifest,
    ManifestError,
    Side,
)
from runtime_v2_carrier_bench_report import (  # noqa: E402
    manifest_digest,
    render_aborted_report,
    render_report,
    write_report,
)
from runtime_v2_carrier_bench_runner import (  # noqa: E402
    BatchResult,
    BuiltFixture,
    LivenessRecord,
    RunRecord,
    build_fixtures,
    build_liveness_fixtures,
    build_surge,
    execute_manifest,
    execute_liveness_probes,
)


def main() -> int:
    args = _arguments()
    benchmark_phase = getattr(args, "phase", "wave-a")
    capture_wave_a_baseline = getattr(args, "capture_wave_a_baseline", False)
    started_at = _utc_now()
    attempt_id = f"{started_at.replace(':', '').replace('-', '')}-{uuid.uuid4().hex[:8]}"
    candidate_root = args.candidate_root.resolve()
    manifest_path = args.manifest.resolve()
    report_path = (
        args.report.resolve()
        if args.report is not None
        else candidate_root
        / "build"
        / "benchmarks"
        / "runtime-v2-carrier"
        / f"{attempt_id}.json"
    )
    phase = "manifest_identity"
    manifest: Manifest | None = None
    manifest_sha256: str | None = None
    actual_host = None
    base_commit: str | None = None
    candidate_commit: str | None = None
    events: list[dict[str, object]] = []
    try:
        if capture_wave_a_baseline and benchmark_phase != "wave-a":
            raise ManifestError(
                "--capture-wave-a-baseline is valid only with --phase=wave-a"
            )
        _require_canonical_manifest(candidate_root, manifest_path)
        phase = "manifest_load"
        manifest_sha256 = manifest_digest(manifest_path)
        manifest = load_manifest(manifest_path)
        phase = "host_identity"
        actual_host = detect_host()
        require_reference_host(manifest.reference, actual_host)
        phase = "candidate_identity"
        require_clean_worktree(candidate_root)
        candidate_commit = git_commit(candidate_root)
        _require_descendant(candidate_root, manifest.epic_base, candidate_commit)
        phase = "frozen_input_verification"
        _require_tracked_entries(candidate_root, manifest.harness_files, "harness file")
        _require_tracked_entries(candidate_root, manifest.fixtures, "fixture")
        verify_file_digests(candidate_root, manifest.harness_files, "harness file")
        verify_file_digests(candidate_root, manifest.fixtures, "fixture")
        _verify_harness_inventory(candidate_root, manifest)
        _verify_fixture_inventory(candidate_root, manifest)
        phase = "base_identity"
        with _commit_root(candidate_root, manifest.epic_base, "base") as base_root:
            require_clean_worktree(base_root)
            base_commit = git_commit(base_root)
            if base_commit != manifest.epic_base:
                raise GateFailure(
                    f"base worktree is {base_commit}, manifest requires {manifest.epic_base}"
                )
            with _commit_root(
                candidate_root, candidate_commit, "candidate"
            ) as candidate_build_root:
                require_clean_worktree(candidate_build_root)
                if git_commit(candidate_build_root) != candidate_commit:
                    raise GateFailure("candidate detached worktree commit drifted")
                detached_manifest = (
                    candidate_build_root
                    / "testdata"
                    / "runtime-v2-carrier-bench.json"
                )
                if manifest_digest(detached_manifest) != manifest_sha256:
                    raise GateFailure(
                        "live manifest bytes differ from the candidate commit"
                    )
                verify_file_digests(
                    candidate_build_root, manifest.harness_files, "harness file"
                )
                verify_file_digests(
                    candidate_build_root, manifest.fixtures, "fixture"
                )
                _verify_harness_inventory(candidate_build_root, manifest)
                _verify_fixture_inventory(candidate_build_root, manifest)
                phase = "build_and_measure"
                with tempfile.TemporaryDirectory(
                    prefix="surge-carrier-bench-"
                ) as temporary:
                    records, liveness_records, allocation_controls = _build_and_run(
                        manifest=manifest,
                        harness_root=candidate_build_root,
                        base_root=base_root,
                        candidate_root=candidate_build_root,
                        temporary=Path(temporary),
                        events=events,
                        protocol_sha256=manifest_sha256,
                        benchmark_phase=benchmark_phase,
                    )
        phase = "scoring"
        report, failure = render_report(
            attempt_id=attempt_id,
            started_at=started_at,
            ended_at=_utc_now(),
            manifest=manifest,
            manifest_sha256=manifest_sha256,
            base_commit=base_commit,
            candidate_commit=candidate_commit,
            actual_host=actual_host,
            records=records,
            benchmark_phase=benchmark_phase,
            liveness_records=liveness_records,
            allocation_controls=allocation_controls,
            events=events,
        )
        phase = "report_write"
        write_report(report_path, report)
        print(f"carrier benchmark report: {report_path}")
        if failure is not None:
            print(f"runtime-v2 carrier benchmark failed: {failure}", file=sys.stderr)
            if _baseline_capture_accepts(
                report,
                benchmark_phase=benchmark_phase,
                requested=capture_wave_a_baseline,
            ):
                print("captured complete Wave-A RED baseline; report remains failed")
                return 0
            return 1
        return 0
    except (GateFailure, ManifestError, OSError, json.JSONDecodeError) as err:
        try:
            write_report(
                report_path,
                render_aborted_report(
                    attempt_id=attempt_id,
                    started_at=started_at,
                    ended_at=_utc_now(),
                    phase=phase,
                    failure=err,
                    candidate_root=candidate_root,
                    manifest_path=manifest_path,
                    manifest_sha256=manifest_sha256,
                    manifest=manifest,
                    actual_host=actual_host,
                    base_commit=base_commit,
                    candidate_commit=candidate_commit,
                    events=events,
                ),
            )
            print(f"carrier benchmark aborted report: {report_path}", file=sys.stderr)
        except OSError as report_err:
            print(
                f"runtime-v2 carrier benchmark could not write aborted report: {report_err}",
                file=sys.stderr,
            )
        print(f"runtime-v2 carrier benchmark failed: {err}", file=sys.stderr)
        return 1


def _build_and_run(
    *,
    manifest: Manifest,
    harness_root: Path,
    base_root: Path,
    candidate_root: Path,
    temporary: Path,
    events: list[dict[str, object]],
    protocol_sha256: str,
    benchmark_phase: str,
) -> tuple[
    dict[str, dict[Side, tuple[RunRecord, ...]]],
    tuple[LivenessRecord, ...],
    dict[Side, BatchResult],
]:
    base_surge = temporary / "base" / "surge"
    candidate_surge = temporary / "candidate" / "surge"
    build_surge(base_root, base_surge)
    build_surge(candidate_root, candidate_surge)
    timing_binaries: dict[Side, dict[str, BuiltFixture]] = {
        "base": build_fixtures(
            side_root=base_root,
            harness_root=harness_root,
            surge=base_surge,
            manifest=manifest,
            build_root=temporary / "base" / "fixtures",
            capture_kind="timing",
            include_allocation_control=True,
        ),
        "candidate": build_fixtures(
            side_root=candidate_root,
            harness_root=harness_root,
            surge=candidate_surge,
            manifest=manifest,
            build_root=temporary / "candidate" / "fixtures",
            capture_kind="timing",
            include_allocation_control=True,
        ),
    }
    resource_binaries = build_fixtures(
        side_root=candidate_root,
        harness_root=harness_root,
        surge=candidate_surge,
        manifest=manifest,
        build_root=temporary / "candidate" / "resource-fixtures",
        capture_kind="resource",
    )
    records, allocation_controls = execute_manifest(
        manifest,
        timing_binaries,
        resource_binaries,
        events,
        protocol_sha256,
    )
    liveness_binaries = (
        build_liveness_fixtures(
            side_root=candidate_root,
            harness_root=harness_root,
            surge=candidate_surge,
            manifest=manifest,
            build_root=temporary / "candidate" / "liveness",
        )
        if benchmark_phase == "final"
        else {}
    )
    liveness = execute_liveness_probes(
        manifest,
        liveness_binaries,
        events,
        protocol_sha256,
        benchmark_phase,
    )
    return records, liveness, allocation_controls


@contextmanager
def _commit_root(repository: Path, commit: str, label: str) -> Iterator[Path]:
    with tempfile.TemporaryDirectory(
        prefix=f"surge-carrier-{label}-"
    ) as temporary:
        root = Path(temporary) / "worktree"
        run_checked(
            [
                "git",
                "-c",
                "core.hooksPath=/dev/null",
                "worktree",
                "add",
                "--detach",
                str(root),
                commit,
            ],
            cwd=repository,
            timeout_seconds=120,
        )
        try:
            yield root
        finally:
            run_checked(
                [
                    "git",
                    "-c",
                    "core.hooksPath=/dev/null",
                    "worktree",
                    "remove",
                    "--force",
                    str(root),
                ],
                cwd=repository,
                timeout_seconds=120,
            )


def _require_descendant(root: Path, base: str, candidate: str) -> None:
    run_checked(
        ["git", "merge-base", "--is-ancestor", base, candidate],
        cwd=root,
        timeout_seconds=30,
    )


def _require_canonical_manifest(root: Path, manifest_path: Path) -> None:
    canonical = root / "testdata" / "runtime-v2-carrier-bench.json"
    if manifest_path != canonical or manifest_path.is_symlink():
        raise ManifestError(
            f"benchmark manifest must be the tracked canonical path {canonical}"
        )
    run_checked(
        [
            "git",
            "ls-files",
            "--error-unmatch",
            "--",
            canonical.relative_to(root).as_posix(),
        ],
        cwd=root,
        timeout_seconds=30,
    )


def _require_tracked_entries(
    root: Path, entries: Sequence[FileDigest], label: str
) -> None:
    for entry in entries:
        try:
            run_checked(
                ["git", "ls-files", "--error-unmatch", "--", entry.path],
                cwd=root,
                timeout_seconds=30,
            )
        except GateFailure as err:
            raise ManifestError(f"{label} is not tracked: {entry.path}") from err


def _verify_harness_inventory(root: Path, manifest: Manifest) -> None:
    expected = {entry.path for entry in manifest.harness_files}
    namespace = {
        path.relative_to(root).as_posix()
        for path in (root / "scripts").glob("runtime_v2_carrier_bench*.py")
        if path.is_file() or path.is_symlink()
    }
    actual = set(namespace)
    pending = list(namespace)
    while pending:
        relative = pending.pop()
        for dependency in _repository_python_imports(root, relative):
            if dependency not in actual:
                actual.add(dependency)
                pending.append(dependency)
    if actual != expected:
        raise ManifestError(
            "harness inventory mismatch: "
            f"missing={sorted(expected - actual)} extra={sorted(actual - expected)}"
        )


def _repository_python_imports(root: Path, relative: str) -> set[str]:
    path = root / relative
    try:
        tree = ast.parse(path.read_text(encoding="utf-8"), filename=relative)
    except (OSError, SyntaxError, UnicodeError) as err:
        raise ManifestError(f"cannot inspect harness imports in {relative}: {err}") from err
    modules: list[tuple[str, int]] = []
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            modules.extend((alias.name, 0) for alias in node.names)
        elif isinstance(node, ast.ImportFrom):
            if node.module is not None:
                modules.append((node.module, node.level))
            elif node.level:
                modules.extend((alias.name, node.level) for alias in node.names)
    dependencies: set[str] = set()
    for module, level in modules:
        module_path = Path(*module.split("."))
        if level:
            base = path.parent
            for _ in range(level - 1):
                base = base.parent
            candidates = (
                base / module_path.with_suffix(".py"),
                base / module_path / "__init__.py",
            )
        else:
            candidates = (
                root / "scripts" / module_path.with_suffix(".py"),
                root / "scripts" / module_path / "__init__.py",
                root / module_path.with_suffix(".py"),
                root / module_path / "__init__.py",
            )
        matches = [candidate for candidate in candidates if candidate.is_file()]
        if len(matches) > 1:
            raise ManifestError(
                f"ambiguous repository-local harness import {module!r} in {relative}"
            )
        if matches:
            dependencies.add(matches[0].relative_to(root).as_posix())
    return dependencies


def _verify_fixture_inventory(root: Path, manifest: Manifest) -> None:
    entries = manifest.fixtures
    expected = {entry.path for entry in entries}
    parents = {Path(entry.path).parent for entry in entries}
    actual: set[str] = set()
    for parent in parents:
        for path in (root / parent).rglob("*"):
            if path.is_file() or path.is_symlink():
                actual.add(path.relative_to(root).as_posix())
    if actual != expected:
        raise ManifestError(
            "fixture inventory mismatch: "
            f"missing={sorted(expected - actual)} extra={sorted(actual - expected)}"
        )


def _arguments() -> argparse.Namespace:
    repository = SCRIPT_DIR.parent
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--manifest",
        type=Path,
        default=repository / "testdata" / "runtime-v2-carrier-bench.json",
    )
    parser.add_argument(
        "--report",
        type=Path,
    )
    parser.add_argument("--candidate-root", type=Path, default=repository)
    parser.add_argument(
        "--phase", choices=("wave-a", "final"), default="wave-a"
    )
    parser.add_argument(
        "--capture-wave-a-baseline",
        action="store_true",
        help=(
            "return success only for a complete Wave-A report whose protocol passed "
            "and whose endpoint invariants remain RED"
        ),
    )
    return parser.parse_args()


def _baseline_capture_accepts(
    report: dict[str, object], *, benchmark_phase: str, requested: bool
) -> bool:
    return (
        requested
        and benchmark_phase == "wave-a"
        and report.get("status") == "failed"
        and report.get("protocol_status") == "passed"
        and report.get("endpoint_invariant_status") == "failed"
    )


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="microseconds").replace(
        "+00:00", "Z"
    )


if __name__ == "__main__":
    raise SystemExit(main())
