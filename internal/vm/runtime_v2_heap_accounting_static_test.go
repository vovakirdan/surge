//go:build runtime_v2_pending

package vm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRuntimeV2HeapAccountingStaticPublicABI(t *testing.T) {
	root := repoRoot(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("clang is required for Runtime V2 heap accounting static check: %v", err)
	}

	source := `
#include "rt.h"

void* (*runtime_v2_check_rt_alloc)(uint64_t, uint64_t) = rt_alloc;
void (*runtime_v2_check_rt_free)(uint8_t*, uint64_t, uint64_t) = rt_free;
void* (*runtime_v2_check_rt_realloc)(uint8_t*, uint64_t, uint64_t, uint64_t) = rt_realloc;
void* (*runtime_v2_check_rt_heap_stats)(void) = rt_heap_stats;
`

	cmd := exec.Command(
		clang,
		"-std=c11",
		"-Wall",
		"-Wextra",
		"-Werror",
		"-fsyntax-only",
		"-I"+filepath.Join(root, "runtime", "native"),
		"-x",
		"c",
		"-",
	)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(source)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Runtime V2 heap accounting public ABI check failed:\n%s", output)
	}
}

func TestRuntimeV2HeapAccountingStaticTask5SkeletonShape(t *testing.T) {
	root := repoRoot(t)
	nativeSources := readRuntimeV2HeapAccountingNativeSources(t, root)

	var failures []string
	if !runtimeV2HeapAccountingHasOwnerShape(nativeSources) {
		failures = append(failures,
			"missing runtime/shard-owned heap accounting shape: expose heap accounting cells from rt_runtime or rt_shard, not only rt_alloc.c globals",
		)
	}
	if !runtimeV2HeapAccountingHasColdPath(nativeSources) {
		failures = append(failures,
			"missing explicit cold heap accounting path for allocations before runtime state or outside worker context",
		)
	}
	if !runtimeV2HeapAccountingHasLaneSelection(nativeSources) {
		failures = append(failures,
			"missing lane-local heap accounting selection for worker, I/O, blocking, or synchronous-runner contexts",
		)
	}
	if !runtimeV2HeapAccountingHasLaneInstallSites(nativeSources) {
		failures = append(failures,
			"missing installed heap accounting cells for main, worker, I/O, blocking, and compensation lanes",
		)
	}

	if len(failures) > 0 {
		t.Fatalf("Runtime V2 heap accounting Task 5 skeleton shape is not implemented yet:\n- %s",
			strings.Join(failures, "\n- "))
	}
}

func TestRuntimeV2HeapAccountingStaticTask6RecordMigrationShape(t *testing.T) {
	root := repoRoot(t)
	rtAlloc := readRuntimeV2HeapAccountingFile(t, root, "runtime/native/rt_alloc.c")
	nativeSources := readRuntimeV2HeapAccountingNativeSources(t, root)

	var failures []string
	if runtimeV2HeapAccountingHasOldGlobalCounterSet(rtAlloc) ||
		runtimeV2HeapAccountingHasFileScopeHeapCounter(nativeSources) {
		failures = append(failures,
			"old process-global heap counter source of truth is still present as file-scope static atomics",
		)
	}
	if runtimeV2HeapAccountingHasDirectOldGlobalWrites(rtAlloc) {
		failures = append(failures,
			"record_alloc/record_free/record_realloc still write directly to old heap_* globals instead of selecting one accounting cell",
		)
	}
	if !runtimeV2HeapAccountingRecordHelpersUseAccountingAPI(rtAlloc) {
		failures = append(failures,
			"record_alloc/record_free/record_realloc do not use the rt_heap_accounting record API",
		)
	}

	if len(failures) > 0 {
		t.Fatalf("Runtime V2 heap accounting Task 6 record migration shape is not implemented yet:\n- %s",
			strings.Join(failures, "\n- "))
	}
}

func TestRuntimeV2HeapAccountingStaticTask7SnapshotAggregationShape(t *testing.T) {
	root := repoRoot(t)
	rtAlloc := readRuntimeV2HeapAccountingFile(t, root, "runtime/native/rt_alloc.c")

	var failures []string
	if runtimeV2HeapAccountingStatsLoadsOldGlobals(rtAlloc) {
		failures = append(failures,
			"rt_heap_stats() still loads old heap_* globals directly instead of aggregating accounting cells",
		)
	}
	if !runtimeV2HeapAccountingStatsUsesAggregation(rtAlloc) {
		failures = append(failures,
			"rt_heap_stats() does not call the rt_heap_accounting snapshot aggregation API",
		)
	}

	if len(failures) > 0 {
		t.Fatalf("Runtime V2 heap accounting Task 7 snapshot aggregation shape is not implemented yet:\n- %s",
			strings.Join(failures, "\n- "))
	}
}

func readRuntimeV2HeapAccountingFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func readRuntimeV2HeapAccountingNativeSources(t *testing.T, root string) string {
	t.Helper()
	rels := []string{
		"runtime/native/rt_async_internal.h",
		"runtime/native/rt_runtime.c",
		"runtime/native/rt_alloc.c",
		"runtime/native/rt_heap_accounting.h",
		"runtime/native/rt_heap_accounting.c",
		"runtime/native/rt_async_state.c",
		"runtime/native/rt_worker_turn.c",
		"runtime/native/rt_async_poll.c",
		"runtime/native/rt_async_blocking.c",
	}

	var combined strings.Builder
	for _, rel := range rels {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("read %s: %v", rel, err)
		}
		combined.WriteString("\n/* ")
		combined.WriteString(rel)
		combined.WriteString(" */\n")
		combined.Write(data)
	}
	return combined.String()
}

func runtimeV2HeapAccountingHasOwnerShape(source string) bool {
	code := runtimeV2HeapAccountingCodeOnly(source)
	cellType := regexp.MustCompile(`\b(?:typedef\s+struct\s+rt_heap_accounting_cell|struct\s+rt_heap_accounting_cell)\b`)
	accountingType := regexp.MustCompile(`\b(?:typedef\s+struct\s+rt_heap_accounting|struct\s+rt_heap_accounting)\b`)
	ownerField := regexp.MustCompile(`(?s)struct\s+rt_(?:runtime|shard)\s*\{[^}]*\brt_heap_accounting\s*\*?\s+heap_accounting\b`)
	return cellType.MatchString(code) &&
		accountingType.MatchString(code) &&
		ownerField.MatchString(code)
}

func runtimeV2HeapAccountingHasColdPath(source string) bool {
	code := runtimeV2HeapAccountingCodeOnly(source)
	coldCell := regexp.MustCompile(`\brt_heap_accounting_cell\s+\*?\s*cold_cell\b`)
	coldAccessor := regexp.MustCompile(`\brt_heap_accounting_[a-z0-9_]*cold[a-z0-9_]*(?:cell|account)`)
	return coldCell.MatchString(code) || coldAccessor.MatchString(code)
}

func runtimeV2HeapAccountingHasLaneSelection(source string) bool {
	code := runtimeV2HeapAccountingCodeOnly(source)
	tlsCell := regexp.MustCompile(`(?m)^(?:static\s+)?_Thread_local\s+rt_heap_accounting_cell\s*\*\s+(tls_[a-z0-9_]*heap[a-z0-9_]*cell)\s*;`)
	matches := tlsCell.FindStringSubmatch(code)
	if len(matches) != 2 {
		return false
	}
	body, ok := runtimeV2HeapAccountingFunctionBody(code, "rt_heap_accounting_current_cell")
	if !ok {
		body, ok = runtimeV2HeapAccountingFunctionBody(code, "rt_heap_accounting_select_cell")
	}
	return ok && strings.Contains(body, matches[1]) && strings.Contains(body, "cold_cell")
}

func runtimeV2HeapAccountingHasLaneInstallSites(source string) bool {
	code := runtimeV2HeapAccountingCodeOnly(source)
	required := []string{
		"rt_heap_accounting_set_current_cell(ctx->heap_cell)",
		"rt_heap_accounting_main_cell",
		"rt_heap_accounting_io_cell",
		"rt_heap_accounting_blocking_cell",
		"rt_heap_accounting_compensation_cell",
		"rt_heap_accounting_set_current_cell(saved_cell)",
		"rt_heap_accounting_set_current_cell(NULL)",
	}
	for _, needle := range required {
		if !strings.Contains(code, needle) {
			return false
		}
	}
	return true
}

func runtimeV2HeapAccountingHasOldGlobalCounterSet(source string) bool {
	code := runtimeV2HeapAccountingCodeOnly(source)
	counterNames := []string{
		"heap_alloc_count",
		"heap_free_count",
		"heap_live_blocks",
		"heap_live_bytes",
	}
	for _, name := range counterNames {
		pattern := regexp.MustCompile(`(?m)^static\s+_Atomic\s+uint64_t\s+` + regexp.QuoteMeta(name) + `\s*;`)
		if !pattern.MatchString(code) {
			return false
		}
	}
	return true
}

func runtimeV2HeapAccountingHasFileScopeHeapCounter(source string) bool {
	code := runtimeV2HeapAccountingCodeOnly(source)
	pattern := regexp.MustCompile(`(?m)^static\s+_Atomic\s+uint64_t\s+[a-z0-9_]*heap[a-z0-9_]*\s*;`)
	return pattern.MatchString(code)
}

func runtimeV2HeapAccountingHasDirectOldGlobalWrites(source string) bool {
	code := runtimeV2HeapAccountingCodeOnly(source)
	for _, fn := range []string{"record_alloc", "record_free", "record_realloc"} {
		body, ok := runtimeV2HeapAccountingFunctionBody(code, fn)
		if !ok {
			continue
		}
		if strings.Contains(body, "&heap_alloc_count") ||
			strings.Contains(body, "&heap_free_count") ||
			strings.Contains(body, "&heap_live_blocks") ||
			strings.Contains(body, "&heap_live_bytes") {
			return true
		}
	}
	return false
}

func runtimeV2HeapAccountingRecordHelpersUseAccountingAPI(source string) bool {
	code := runtimeV2HeapAccountingCodeOnly(source)
	checks := map[string]string{
		"record_alloc":   "rt_heap_accounting_record_alloc",
		"record_free":    "rt_heap_accounting_record_free",
		"record_realloc": "rt_heap_accounting_record_realloc",
	}
	for helper, api := range checks {
		body, ok := runtimeV2HeapAccountingFunctionBody(code, helper)
		if !ok || !strings.Contains(body, api) {
			return false
		}
	}
	return true
}

func runtimeV2HeapAccountingStatsLoadsOldGlobals(source string) bool {
	code := runtimeV2HeapAccountingCodeOnly(source)
	body, ok := runtimeV2HeapAccountingFunctionBody(code, "rt_heap_stats")
	if !ok {
		return false
	}
	return strings.Contains(body, "atomic_load_explicit(&heap_alloc_count") ||
		strings.Contains(body, "atomic_load_explicit(&heap_free_count") ||
		strings.Contains(body, "atomic_load_explicit(&heap_live_blocks") ||
		strings.Contains(body, "atomic_load_explicit(&heap_live_bytes")
}

func runtimeV2HeapAccountingStatsUsesAggregation(source string) bool {
	code := runtimeV2HeapAccountingCodeOnly(source)
	body, ok := runtimeV2HeapAccountingFunctionBody(code, "rt_heap_stats")
	return ok && strings.Contains(body, "rt_heap_accounting_snapshot")
}

func runtimeV2HeapAccountingCodeOnly(source string) string {
	withoutBlocks := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(source, "")
	return regexp.MustCompile(`(?m)//.*$`).ReplaceAllString(withoutBlocks, "")
}

func runtimeV2HeapAccountingFunctionBody(source, name string) (string, bool) {
	searchStart := 0
	for {
		relStart := strings.Index(source[searchStart:], name+"(")
		if relStart < 0 {
			return "", false
		}
		start := searchStart + relStart
		tail := source[start:]
		openRel := strings.Index(tail, "{")
		if openRel < 0 {
			return "", false
		}
		semiRel := strings.Index(tail, ";")
		if semiRel >= 0 && semiRel < openRel {
			searchStart = start + len(name)
			continue
		}
		open := start + openRel
		depth := 0
		for i := open; i < len(source); i++ {
			switch source[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return source[open : i+1], true
				}
			}
		}
		return "", false
	}
}
