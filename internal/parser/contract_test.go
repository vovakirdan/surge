package parser

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
)

func TestParseContractWithMembers(t *testing.T) {
	input := `
contract ErrorLike<E>(
    @readonly field msg: string;
    pub async fn format(self: &E) -> string;
)
`
	builder, fileID, bag := parseSource(t, input)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}

	file := builder.Files.Get(fileID)
	if file == nil {
		t.Fatal("file not found")
	}
	if len(file.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(file.Items))
	}

	contract, ok := builder.Items.Contract(file.Items[0])
	if !ok || contract == nil {
		t.Fatalf("expected contract item, got %v", builder.Items.Get(file.Items[0]).Kind)
	}
	if contract.Visibility != ast.VisPrivate {
		t.Fatalf("expected private visibility, got %v", contract.Visibility)
	}
	if len(contract.Generics) != 1 {
		t.Fatalf("expected 1 generic parameter, got %d", len(contract.Generics))
	}
	if got := lookupNameOr(builder, contract.Generics[0], ""); got != "E" {
		t.Fatalf("unexpected generic name: %q", got)
	}

	members := builder.Items.GetContractItemIDs(contract)
	if len(members) != 2 {
		t.Fatalf("expected 2 contract members, got %d", len(members))
	}

	fieldItem := builder.Items.ContractItem(members[0])
	if fieldItem == nil || fieldItem.Kind != ast.ContractItemField {
		t.Fatalf("expected first member to be a field, got %v", fieldItem)
	}
	field := builder.Items.ContractField(ast.ContractFieldID(fieldItem.Payload))
	if field == nil {
		t.Fatal("contract field payload missing")
	}
	if got := lookupNameOr(builder, field.Name, ""); got != "msg" {
		t.Fatalf("unexpected field name: %q", got)
	}
	if field.SemicolonSpan.Empty() {
		t.Fatal("expected semicolon span for contract field")
	}
	attrs := builder.Items.CollectAttrs(field.AttrStart, field.AttrCount)
	if len(attrs) != 1 || lookupNameOr(builder, attrs[0].Name, "") != "readonly" {
		t.Fatalf("unexpected field attributes: %+v", attrs)
	}

	fnItem := builder.Items.ContractItem(members[1])
	if fnItem == nil || fnItem.Kind != ast.ContractItemFn {
		t.Fatalf("expected second member to be a function, got %v", fnItem)
	}
	fnReq := builder.Items.ContractFn(ast.ContractFnID(fnItem.Payload))
	if fnReq == nil {
		t.Fatal("contract function payload missing")
	}
	if fnReq.Flags&ast.FnModifierPublic == 0 || fnReq.Flags&ast.FnModifierAsync == 0 {
		t.Fatalf("expected pub async flags, got %v", fnReq.Flags)
	}
	if fnReq.ParamsCount != 1 {
		t.Fatalf("expected 1 parameter, got %d", fnReq.ParamsCount)
	}
	if fnReq.SemicolonSpan.Empty() {
		t.Fatal("expected semicolon span for contract function")
	}
}

func TestParseContractVisibility(t *testing.T) {
	input := `
pub contract Printable(
    fn print(self: Printable) -> int;
)
`
	builder, fileID, bag := parseSource(t, input)
	if bag.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diagnosticsSummary(bag))
	}
	file := builder.Files.Get(fileID)
	if len(file.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(file.Items))
	}
	contract, ok := builder.Items.Contract(file.Items[0])
	if !ok || contract == nil {
		t.Fatalf("expected contract item, got %v", builder.Items.Get(file.Items[0]).Kind)
	}
	if contract.Visibility != ast.VisPublic {
		t.Fatalf("expected public contract, got %v", contract.Visibility)
	}
}

func TestContractMissingSemicolon(t *testing.T) {
	input := `
contract Bad(
    field id: int
)
`
	builder, fileID, bag := parseSource(t, input)
	if !bag.HasErrors() {
		t.Fatal("expected diagnostics for missing semicolon")
	}
	if !hasDiagnosticCode(bag, diag.SynExpectSemicolon) {
		t.Fatalf("expected SynExpectSemicolon, got %s", diagnosticsSummary(bag))
	}
	file := builder.Files.Get(fileID)
	if len(file.Items) != 0 {
		t.Fatalf("expected no items on parse failure, got %d", len(file.Items))
	}
}

func TestContractFunctionBodyError(t *testing.T) {
	input := `
contract Bad(
    fn make() {}
)
`
	builder, fileID, bag := parseSource(t, input)
	if !bag.HasErrors() {
		t.Fatal("expected diagnostics for function body inside contract")
	}
	if !hasDiagnosticCode(bag, diag.SynUnexpectedToken) {
		t.Fatalf("expected SynUnexpectedToken, got %s", diagnosticsSummary(bag))
	}
	file := builder.Files.Get(fileID)
	if len(file.Items) != 0 {
		t.Fatalf("expected no items on parse failure, got %d", len(file.Items))
	}
}

func TestFieldKeywordOutsideContract(t *testing.T) {
	builder, fileID, bag := parseSource(t, "field value: int;")
	if !bag.HasErrors() {
		t.Fatal("expected diagnostics for top-level field usage")
	}
	if !hasDiagnosticCode(bag, diag.SynUnexpectedTopLevel) {
		t.Fatalf("expected SynUnexpectedTopLevel, got %s", diagnosticsSummary(bag))
	}
	file := builder.Files.Get(fileID)
	if len(file.Items) != 0 {
		t.Fatalf("expected no items, got %d", len(file.Items))
	}
}

func hasDiagnosticCode(bag *diag.Bag, code diag.Code) bool {
	if bag == nil {
		return false
	}
	for _, d := range bag.Items() {
		if d.Code == code {
			return true
		}
	}
	return false
}
