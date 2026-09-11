//go:build runtime_v2_pending

package vm_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"surge/internal/backend/llvm"
	"surge/internal/buildpipeline"
	"surge/internal/mir"
	"surge/internal/types"
)

type numericEmitModule struct {
	ir      string
	dir     string
	typeIDs map[string]types.TypeID
}

func requireNumericProofRun(t *testing.T) {
	t.Helper()
	skip := strings.TrimSpace(os.Getenv("SURGE_SKIP_TIMEOUT_TESTS"))
	if testing.Short() || (skip != "" && skip != "0" && !strings.EqualFold(skip, "false")) {
		t.Fatal("numeric lifecycle proof requires -short=false and SURGE_SKIP_TIMEOUT_TESTS unset, 0, or false")
	}
}

func compileNumericEmitModule(t *testing.T) numericEmitModule {
	t.Helper()
	requireNumericProofRun(t)
	root := repoRoot(t)
	t.Setenv("SURGE_STDLIB", root)
	sourcePath := filepath.Join(root, "internal", "vm", "testdata", "numeric_emit.sg")
	compiled, err := buildpipeline.Compile(t.Context(), &buildpipeline.CompileRequest{
		TargetPath: sourcePath, BaseDir: root, Analysis: true,
		MaxDiagnostics: 200, Backend: buildpipeline.BackendLLVM,
	})
	if err != nil || compiled == nil || compiled.MIR == nil || compiled.Diagnose == nil {
		t.Fatalf("compile emitted numeric stand source: %v", err)
	}
	d := compiled.Diagnose
	ir, err := llvm.EmitModule(compiled.MIR, d.Sema.TypeInterner, d.Symbols.Table, d.FileSet)
	if err != nil {
		t.Fatalf("emit numeric stand module: %v", err)
	}
	dir := t.TempDir()
	in := d.Sema.TypeInterner
	m := numericEmitModule{ir: ir, dir: dir, typeIDs: map[string]types.TypeID{
		"int": in.Builtins().Int, "uint": in.Builtins().Uint,
	}}
	var header strings.Builder
	header.WriteString("// Generated from the compiled MIR, checked against emitted definitions.\n")
	for _, kind := range []string{"int", "uint"} {
		for _, operation := range []string{"copy", "explicit_clone", "discard"} {
			f := numericEmitFunc(t, compiled.MIR, operation+"_"+kind)
			ret := "ptr"
			cRet := "void*"
			if operation == "discard" {
				ret, cRet = "void", "void"
			}
			symbol := fmt.Sprintf("fn.%d", f.ID)
			if f.ParamCount != 1 || !strings.Contains(ir, "define "+ret+" @"+symbol+"(ptr %p0)") {
				t.Fatalf("numeric source function %s has an unexpected emitted ABI", f.Name)
			}
			fmt.Fprintf(&header, "%s numeric_%s_%s(void*) __asm__(%q);\n", cRet, kind, operation, symbol)
		}
		box := numericEmitFunc(t, compiled.MIR, "copy_box_"+kind)
		if box.ParamCount != 1 || len(box.Locals) == 0 {
			t.Fatalf("box parameter census missing for %s", kind)
		}
		for _, op := range []struct {
			name, prefix, params string
			id                   types.TypeID
		}{
			{"clone", "clone", "ptr %dst, ptr %src", m.typeIDs[kind]},
			{"box_clone", "clone", "ptr %dst, ptr %src", box.Locals[0].Type},
			{"clone_elem", "clone_elem", "ptr %dst, ptr %src", m.typeIDs[kind]},
			{"drop", "drop", "ptr %p", m.typeIDs[kind]},
			{"box_drop", "drop", "ptr %p", box.Locals[0].Type},
			{"drop_elem", "drop_elem", "ptr %slot", m.typeIDs[kind]},
			{"unshare", "unshare", "ptr %val", m.typeIDs[kind]},
			{"cross_clone", "cross_clone_walk", "ptr %dst, ptr %src", m.typeIDs[kind]},
		} {
			symbol := fmt.Sprintf("%s.type%d", op.prefix, op.id)
			if strings.Count(ir, "define void @"+symbol+"("+op.params+") {") != 1 {
				t.Fatalf("source did not demand exactly one expected numeric glue %s(%s)", symbol, op.params)
			}
			params := "void*"
			if strings.Contains(op.params, ",") {
				params += ", void*"
			}
			fmt.Fprintf(&header, "void numeric_%s_%s(%s) __asm__(%q);\n", kind, op.name, params, symbol)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "numeric_emit_symbols.h"), []byte(header.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("numeric emitted source=%s source_SHA256=%x IR_SHA256=%x header_SHA256=%x", sourcePath, sha256.Sum256(sourceBytes), sha256.Sum256([]byte(ir)), sha256.Sum256([]byte(header.String())))
	return m
}

func numericEmitFunc(t *testing.T, mod *mir.Module, name string) *mir.Func {
	t.Helper()
	var found *mir.Func
	for _, f := range mod.Funcs {
		if f != nil && f.Name == name {
			if found != nil {
				t.Fatalf("duplicate numeric fixture function %s", name)
			}
			found = f
		}
	}
	if found == nil {
		t.Fatalf("numeric fixture function %s was not compiled", name)
	}
	return found
}

func buildNumericEmitStand(t *testing.T, m numericEmitModule, ir string, flags []string) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("required emitted numeric proof compiler unavailable: %v", err)
	}
	dir := t.TempDir()
	path, object := filepath.Join(dir, "numeric.ll"), filepath.Join(dir, "numeric.o")
	if err := os.WriteFile(path, []byte(ir), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-O0", "-g", "-Wno-override-module"}
	args = append(args, flags...)
	args = append(args, "-x", "ir", "-c", path, "-o", object)
	cmd := exec.Command(clang, args...)
	stdout, stderr, code := runCommand(t, cmd, "")
	if code != 0 {
		t.Fatalf("compile emitted numeric IR failed (not a lifecycle witness): code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	link := []string{"-O0", "-g", "-I" + m.dir, object}
	link = append(link, flags...)
	for _, kind := range []string{"int", "uint"} {
		for _, op := range []string{"retain", "release", "clone", "unshare"} {
			link = append(link, "-Wl,--wrap=rt_big"+kind+"_"+op)
		}
	}
	t.Logf("numeric emitted object input_SHA256=%x command=%q args=%q", sha256.Sum256([]byte(ir)), clang, args)
	return buildBignumNativeStand(t, "numeric_emit", "numeric_emit.c", link)
}

// Mutate only one named, source-generated body. Every replacement has census
// one before clang sees it; a compile/link failure never counts as detection.
func numericEmitMutant(t *testing.T, m numericEmitModule, kind, mutation string) string {
	t.Helper()
	prefix := "drop"
	if mutation == "scratch" {
		prefix = "clone"
	} else if mutation == "sibling" {
		prefix = "unshare"
	}
	symbol := fmt.Sprintf("%s.type%d", prefix, m.typeIDs[kind])
	bodyRE := regexp.MustCompile(`(?s)define void @` + regexp.QuoteMeta(symbol) + `\([^\n]*\) \{.*?\n\}`)
	body := bodyRE.FindString(m.ir)
	if body == "" || len(bodyRE.FindAllStringIndex(m.ir, -1)) != 1 {
		t.Fatalf("mutant target %s census is not one", symbol)
	}
	changed := body
	switch mutation {
	case "scratch", "release-guard":
		guard := regexp.MustCompile(`br i1 (%g\d+), label %(numeric\.heap\.\d+), label %(numeric\.done\.\d+)`)
		matches := guard.FindAllStringSubmatch(body, -1)
		if len(matches) != 1 {
			t.Fatalf("%s heap guard census = %d, want one", symbol, len(matches))
		}
		changed = strings.Replace(changed, matches[0][0], "br label %"+matches[0][2], 1)
		if mutation == "scratch" {
			rc := regexp.MustCompile(`(%g\d+) = getelementptr inbounds i8, ptr (%g\d+), i64 (4|8)`)
			gep := rc.FindAllStringSubmatch(body, -1)
			if len(gep) != 1 {
				t.Fatalf("scratch mutant RC address census = %d, want one", len(gep))
			}
			replacement := "%negative.rc = getelementptr i8, ptr " + gep[0][2] + ", i64 " + gep[0][3] + "\n  " +
				gep[0][1] + " = select i1 " + matches[0][1] + ", ptr %negative.rc, ptr @__surge_rc_scratch"
			changed = strings.Replace(changed, gep[0][0], replacement, 1)
		}
	case "sibling":
		call := regexp.MustCompile(`(%g\d+) = call ptr @rt_big` + kind + `_unshare\(ptr (%g\d+)\)`)
		match := call.FindAllStringSubmatch(body, -1)
		if len(match) != 1 {
			t.Fatalf("sibling mutant call census = %d, want one", len(match))
		}
		changed = strings.Replace(changed, match[0][0], match[0][1]+" = getelementptr i8, ptr "+match[0][2]+", i64 0", 1)
	case "last-release":
		call := regexp.MustCompile(`call void @rt_big` + kind + `_release\(ptr %g\d+\)`)
		if len(call.FindAllStringIndex(body, -1)) != 1 {
			t.Fatal("last-release mutant call census is not one")
		}
		changed = call.ReplaceAllString(changed, "; negative control: omitted last release")
	default:
		t.Fatalf("unknown numeric mutation %s", mutation)
	}
	return strings.Replace(m.ir, body, changed, 1)
}
