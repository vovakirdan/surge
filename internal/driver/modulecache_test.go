package driver_test

import (
	"testing"

	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/project"
)

func TestModuleCache_HitMiss(t *testing.T) {
	c := driver.NewModuleCache(16)
	var d1, d2 project.Digest
	d1[0] = 1
	d2[0] = 2

	m := project.ModuleMeta{Path:"m/x", ContentHash:d1}
	first := &diag.Diagnostic{Message:"boom"}
	c.Put(m, true, first)

	if _, _, _, ok := c.Get("m/x", d2); ok {
		t.Fatal("expected miss on different content hash")
	}
	meta, br, fe, ok := c.Get("m/x", d1)
	if !ok { t.Fatal("expected hit") }
	if !br { t.Fatal("expected broken=true") }
	if fe == nil || fe.Message != "boom" { t.Fatal("expected firstErr to match") }
	if meta.Path != "m/x" { t.Fatal("wrong meta returned") }
}
