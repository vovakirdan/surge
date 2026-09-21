package sema

import (
	"slices"
	"testing"
)

// These rows name Result.EnumVariantUses, so the file compiles only with the record in place:
// it is not part of Stage A. 3 RUN: 1 parent, 2 leaves.
func TestReturnOriginEnumVariantRecord(t *testing.T) {
	t.Run("variant_record_deleted", func(t *testing.T) {
		f := selectorSyntaxFixture(t, selectorEnumSource, selectorEnumDigest)
		id, data := selectorVariant(t, f, selectorEnumSource, 162, 172, "Color::Red", f.checked.TypeInterner.Builtins().Int)
		delete(f.checked.EnumVariantUses, id)
		selectorAbort(t, f, data.Target)
	})
	t.Run("variant_record_rebound", func(t *testing.T) {
		f := selectorSyntaxFixture(t, selectorEnumSource, selectorEnumDigest)
		id, _ := selectorVariant(t, f, selectorEnumSource, 162, 172, "Color::Red", f.checked.TypeInterner.Builtins().Int)
		_, other := selectorVariant(t, f, selectorEnumSource, 194, 202, "Token::L", f.checked.TypeInterner.Builtins().String)
		use := f.checked.EnumVariantUses[id]
		use.Target = other.Target
		f.checked.EnumVariantUses[id] = use
		analysis, err := AnalyzeReturnOrigins(t.Context(), f.checked, []ReturnOriginUnit{f.unit})
		if err != nil || analysis == nil {
			t.Fatalf("origin analysis aborted: %v", err)
		}
		span := f.builder.Exprs.Get(id).Span
		if !slices.ContainsFunc(analysis.Pending, func(p ReturnOriginPending) bool {
			return p.SourceKey == selectorSyntaxKey && p.Span == span && p.Reason == "enum variant lacks its checked enum target"
		}) {
			t.Errorf("a record naming another target was accepted: %+v", analysis.Pending)
		}
	})
}
