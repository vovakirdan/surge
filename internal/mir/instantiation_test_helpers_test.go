package mir_test

import (
	"fmt"
	"testing"

	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func finalizeTestInstantiationClosure(t testing.TB, typesIn *types.Interner, symbolsRes *symbols.Result, semaRes *sema.Result) {
	t.Helper()
	identity, err := sema.NewInstantiationKeyContext(typesIn, symbolsRes, func(id source.FileID) (string, error) {
		if id != 0 {
			return "", fmt.Errorf("unknown test source %d", id)
		}
		return "test.sg", nil
	})
	if err != nil {
		t.Fatalf("instantiation identity: %v", err)
	}
	semaRes.InstantiationIdentity = &identity
	if err := semaRes.FinalizeInstantiationClosure(identity, 64); err != nil {
		t.Fatalf("instantiation closure: %v", err)
	}
}
