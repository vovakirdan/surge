package driver

import (
	"fmt"
	"testing"

	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
)

// An imported copy publishes the one body its declaration facts meet. Facts
// that meet two bodies, a different declared name, receiver or signature, a
// source key that does not own the span, or a symbol that is not an imported
// function publish nothing.
func TestImportedCallableIdentityCapture(t *testing.T) {
	strings := source.NewInterner()
	table := symbols.NewTable(symbols.Hints{}, strings)
	span := source.Span{File: 3, Start: 10, End: 15}
	sig := &symbols.FunctionSignature{Params: []symbols.TypeKey{"&string"}, Result: "&string"}
	add := func(name string, flags symbols.SymbolFlags, kind symbols.SymbolKind) symbols.SymbolID {
		return table.Symbols.New(&symbols.Symbol{Name: strings.Intern(name), Kind: kind, Flags: flags, Span: span, Signature: sig})
	}
	imported := symbols.SymbolFlagImported | symbols.SymbolFlagPublic
	copySym := add("first", imported, symbols.SymbolFunction)
	aliased := table.Symbols.New(&symbols.Symbol{Name: strings.Intern("pick"), ImportName: strings.Intern("first"), Kind: symbols.SymbolFunction, Flags: imported, Span: span, Signature: sig})
	declared := add("first", symbols.SymbolFlagPublic, symbols.SymbolFunction)
	notFunction := add("first", imported, symbols.SymbolType)
	record := func(key string) sema.CallableCandidate {
		return sema.CallableCandidate{Name: "first", Source: span, SourceKey: "dep/main.sg", BodyKey: key, Params: sig.Params, Result: sig.Result}
	}
	resolve := func(file source.FileID) (string, error) {
		if file == span.File {
			return "dep/main.sg", nil
		}
		return "", fmt.Errorf("unknown source file %d", file)
	}
	capture := func(records ...sema.CallableCandidate) map[symbols.SymbolID]string {
		index := newImportedCallableIndex([]*sema.Result{{CallableCandidates: records}}, resolve)
		out := make(map[symbols.SymbolID]string)
		for id, identity := range index.capture([]*symbols.Table{table}) {
			if identity.Symbol != id || (identity.BodyKey != "" && identity.SourceKey != "dep/main.sg") {
				t.Fatalf("capture published a malformed identity: %+v", identity)
			}
			out[id] = identity.BodyKey
		}
		return out
	}
	t.Run("one_body", func(t *testing.T) {
		got := capture(record("body-a"))
		if got[copySym] != "body-a" || got[aliased] != "body-a" || len(got) != 2 {
			t.Fatalf("a copy and its alias must name the one body, declared=%d notFunction=%d: %v", declared, notFunction, got)
		}
	})
	t.Run("two_bodies_one_declaration", func(t *testing.T) {
		if got := capture(record("body-a"), record("body-b")); got[copySym] != "" || got[aliased] != "" || len(got) != 2 {
			t.Fatalf("facts meeting two bodies must publish the empty identity: %v", got)
		}
	})
	t.Run("same_body_twice", func(t *testing.T) {
		if got := capture(record("body-a"), record("body-a")); got[copySym] != "body-a" {
			t.Fatalf("one body recorded twice is still one body: %v", got)
		}
	})
	for name, mutate := range map[string]func(*sema.CallableCandidate){
		"other_name":     func(c *sema.CallableCandidate) { c.Name = "second" },
		"other_receiver": func(c *sema.CallableCandidate) { c.ReceiverKey = "Other" },
		"other_params":   func(c *sema.CallableCandidate) { c.Params = []symbols.TypeKey{"string"} },
		"other_result":   func(c *sema.CallableCandidate) { c.Result = "string" },
		"other_source":   func(c *sema.CallableCandidate) { c.SourceKey = "other.sg" },
		"other_span":     func(c *sema.CallableCandidate) { c.Source.End++ },
		"builtin_key_for_a_plain_copy": func(c *sema.CallableCandidate) {
			c.SourceKey, c.Builtin = "builtin", true
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := record("body-a")
			mutate(&c)
			if got := capture(c); len(got) != 0 {
				t.Fatalf("a record that disagrees with the copy must publish nothing: %v", got)
			}
		})
	}
}
