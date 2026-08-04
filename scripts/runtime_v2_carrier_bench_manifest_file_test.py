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


if __name__ == "__main__":
    unittest.main()
