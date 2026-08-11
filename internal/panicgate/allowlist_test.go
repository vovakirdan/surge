package panicgate

import "testing"

// Every way the ledger and the tree can disagree, proved to fire. A gate whose
// failure paths are never executed is a gate that is green for reasons nobody
// has checked; the whole point of this package is that such a thing is not
// evidence.

// One file and one function are enough for these: the key is file + function +
// ordinal, and what varies between the cases below is the ordinal and the words.
const (
	testFile = "a.c"
	testFunc = "f"
)

func site(ordinal int, msg string) Site {
	return Site{File: testFile, Function: testFunc, Ordinal: ordinal, Raiser: "raise", Message: msg}
}

func ledger(rows ...Row) *Allowlist {
	return &Allowlist{
		Version: 1,
		Groups:  []Group{{ID: "G", Owner: "o", Disposition: "d", Reason: "r", InvalidatedWhen: "w"}},
		Sites:   rows,
	}
}

func TestAnUnexcusedRaiseIsUncovered(t *testing.T) {
	f := Check([]Site{site(1, "boom")}, nil, ledger())
	if len(f.Uncovered) != 1 || f.Uncovered[0].Message != "boom" {
		t.Fatalf("uncovered = %+v", f.Uncovered)
	}
	covered := Check([]Site{site(1, "boom")},
		map[string][]string{"boom": {"dir/fix"}}, ledger())
	if len(covered.Uncovered) != 0 {
		t.Fatalf("a raise a fixture records must not be reported: %+v", covered.Uncovered)
	}
}

func TestAWordingChangeIsReportedAtTheSameSite(t *testing.T) {
	f := Check(
		[]Site{site(1, "new words")},
		nil,
		ledger(Row{Site: "a.c::f#1", Message: "old words", Group: "G"}),
	)
	if len(f.Drifted) != 1 {
		t.Fatalf("drifted = %+v (uncovered %+v, stale %+v)", f.Drifted, f.Uncovered, f.Stale)
	}
	if f.Drifted[0].Recorded != "old words" || f.Drifted[0].Actual != "new words" {
		t.Fatalf("drift = %+v", f.Drifted[0])
	}
	if len(f.Uncovered) != 0 || len(f.Stale) != 0 {
		t.Fatalf("a wording change must be one finding, not three: %+v", f)
	}
}

func TestARaiseInsertedAboveOthersRenumbersRatherThanInvalidates(t *testing.T) {
	f := Check(
		[]Site{
			site(1, "brand new"),
			site(2, "first"),
			site(3, "second"),
		},
		nil,
		ledger(
			Row{Site: "a.c::f#1", Message: "first", Group: "G"},
			Row{Site: "a.c::f#2", Message: "second", Group: "G"},
		),
	)
	if len(f.Renumbered) != 2 {
		t.Fatalf("renumbered = %+v", f.Renumbered)
	}
	if f.Renumbered[0].Site.Key() != "a.c::f#2" || f.Renumbered[1].Site.Key() != "a.c::f#3" {
		t.Fatalf("renumbered to the wrong keys: %+v", f.Renumbered)
	}
	if len(f.Uncovered) != 1 || f.Uncovered[0].Message != "brand new" {
		t.Fatalf("the inserted raise must still be reported: %+v", f.Uncovered)
	}
	if len(f.Drifted) != 0 || len(f.Stale) != 0 {
		t.Fatalf("an insertion must not read as drift or staleness: %+v", f)
	}
}

func TestTwoRaisesWithOneWordingAreNotGuessedBetween(t *testing.T) {
	// Both sites say the same thing, so a row that no longer matches by
	// position cannot be re-attached to one of them without guessing.
	f := Check(
		[]Site{site(1, "same"), site(2, "same")},
		nil,
		ledger(Row{Site: "a.c::f#3", Message: "same", Group: "G"}),
	)
	if len(f.Renumbered) != 0 {
		t.Fatalf("an ambiguous move must not be guessed: %+v", f.Renumbered)
	}
	if len(f.Stale) != 1 {
		t.Fatalf("stale = %+v", f.Stale)
	}
}

func TestARowForARaiseThatIsGoneIsStale(t *testing.T) {
	f := Check(nil, nil, ledger(Row{Site: "gone.c::f#1", Message: "x", Group: "G"}))
	if len(f.Stale) != 1 {
		t.Fatalf("stale = %+v", f.Stale)
	}
}

func TestAnExcuseOvertakenByAFixtureIsRedundant(t *testing.T) {
	f := Check(
		[]Site{site(1, "boom")},
		map[string][]string{"boom": {"dir/fix"}},
		ledger(Row{Site: "a.c::f#1", Message: "boom", Group: "G"}),
	)
	if len(f.Redundant) != 1 {
		t.Fatalf("redundant = %+v", f.Redundant)
	}
}

func TestAGroupNobodyUsesAndAGroupNobodyDefinedBothFail(t *testing.T) {
	unused := Check(nil, nil, ledger())
	if len(unused.UnusedGroup) != 1 {
		t.Fatalf("unused = %+v", unused.UnusedGroup)
	}
	undefined := Check(
		[]Site{site(1, "boom")},
		nil,
		&Allowlist{Sites: []Row{{Site: "a.c::f#1", Message: "boom", Group: "nope"}}},
	)
	if len(undefined.UnknownGroup) != 1 {
		t.Fatalf("unknown = %+v", undefined.UnknownGroup)
	}
}

func TestDuplicateAndUnsortedRowsFail(t *testing.T) {
	f := Check(
		[]Site{site(1, "boom"), site(2, "bang")},
		nil,
		ledger(
			Row{Site: "a.c::f#2", Message: "bang", Group: "G"},
			Row{Site: "a.c::f#1", Message: "boom", Group: "G"},
			Row{Site: "a.c::f#1", Message: "boom", Group: "G"},
		),
	)
	if f.Unsorted == "" {
		t.Error("rows out of order must be reported")
	}
	if len(f.Duplicate) != 1 {
		t.Errorf("duplicate = %+v", f.Duplicate)
	}
}

// The bounds reporter formats its operands into the text, so the recorded
// instance and the template it came from have to compare equal.
func TestRecordedOperandsCompareAgainstTheTemplate(t *testing.T) {
	got := NormaliseMessage("array index 9 out of range for length 3")
	want := NormaliseMessage("array index {} out of range for length {}")
	if got != want {
		t.Fatalf("%q != %q", got, want)
	}
	if NormaliseMessage("integer overflow") != "integer overflow" {
		t.Fatal("a message with no operands must be left alone")
	}
}
