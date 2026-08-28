package vm_test

import (
	"strconv"
	"strings"
	"testing"
)

// One scheduler-pop record, as its owner published it. The owner is named on
// the record because the cell behind it has exactly one: a carrier, or the
// runtime's control lane. Everything here is something that owner did inside
// its own pop -- where it took the task from, a steal it was refused, and
// whether the connection task it ran belonged to the shard it ran on.
type schedTraceOwner struct {
	owner             string
	id                uint64
	shard             string
	local             uint64
	inject            uint64
	steal             uint64
	events            uint64
	popMix            uint64
	tier1StealDenied  uint64
	connOwnerLocal    uint64
	connOwnerMismatch uint64
}

type schedTrace struct {
	mode              string
	seed              uint64
	local             uint64
	inject            uint64
	steal             uint64
	tier1StealDenied  uint64
	connOwnerPlaced   uint64
	connOwnerLocal    uint64
	connOwnerMismatch uint64
	events            uint64
	popMix            uint64
	owners            []schedTraceOwner
	declaredCarriers  uint64
	declaredOwners    uint64
	unownedPops       uint64
	droppedRecords    uint64
}

type execTrace map[string]uint64

func parseExecTrace(t *testing.T, stderr string) execTrace {
	t.Helper()
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.HasPrefix(line, "TRACE_EXEC ") {
			continue
		}
		out := execTrace{}
		fields := strings.Fields(line)
		for _, field := range fields[1:] {
			kv := strings.SplitN(field, "=", 2)
			if len(kv) != 2 || kv[0] == "reason" {
				continue
			}
			v, err := strconv.ParseUint(kv[1], 10, 64)
			if err != nil {
				t.Fatalf("parse TRACE_EXEC %s: %v", kv[0], err)
			}
			out[kv[0]] = v
		}
		return out
	}
	t.Fatalf("missing TRACE_EXEC in stderr:\n%s", stderr)
	return nil
}

func parseExecSnapshot(t *testing.T, stderr string) execTrace {
	t.Helper()
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.HasPrefix(line, "TRACE_EXEC_SNAPSHOT ") {
			continue
		}
		out := execTrace{}
		fields := strings.Fields(line)
		for _, field := range fields[1:] {
			kv := strings.SplitN(field, "=", 2)
			if len(kv) != 2 || kv[0] == "reason" {
				continue
			}
			v, err := strconv.ParseUint(kv[1], 10, 64)
			if err != nil {
				t.Fatalf("parse TRACE_EXEC_SNAPSHOT %s: %v", kv[0], err)
			}
			out[kv[0]] = v
		}
		return out
	}
	t.Fatalf("missing TRACE_EXEC_SNAPSHOT in stderr:\n%s", stderr)
	return nil
}

func schedTraceFields(line string) map[string]string {
	out := map[string]string{}
	for _, field := range strings.Fields(line)[1:] {
		if kv := strings.SplitN(field, "=", 2); len(kv) == 2 {
			out[kv[0]] = kv[1]
		}
	}
	return out
}

func schedTraceU64(t *testing.T, fields map[string]string, key string) uint64 {
	t.Helper()
	raw, ok := fields[key]
	if !ok {
		t.Fatalf("SCHED_TRACE record has no %s: %v", key, fields)
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		t.Fatalf("parse %s=%q: %v", key, raw, err)
	}
	return v
}

// parseSchedTrace reads the per-owner scheduler records. No row sums across
// owners: a sum over owners has writers that share neither a lock nor an owner,
// so the reader adds the owner records up here and learns from the runtime
// record's `owners` exactly which set it added up. The runtime's own row is not
// such a sum -- it is one owner's three counts, of events no carrier and no
// lane performed.
func parseSchedTrace(t *testing.T, stderr string) schedTrace {
	t.Helper()
	out := schedTrace{}
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.HasPrefix(line, "SCHED_TRACE") {
			continue
		}
		fields := schedTraceFields(line)
		switch fields["owner"] {
		case "runtime":
			out.mode = fields["mode"]
			out.seed = schedTraceU64(t, fields, "seed")
			out.declaredCarriers = schedTraceU64(t, fields, "carriers")
			out.declaredOwners = schedTraceU64(t, fields, "owners")
			out.connOwnerPlaced = schedTraceU64(t, fields, "conn_owner_placed")
			out.unownedPops = schedTraceU64(t, fields, "unowned_pops")
			out.droppedRecords = schedTraceU64(t, fields, "dropped_records")
		case "carrier", "control":
			owner := schedTraceOwner{
				owner:             fields["owner"],
				id:                schedTraceU64(t, fields, "id"),
				shard:             fields["shard"],
				local:             schedTraceU64(t, fields, "local"),
				inject:            schedTraceU64(t, fields, "inject"),
				steal:             schedTraceU64(t, fields, "steal"),
				events:            schedTraceU64(t, fields, "events"),
				popMix:            schedTraceU64(t, fields, "pop_mix"),
				tier1StealDenied:  schedTraceU64(t, fields, "tier1_steal_denied"),
				connOwnerLocal:    schedTraceU64(t, fields, "conn_owner_local"),
				connOwnerMismatch: schedTraceU64(t, fields, "conn_owner_mismatch"),
			}
			out.owners = append(out.owners, owner)
			out.local += owner.local
			out.inject += owner.inject
			out.steal += owner.steal
			out.events += owner.events
			// Refused steals and connection runs are the popper's own, so they
			// arrive per owner and are added up over the owner set the runtime
			// record names, exactly as the pops are.
			out.tier1StealDenied += owner.tier1StealDenied
			out.connOwnerLocal += owner.connOwnerLocal
			out.connOwnerMismatch += owner.connOwnerMismatch
			// Composed over the owner set the runtime record names, not read
			// off a word several owners wrote. Addition is what makes the
			// owners' fingerprints composable at all.
			out.popMix += owner.popMix
		default:
			t.Fatalf("SCHED_TRACE record names no owner:\n%s", line)
		}
	}
	// The runtime record is emitted last and always names at least the control
	// lane, so its absence is the absence of the dump.
	if out.declaredOwners == 0 {
		t.Fatalf("missing SCHED_TRACE runtime record in stderr")
	}
	return out
}
