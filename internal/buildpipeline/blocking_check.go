package buildpipeline

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/source"
)

const blockingNotSupportedMsg = "blocking { } is not supported in the VM backend; VM is single-threaded and has no blocking pool"

func addBlockingVMErrors(req *CompileRequest, diagRes *driver.DiagnoseResult) {
	if req == nil || diagRes == nil || diagRes.Bag == nil || diagRes.Builder == nil {
		return
	}
	if req.Backend != BackendVM {
		return
	}
	for _, sp := range collectBlockingSpans(diagRes.Builder, diagRes.RootModuleFileIDs()) {
		diagRes.Bag.Add(&diag.Diagnostic{
			Severity: diag.SevError,
			Code:     diag.FutBlockingNotSupported,
			Message:  blockingNotSupportedMsg,
			Primary:  sp,
		})
	}
}

// collectBlockingSpans reads the parsed expression arena rather than a lowered
// tree. The guard runs before the pipeline lowers HIR, so a guard that asked
// HIR would find nothing to refuse and the program would reach the VM, which
// has no opcode for the instruction and panics mid-run instead.
//
// Arena order is allocation order, which is parse order, so the spans — and
// therefore the diagnostics — come out the same on every compile.
func collectBlockingSpans(builder *ast.Builder, files []source.FileID) []source.Span {
	if builder == nil || builder.Exprs == nil || builder.Exprs.Arena == nil || len(files) == 0 {
		return nil
	}
	compiled := make(map[source.FileID]struct{}, len(files))
	for _, id := range files {
		compiled[id] = struct{}{}
	}
	var spans []source.Span
	for i := uint32(1); i <= builder.Exprs.Arena.Len(); i++ {
		expr := builder.Exprs.Get(ast.ExprID(i))
		if expr == nil || expr.Kind != ast.ExprBlocking {
			continue
		}
		if _, ok := compiled[expr.Span.File]; !ok {
			continue
		}
		spans = append(spans, expr.Span)
	}
	return spans
}
