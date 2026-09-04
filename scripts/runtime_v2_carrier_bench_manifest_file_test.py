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

    def test_liveness_probes_are_deferred_on_the_point_nothing_arms(self) -> None:
        # THIS ROW EXISTS BECAUSE THE MANIFEST AND ITS LOADER CAME APART. The
        # commit that renamed the sync point, dropped the probes' byte figures
        # and deferred their final phase changed only the JSON; the loader went
        # on demanding the three byte keys and the validator went on demanding
        # the old point and a required final phase. The result was not a red
        # assertion anywhere -- load_manifest raised in setUpClass, the suite
        # ran ZERO tests, and the gate that owns this file reported nothing to
        # fail. A suite that cannot start reads exactly like a suite that
        # passed.
        #
        # So this asserts the SHAPE the manifest actually has, from the loaded
        # object, and would have failed the moment the two came apart.
        probes = {probe.probe_id: probe for probe in self.manifest.liveness_probes}
        self.assertEqual(set(probes), {"large-payload-park-cancel", "large-payload-park-shutdown"})
        for probe_id, probe in sorted(probes.items()):
            with self.subTest(probe=probe_id):
                self.assertEqual(probe.syncpoint, "SP_TRANSPORT_DATA_SLOT_TASK_PARKED")
                self.assertEqual(probe.wave_a.status, "deferred")
                self.assertEqual(probe.final.status, "deferred")
                for availability in (probe.wave_a, probe.final):
                    self.assertTrue(availability.reason)
                    self.assertEqual(
                        availability.provenance_commit, self.manifest.epic_base
                    )
                # A byte figure on a probe is what the transport ruling
                # retired: the field is gone from the model, so asking for it
                # is an AttributeError rather than a stale number.
                for gone in (
                    "payload_bytes",
                    "min_peak_transport_bytes",
                    "max_peak_transport_bytes",
                ):
                    self.assertFalse(hasattr(probe, gone), gone)

    def test_file_inventories_and_hashes_are_exact(self) -> None:
        verify_file_digests(self.root, self.manifest.harness_files, "harness file")
        verify_file_digests(self.root, self.manifest.fixtures, "fixture")
        _verify_harness_inventory(self.root, self.manifest)
        _verify_fixture_inventory(self.root, self.manifest)

    def test_matrix_and_blocking_topology_are_frozen(self) -> None:
        rows = {row.row_id: row for row in self.manifest.rows}
        self.assertEqual(len(rows), 46)
        self.assertEqual(self.manifest.blocking_threads, 1)
        self.assertEqual(rows["select-send-scalar"].expected_checksum, "313832")
        self.assertEqual(rows["select-send-composite"].expected_checksum, "349707")
        self.assertEqual(rows["far-large-capture"].payload_bytes, 4096)
        self.assertEqual(rows["far-large-payload-contention"].payload_bytes, 8192)
        self.assertEqual(
            {probe.probe_id for probe in self.manifest.liveness_probes},
            {"large-payload-park-cancel", "large-payload-park-shutdown"},
        )

        # A SECOND, DELIBERATE COPY of every nonzero structural allocation
        # budget in the manifest. It is duplicated on purpose: the manifest is
        # data the benchmark reads, so a budget edited there alone would move
        # the contract silently. Changing a budget means editing this literal
        # too, which is what puts a human in front of the change.
        #
        # Because it is a copy, it can go stale on its own, and it has: these
        # numbers must be re-stated here whenever the manifest is re-captured.
        #
        # THE TWO channel-unbuffered ROWS ARE NO LONGER HELD BACK, and what
        # changed is the measurement rather than anyone's patience. They were
        # kept at 130 on the reading that they answer a different count between
        # identical runs, by one and with the sign flipping. Re-measured over
        # every sample the protocol takes -- two warmups and seven pairs, two
        # batches each, eighteen figures per row -- both answer 4, eighteen
        # times out of eighteen. The wobble was the runtime TOPOLOGY going
        # unpinned, not the row: an unpinned binary reads 343 then 350 where a
        # binary given the manifest's own shards, threads and blocking threads
        # reads one number and repeats it.
        #
        # RE-CAPTURED 2026-09-03 (RV2-DEBT-329). The first paired run since the
        # budgets were pinned on 2026-08-30 refused the tree at blocking-composite,
        # 344 against 341, and a one-run sweep of every budgeted row read the
        # same shape everywhere: +2 on every row that starts the executor, +3 on
        # every row whose task also joins a scope, 0 on the rows that never
        # spawn. The three are one-time allocations of the executor's lazy
        # initialisation, which lands inside warmup batch 0: the two
        # worker_wake_pending arrays (one per shard scheduler, D4.6, 1ba55ecd)
        # and the first growth of the scope's children array (a connection
        # child is counted by its scope since c9c083e2). Structure, not per-op
        # cost; named by an rt_alloc site census under gdb on 8b12beb3,
        # 71bd0674 and HEAD, so the twenty rows below move by exactly that.
        #
        # RE-CAPTURED AGAIN 2026-09-03, later (RV2-DEBT-330): +2 on the rows
        # that start the executor, +3 on the buffered channel rows, -1 on the
        # rendezvous and select-send rows. The first full run after the re-pin above refused at
        # channel-unbuffered-composite, 3 against 4, on one sample in eighteen:
        # thirty single runs read 29 x 4 and 1 x 3, with IDENTICAL rt_alloc
        # site histograms. The owner ruled the budget stays exact (variant
        # (а)). A barrier before the read (rt_debug_quiesce) alone left the
        # twin at 189/11 of 200, and a census of the allocation SEQUENCE
        # against the reads named the mover: deque_reserve, a ready deque's
        # first growth from capacity 0, placed by the schedule. The deques are
        # reserved with their scheduler now (deque_prepare, sixteen slots), so
        # the lazy growths that used to be counted wherever the schedule put
        # them are gone from every batch, and both unbuffered twins read 3 in
        # 200 of 200 single runs with the observer exactly as shipped -- the
        # barrier is not in this fixture, because the base compiler that also
        # builds it does not know the intrinsic. The numbers below are the
        # no-barrier sweep on the reserved-deque tree.
        #
        # RE-CAPTURED 2026-09-04 (owner ruling, variant "в"): every batch is
        # 512 operations instead of 64, so a batch is hundreds of microseconds
        # and its throughput is a measurement rather than a reading of the
        # timer. The per-operation rows move by eight times their per-op cost
        # on top of the fixed executor start (blocking 346 -> 2589, far-channel
        # 414 -> 3102, task 217 -> 1564); the array rows gain the extra grow
        # steps of a 512-element array (7 -> 10, and the steady/teardown rows
        # 0 -> 3 because a 512-element setup grows past the inline capacity);
        # the buffered/unbuffered channel and select-send rows do not move,
        # because their cost is per batch, not per operation. Forty-six rows,
        # three batches each on the candidate tree, one number per row all
        # three times (pin512 pass, 2026-09-04).
        nonzero_allocations = {
            row.row_id: row.candidate_structural_allocations_per_batch
            for row in self.manifest.rows
            if row.candidate_structural_allocations_per_batch != 0
        }
        self.assertEqual(
            nonzero_allocations,
            {
                "array-grow-composite": 10,
                "array-grow-scalar": 10,
                "array-steady-composite": 3,
                "array-steady-scalar": 3,
                "blocking-composite": 2589,
                "blocking-scalar": 1565,
                "channel-buffered-composite": 19,
                "channel-buffered-scalar": 19,
                "channel-unbuffered-composite": 3,
                "channel-unbuffered-scalar": 3,
                "far-channel-composite": 3102,
                "far-channel-scalar": 3102,
                "far-immediate-composite": 2073,
                "far-immediate-scalar": 1049,
                "far-large-capture": 5145,
                "far-large-payload-contention": 5140,
                "far-large-result": 3097,
                "far-select-composite": 3107,
                "far-select-scalar": 3107,
                "far-share-control": 1566,
                "far-task-composite": 3097,
                "far-task-scalar": 2073,
                "map-insert-composite": 7,
                "map-insert-scalar": 7,
                "map-rehash-composite": 7,
                "map-rehash-scalar": 7,
                "select-send-composite": 5,
                "select-send-scalar": 5,
                "task-clone-composite": 2076,
                "task-clone-scalar": 1564,
                "task-composite": 2076,
                "task-scalar": 1564,
            },
        )
        self.assertTrue(all(row.operations_per_batch == 512 for row in self.manifest.rows))
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

    def test_runtime_exit_metrics_are_reported_and_never_gated(self) -> None:
        # OWNER RULING 2026-09-03 (Wave E, E5): the five runtime-exit metrics
        # are telemetry. Every row still REQUIRES them -- the record is read,
        # parsed and written to the report -- and no row or cross-row invariant
        # may name one. The old `credit_stalls eq 128` and the payload-derived
        # `peak_transport_bytes` windows asserted a byte-credit transport this
        # tree does not have; the measured table (NOTES, E5) is the record of
        # why.
        telemetry = {
            metric.name for metric in self.manifest.metrics if metric.source == "runtime_exit"
        }
        self.assertEqual(
            telemetry,
            {"bytes_copied", "bytes_moved", "callback_count", "data_slot_stalls", "peak_transport_bytes"},
        )
        for row in self.manifest.rows:
            with self.subTest(row=row.row_id):
                self.assertTrue(telemetry <= set(row.required_metrics))
                self.assertFalse(
                    any(item.metric in telemetry for item in row.invariants),
                    "a runtime-exit metric is gated",
                )
        self.assertFalse(
            any(item.metric in telemetry for item in self.manifest.cross_row_invariants)
        )
        self.assertNotIn("far-jumbo-contention", {row.row_id for row in self.manifest.rows})

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
