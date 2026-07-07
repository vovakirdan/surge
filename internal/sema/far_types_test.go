package sema

import (
	"context"
	"testing"

	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestFarTypeCanonicalIdentityAndOperands(t *testing.T) {
	src := `
type TcpConn = { __opaque: int };
type Channel<T> = { __opaque: int };
type Message = { id: int };
type RemoteConn = far TcpConn;

fn accepts_alias(x: RemoteConn) -> bool {
    return x is far TcpConn;
}

fn carries_far(ch: Channel<far TcpConn>) -> nothing {
    return nothing;
}

fn remote_channel(ch: far Channel<Message>) -> nothing {
    return nothing;
}
`
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(64)
	res := Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  symRes,
	})
	if semaBag.HasErrors() {
		t.Fatalf("sema diagnostics: %s", diagnosticsSummary(semaBag))
	}

	remoteAlias := requireTypeSymbol(t, symRes, "RemoteConn")
	aliasInfo, ok := res.TypeInterner.AliasInfo(remoteAlias.Type)
	if !ok || aliasInfo == nil {
		t.Fatalf("RemoteConn is not an alias type: %s", types.Label(res.TypeInterner, remoteAlias.Type))
	}
	target, ok := res.TypeInterner.AliasTarget(remoteAlias.Type)
	if !ok {
		t.Fatalf("RemoteConn has no alias target")
	}
	targetInfo, ok := res.TypeInterner.Lookup(target)
	if !ok || targetInfo.Kind != types.KindFar {
		t.Fatalf("RemoteConn target = %s, want far", types.Label(res.TypeInterner, target))
	}
	if res.TypeInterner.IsCopy(target) {
		t.Fatalf("far handle must be move-only by default")
	}

	var sawFarIsOperand bool
	for _, operand := range res.IsOperands {
		info, ok := res.TypeInterner.Lookup(operand.Type)
		if ok && info.Kind == types.KindFar {
			sawFarIsOperand = true
			break
		}
	}
	if !sawFarIsOperand {
		t.Fatalf("expected `is far TcpConn` operand to resolve as far type")
	}

	carriesFarFn := requireFunctionSymbol(t, symRes, "carries_far")
	remoteChannelFn := requireFunctionSymbol(t, symRes, "remote_channel")
	if carriesFarFn.Signature == nil || len(carriesFarFn.Signature.Params) != 1 {
		t.Fatalf("missing carries_far signature")
	}
	if remoteChannelFn.Signature == nil || len(remoteChannelFn.Signature.Params) != 1 {
		t.Fatalf("missing remote_channel signature")
	}
	if got := string(carriesFarFn.Signature.Params[0]); got != "Channel<far TcpConn>" {
		t.Fatalf("carries_far param key = %q, want %q", got, "Channel<far TcpConn>")
	}
	if got := string(remoteChannelFn.Signature.Params[0]); got != "far Channel<Message>" {
		t.Fatalf("remote_channel param key = %q, want %q", got, "far Channel<Message>")
	}
	if got := types.Label(res.TypeInterner, target); got != "far TcpConn" {
		t.Fatalf("far label = %q, want %q", got, "far TcpConn")
	}
}

func requireTypeSymbol(t *testing.T, res *symbols.Result, name string) *symbols.Symbol {
	t.Helper()
	return requireSymbol(t, res, name, symbols.SymbolType)
}

func requireFunctionSymbol(t *testing.T, res *symbols.Result, name string) *symbols.Symbol {
	t.Helper()
	return requireSymbol(t, res, name, symbols.SymbolFunction)
}

func requireSymbol(t *testing.T, res *symbols.Result, name string, kind symbols.SymbolKind) *symbols.Symbol {
	t.Helper()
	if res == nil || res.Table == nil || res.Table.Symbols == nil || res.Table.Strings == nil {
		t.Fatal("missing symbol result")
	}
	for i := 1; i <= res.Table.Symbols.Len(); i++ {
		sym := res.Table.Symbols.Get(symbols.SymbolID(i))
		if sym == nil || sym.Kind != kind {
			continue
		}
		text, ok := res.Table.Strings.Lookup(sym.Name)
		if ok && text == name {
			return sym
		}
	}
	t.Fatalf("missing %s symbol %q", kind, name)
	return nil
}
