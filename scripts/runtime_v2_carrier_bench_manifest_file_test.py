"""Checks for the canonical Runtime V2 carrier benchmark manifest."""

from __future__ import annotations

import subprocess
import sys
import unittest
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

from runtime_v2_carrier_bench import (
    _verify_fixture_inventory,
    _verify_harness_inventory,
)
from runtime_v2_carrier_bench_manifest import load_manifest, verify_file_digests


class CanonicalManifestTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.root = SCRIPT_DIR.parent
        cls.manifest = load_manifest(
            cls.root / "testdata" / "runtime-v2-carrier-bench.json"
        )

    def test_file_inventories_and_hashes_are_exact(self) -> None:
        verify_file_digests(self.root, self.manifest.harness_files, "harness file")
        verify_file_digests(self.root, self.manifest.fixtures, "fixture")
        _verify_harness_inventory(self.root, self.manifest)
        _verify_fixture_inventory(self.root, self.manifest)

    def test_matrix_and_blocking_topology_are_frozen(self) -> None:
        rows = {row.row_id: row for row in self.manifest.rows}
        self.assertEqual(len(rows), 46)
        self.assertEqual(self.manifest.blocking_threads, 1)
        self.assertEqual(rows["select-send-scalar"].expected_checksum, "11432")
        self.assertEqual(rows["select-send-composite"].expected_checksum, "15947")
        self.assertEqual(rows["far-large-capture"].payload_bytes, 4096)
        self.assertEqual(rows["far-jumbo-contention"].payload_bytes, 8192)
        self.assertEqual(
            {probe.probe_id for probe in self.manifest.liveness_probes},
            {"jumbo-credit-cancel", "jumbo-global-shutdown"},
        )

        nonzero_allocations = {
            row.row_id: row.candidate_structural_allocations_per_batch
            for row in self.manifest.rows
            if row.candidate_structural_allocations_per_batch != 0
        }
        self.assertEqual(
            nonzero_allocations,
            {
                "array-grow-composite": 7,
                "array-grow-scalar": 7,
                "blocking-composite": 278,
                "blocking-scalar": 278,
                "channel-buffered-composite": 15,
                "channel-buffered-scalar": 15,
                "channel-unbuffered-composite": 130,
                "channel-unbuffered-scalar": 130,
                "far-channel-composite": 544,
                "far-channel-scalar": 544,
                "far-immediate-composite": 281,
                "far-immediate-scalar": 281,
                "far-jumbo-contention": 1042,
                "far-large-capture": 1049,
                "far-large-result": 537,
                "far-select-composite": 615,
                "far-select-scalar": 615,
                "far-share-control": 351,
                "far-task-composite": 537,
                "far-task-scalar": 537,
                "map-insert-composite": 4,
                "map-insert-scalar": 4,
                "map-rehash-composite": 4,
                "map-rehash-scalar": 4,
                "select-send-composite": 7,
                "select-send-scalar": 7,
                "task-clone-composite": 278,
                "task-clone-scalar": 278,
                "task-composite": 278,
                "task-scalar": 278,
            },
        )
        self.assertFalse(
            any(
                invariant.metric == "allocation_count"
                for row in self.manifest.rows
                for invariant in row.invariants
            )
        )
        self.assertFalse(
            any(
                invariant.metric == "allocation_count"
                for invariant in self.manifest.cross_row_invariants
            )
        )

    def test_required_red_endpoints_are_machine_checked(self) -> None:
        rows = {row.row_id: row for row in self.manifest.rows}
        exact = {
            (item.metric, item.operator, item.value, item.side)
            for item in rows["far-jumbo-contention"].invariants
        }
        self.assertIn(("bytes_moved", "eq", 2097152, "candidate"), exact)
        self.assertIn(("callback_count", "eq", 256, "candidate"), exact)
        self.assertIn(("credit_stalls", "eq", 128, "candidate"), exact)
        proportional = {
            (item.left_row, item.right_row, item.metric)
            for item in self.manifest.cross_row_invariants
            if item.relation == "payload_proportional"
        }
        self.assertEqual(
            proportional,
            {("far-large-capture", "far-jumbo-contention", "bytes_moved")},
        )

    def test_canonical_make_gate_selects_final_phase(self) -> None:
        direct = subprocess.run(
            ["make", "-n", "runtime-v2-carrier-bench"],
            cwd=self.root,
            check=True,
            capture_output=True,
            text=True,
        ).stdout
        alias = subprocess.run(
            ["make", "-n", "runtime-v2-carrier-bench-final"],
            cwd=self.root,
            check=True,
            capture_output=True,
            text=True,
        ).stdout
        for output in (direct, alias):
            self.assertIn("--phase=final", output)
            self.assertNotIn("--phase=wave-a", output)
            self.assertIn("PYTHONDONTWRITEBYTECODE=1", output)

        baseline = subprocess.run(
            ["make", "-n", "runtime-v2-carrier-baseline-capture"],
            cwd=self.root,
            check=True,
            capture_output=True,
            text=True,
        ).stdout
        self.assertIn("--phase=wave-a --capture-wave-a-baseline", baseline)
        self.assertIn("PYTHONDONTWRITEBYTECODE=1", baseline)
        self.assertIn(
            "endpoint RED baseline (protocol failures remain fatal)", baseline
        )

    def test_carrier_check_selects_the_live_carrier_census_gate(self) -> None:
        output = subprocess.run(
            ["make", "-n", "runtime-v2-carrier-check"],
            cwd=self.root,
            check=True,
            capture_output=True,
            text=True,
        ).stdout
        self.assertIn("go test ./internal/carriergate -count=1", output)
        self.assertIn("TestRuntime(TestSyncPoint|CarrierBench)BuildFlag", output)


if __name__ == "__main__":
    unittest.main()
