package sema

import (
	"context"
	"testing"

	"surge/internal/diag"
)

// A borrow reaches a task through three spellings, and the storage fact must
// be the same for all three: the borrowed local is an address-stable place of
// the creator's activation. `spawn f(&s)` said so already; `f(&s).await()` and
// `s.method()` returning a Task through a sync forwarder did not, and a child
// reading `s` through the pointer it was handed found a per-poll alloca
// once the constructor stopped copying the referent (RV2-DEBT-303's box).
func TestTaskProducingCallPinsItsBorrowedPlaces(t *testing.T) {
	src := `
tag Success<T>(T);
tag Cancelled();
type TaskResult<T> = Success(T) | Cancelled;
@intrinsic
type Task<T> = { __opaque: int };
extern<Task<T>> {
    @intrinsic pub fn await(self: own Task<T>) -> TaskResult<T>;
    @intrinsic pub fn clone(self: &Task<T>) -> Task<T>;
}
type Sem = { n: int };
async fn sem_task(s: &Sem) -> nothing { let v = (*s).n; return nothing; }
extern<Sem> {
    pub fn acquire(self: &Sem) -> Task<nothing> { return sem_task(self); }
}
async fn w_method(sem: Sem) -> int { let mut s = sem; let _ = s.acquire().await(); return 0; }
async fn w_direct(sem: Sem) -> int { let mut s = sem; let _ = sem_task(&s).await(); return 0; }
async fn w_spawn(sem: Sem) -> int { let mut s = sem; let t = spawn sem_task(&s); let _ = t.await(); return 0; }
async fn w_clone(sem: Sem) -> int { let t = spawn sem_task(&sem); let c = t.clone(); let _ = t.await(); let _ = c.await(); return 0; }
`
	builder, fileID, parseBag := parseSource(t, src)
	if n := len(parseBag.Items()); n != 0 {
		t.Fatalf("parse produced %d diagnostics", n)
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(64)
	res := Check(context.Background(), builder, fileID, Options{
		Reporter:   &diag.BagReporter{Bag: semaBag},
		Symbols:    symRes,
		ModulePath: builder.StringsInterner.Intern("core"),
	})
	for _, d := range semaBag.Items() {
		if d.Severity == diag.SevError {
			t.Fatalf("clean program refused: %s: %s", d.Code.ID(), d.Message)
		}
	}
	pinned := map[string]string{}
	for key, syms := range res.StableActivationPlaces {
		if !key.Fn.IsValid() {
			continue
		}
		fn := symRes.Table.Symbols.Get(key.Fn)
		if fn == nil {
			continue
		}
		for _, s := range syms {
			if sym := symRes.Table.Symbols.Get(s); sym != nil {
				pinned[builder.StringsInterner.MustLookup(fn.Name)] = builder.StringsInterner.MustLookup(sym.Name)
			}
		}
	}
	for _, fn := range []string{"w_method", "w_direct", "w_spawn"} {
		if pinned[fn] != "s" {
			t.Fatalf("%s: borrowed local not recorded address-stable (got %q); all: %v", fn, pinned[fn], pinned)
		}
	}
	// t.clone() borrows a TASK handle, not the frame: the tracker owns that
	// story, and no place of w_clone's frame is pinned by the clone. (The spawn
	// in it pins `sem`, which is the spawn's doing.)
	if pinned["w_clone"] != "sem" {
		t.Fatalf("w_clone: want only the spawn's pin on sem, got %q", pinned["w_clone"])
	}
}
