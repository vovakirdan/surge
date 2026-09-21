package driver

import (
	"slices"
	"strings"
	"testing"

	"surge/internal/sema"
)

// A core task constructor is believed by its declaration: checkpoint and sleep start a
// task that captures nothing, and a clone hands back the receiver's own task. Only the
// payload is walked, so a clone whose payload is refused keeps a refusal at the call.
// A function value and a generic helper stay refused.
// What a running task borrows is the task check's question, not this certificate's.
type taskCertificateSite struct {
	span   handleCertificateSpan
	reason string
}

type taskCertificateCase struct {
	handle handleCertificateCase
	// kept: each span keeps a Pending with exactly its reason.
	kept []taskCertificateSite
	// dropped: each span no longer has a Pending with its reason.
	dropped []taskCertificateSite
	// refusedIn: some Pending stays inside this span.
	refusedIn *handleCertificateSpan
	// coreCloneClear: the core clone declaration has no row at all.
	coreCloneClear bool
}

const taskCloneDeclaration = "@intrinsic pub fn clone(self: &Task<T>) -> Task<T>;"

func taskCertificateCases() []taskCertificateCase {
	return []taskCertificateCase{
		{handle: handleCertificateCase{name: "checkpoint_tick", clean: true, digest: "30cff77a8b99ff57192b72ca1bc6f057fd57e672c507f69b68eea4434d7de64d",
			summaries: []string{"tick"}, cleared: []handleCertificateSpan{{60, 72, "checkpoint()"}},
			text: "pragma module::dep;\nfn tick() -> Task<nothing> {\n    return checkpoint();\n}\n"}},
		{handle: handleCertificateCase{name: "sleep_nap", clean: true, digest: "f5d04bfe037a63f52f84a4805bcb616496aa91bf8f5f63d7b6d5eec91624528e",
			summaries: []string{"nap"}, cleared: []handleCertificateSpan{{67, 76, "sleep(ms)"}},
			text: "pragma module::dep;\nfn nap(ms: uint) -> Task<nothing> {\n    return sleep(ms);\n}\n"}},
		{handle: handleCertificateCase{name: "task_clone_int", clean: true, digest: "4e95adce54464f0b667ee298562ff59a08e01da2503548d06c5967b1b4f51c86",
			summaries: []string{"keep_int"}, cleared: []handleCertificateSpan{{73, 82, "t.clone()"}},
			text: "pragma module::dep;\nfn keep_int(t: &Task<int>) -> Task<int> {\n    return t.clone();\n}\n"}, coreCloneClear: true},
		// A payload the walk cannot classify keeps its refusal at the call. (A generic use would also
		// bring the core clone row back, but only a root program has finalized uses; a dependency has none.)
		{handle: handleCertificateCase{name: "task_clone_placement_control", digest: "2444026255ceb3273c9fa20b81746bc68f46df0a9042c76ce5ab08d25cc4f836",
			text: "pragma module::dep;\nfn keep_place(t: &Task<Placement>) -> Task<Placement> {\n    return t.clone();\n}\n"},
			kept: []taskCertificateSite{{handleCertificateSpan{87, 96, "t.clone()"}, genericConditionUnsupported}}},
		// Inside a generic helper the clone is certified, but its use keeps another refusal.
		{handle: handleCertificateCase{name: "task_clone_generic_control", digest: "f55e673bae8063941117f308dc1ab1bf993fa092d3ee00cb20e4c801ebda53bf",
			text: "pragma module::dep;\nfn wrap<T>(p: &Task<T>) -> Task<T> {\n    return p.clone();\n}\nfn use_wrap(t: &Task<int>) -> Task<int> {\n    return wrap::<int>(t);\n}\n"},
			refusedIn: &handleCertificateSpan{68, 77, "p.clone()"},
			dropped:   []taskCertificateSite{{handleCertificateSpan{68, 77, "p.clone()"}, genericConditionUnsupported}}, coreCloneClear: true},
		// A function value has no callee, so the certificate never reaches it.
		{handle: handleCertificateCase{name: "sleep_value_control", digest: "ea3942c8434d5062b3d39de8902af31d2e27e1afa37475a7374d242385526c68",
			text: "pragma module::dep;\nfn nap_value(ms: uint) -> Task<nothing> {\n    let f = sleep;\n    return f(ms);\n}\n"},
			refusedIn: &handleCertificateSpan{20, 100, "fn nap_value(ms: uint) -> Task<nothing> {\n    let f = sleep;\n    return f(ms);\n}"}},
	}
}

func TestAnalyzeTaskCertificate(t *testing.T) {
	for _, tc := range taskCertificateCases() {
		t.Run(tc.handle.name, func(t *testing.T) {
			f, analysis, local := analyzeHandleCertificate(t, "task_certificate", tc.handle, nil)
			checkHandleCertificate(t, tc.handle, f, analysis, local)
			checkTaskCertificate(t, tc, f, analysis, local)
		})
	}
}

func checkTaskCertificate(t *testing.T, tc taskCertificateCase, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis, local []sema.ReturnOriginPending) {
	t.Helper()
	sites := append(slices.Clone(tc.kept), tc.dropped...)
	spans := make([]handleCertificateSpan, 0, len(sites)+1)
	for _, site := range sites {
		spans = append(spans, site.span)
	}
	if tc.refusedIn != nil {
		spans = append(spans, *tc.refusedIn)
	}
	for _, span := range spans {
		if span.start < 0 || span.end > len(tc.handle.text) || span.start >= span.end || tc.handle.text[span.start:span.end] != span.snippet {
			t.Fatalf("PRECONDITION: frozen span %d:%d is not %q", span.start, span.end, span.snippet)
		}
	}
	at := func(site taskCertificateSite) bool {
		return slices.ContainsFunc(local, func(p sema.ReturnOriginPending) bool {
			return p.SourceKey == f.unit.SourceKey && int(p.Span.Start) == site.span.start && int(p.Span.End) == site.span.end && p.Reason == site.reason
		})
	}
	for _, site := range tc.kept {
		if !at(site) {
			t.Errorf("lost %q at %d:%d %q: %+v", site.reason, site.span.start, site.span.end, site.span.snippet, local)
		}
	}
	for _, site := range tc.dropped {
		if at(site) {
			t.Errorf("still %q at %d:%d %q", site.reason, site.span.start, site.span.end, site.span.snippet)
		}
	}
	if tc.refusedIn != nil && !slices.ContainsFunc(local, func(p sema.ReturnOriginPending) bool {
		return p.SourceKey == f.unit.SourceKey && int(p.Span.Start) >= tc.refusedIn.start && int(p.Span.End) <= tc.refusedIn.end
	}) {
		t.Errorf("lost every refusal inside %q: %+v", tc.refusedIn.snippet, local)
	}
	if !tc.coreCloneClear {
		return
	}
	if reasons := taskCoreRowReasons(t, f, analysis, taskCloneDeclaration, "clone"); len(reasons) != 0 {
		t.Errorf("core clone declaration keeps %v", reasons)
	}
}

// taskCoreRowReasons answers the Pending reasons at a core declaration's name, found by its unique text.
func taskCoreRowReasons(t *testing.T, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis, pattern, name string) []string {
	t.Helper()
	var core *sema.ReturnOriginUnit
	for i := range f.inputs.units {
		if f.inputs.units[i].SourceKey == "core/intrinsics.sg" {
			if core != nil {
				t.Fatal("PRECONDITION: core intrinsics unit is not unique")
			}
			core = &f.inputs.units[i]
		}
	}
	if core == nil {
		t.Fatal("PRECONDITION: core intrinsics unit is missing")
	}
	file := core.Builder.Files.Get(core.FileID).Span.File
	content := string(f.owner.FileSet.Get(file).Content)
	if strings.Count(content, pattern) != 1 || strings.Count(pattern, "fn "+name) != 1 {
		t.Fatalf("PRECONDITION: core declaration %q is not unique", pattern)
	}
	start := strings.Index(content, pattern) + strings.Index(pattern, "fn "+name) + len("fn ")
	var reasons []string
	for _, p := range analysis.Pending {
		if p.SourceKey == core.SourceKey && p.Span.File == file && int(p.Span.Start) == start && int(p.Span.End) == start+len(name) {
			reasons = append(reasons, p.Reason)
		}
	}
	return reasons
}

// The core task declarations lose their refusal in a program that clones no task.
func TestReturnOriginTaskCertificateCoreRows(t *testing.T) {
	f, analysis, _ := analyzeHandleCertificate(t, "task_certificate_core_rows", taskCertificateCases()[0].handle, nil)
	for _, row := range []struct{ pattern, name string }{
		{taskCloneDeclaration, "clone"},
		{"pub fn checkpoint() -> Task<nothing>;", "checkpoint"},
		{"@intrinsic pub fn sleep(", "sleep"},
	} {
		t.Run(row.name, func(t *testing.T) {
			if reasons := taskCoreRowReasons(t, f, analysis, row.pattern, row.name); len(reasons) != 0 {
				t.Errorf("core %s keeps %v", row.name, reasons)
			}
		})
	}
}

// Breaking only the module identity of one core task candidate keeps that call's refusal.
func TestReturnOriginTaskCertificateIdentityMutation(t *testing.T) {
	for _, row := range []struct {
		name, base, candidate string
		refused               handleCertificateSpan
	}{
		{"mutated_sleep", "sleep_nap", "sleep", handleCertificateSpan{67, 76, "sleep(ms)"}},
		{"mutated_task_clone", "task_clone_int", "clone", handleCertificateSpan{73, 82, "t.clone()"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			var tc handleCertificateCase
			for _, c := range taskCertificateCases() {
				if c.handle.name == row.base {
					tc = c.handle
				}
			}
			if tc.name == "" {
				t.Fatalf("PRECONDITION: no task certificate case %q", row.base)
			}
			tc.name, tc.clean, tc.cleared, tc.summaries, tc.refused = row.name, false, nil, nil, []handleCertificateSpan{row.refused}
			mutate := func(f originalGenericFixture) {
				matched := 0
				for i := range f.authority.CallableCandidates {
					candidate := &f.authority.CallableCandidates[i]
					if candidate.Name == row.candidate && candidate.SourceKey == "builtin" && candidate.ModulePath == "core/intrinsics" {
						candidate.ModulePath = "core/intrinsics_shadow"
						matched++
					}
				}
				if matched != 1 {
					t.Fatalf("PRECONDITION: core %s candidate is not unique: %d", row.candidate, matched)
				}
			}
			f, analysis, local := analyzeHandleCertificate(t, "task_certificate_mutation", tc, mutate)
			checkHandleCertificate(t, tc, f, analysis, local)
		})
	}
}
