package driver

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"surge/internal/sema"
)

// A core constructor that returns only a fresh runtime handle word borrows
// nothing from its inputs, but only the exact core declaration is believed. The
// same signature declared in a dependency, the same name in a no_std module and
// the core declaration with a broken module identity all keep their refusal.
// Only the dependency module varies; every span is frozen with its text.
type handleCertificateSpan struct {
	start, end int
	snippet    string
}

type handleCertificateCase struct {
	name, text, digest string
	// clean: no Pending may remain anywhere in the dependency source.
	clean bool
	// refused: an unsupported refusal stays at exactly each of these spans.
	refused []handleCertificateSpan
	// cleared: no Pending of any reason remains at exactly each of these spans.
	cleared []handleCertificateSpan
	// summaries: these bodies must return normally with no sources.
	summaries []string
}

func handleCertificateCases() []handleCertificateCase {
	return []handleCertificateCase{
		{name: "fs_open", clean: true, digest: "9c4bc7d72adb9163ed4c2cc8ee35af9548b065bb456ca1178698f79a3b5f3473", summaries: []string{"open_with"},
			cleared: []handleCertificateSpan{{106, 129, "rt_fs_open(path, flags)"}},
			text:    "pragma module::dep;\nfn open_with(path: &string, flags: FsOpenFlags) -> Erring<File, FsError> {\n    return rt_fs_open(path, flags);\n}\n"},
		{name: "net_handles", clean: true, digest: "e42a0a37dccb32559dcddc3630b8cc197a3ac09171eaee422144ea33cc82a33c", summaries: []string{"listen_on", "connect_to", "accept_from"},
			cleared: []handleCertificateSpan{{99, 124, "rt_net_listen(addr, port)"}, {204, 230, "rt_net_connect(addr, port)"}, {301, 317, "rt_net_accept(l)"}},
			text:    "pragma module::dep;\nfn listen_on(addr: &string, port: uint) -> NetResult<TcpListener> {\n    return rt_net_listen(addr, port);\n}\nfn connect_to(addr: &string, port: uint) -> NetResult<TcpConn> {\n    return rt_net_connect(addr, port);\n}\nfn accept_from(l: &TcpListener) -> NetResult<TcpConn> {\n    return rt_net_accept(l);\n}\n"},
		{name: "shard_placement", clean: true, digest: "cb55ab61c46a111f0c7205b90280ce63633d54acace5a246e7fc26a28d143a4b", summaries: []string{"place_on"},
			cleared: []handleCertificateSpan{{71, 80, "shard(id)"}},
			text:    "pragma module::dep;\nfn place_on(id: ShardId) -> Placement {\n    return shard(id);\n}\n"},
		{name: "rwlock_new", clean: true, digest: "1cc56cdb00876afdb6114c664ad2abb599dca926d062ae5708c923ad2f974b68", summaries: []string{"make_lock"},
			cleared: []handleCertificateSpan{{58, 70, "RwLock.new()"}},
			text:    "pragma module::dep;\nfn make_lock() -> RwLock {\n    return RwLock.new();\n}\n"},
		// A table name miss: no signature or shape rule certifies an opaque handle result.
		{name: "no_signature_rule_fs", digest: "3d988d17c91e31749c0ded07cadcf6d883a5805bbdf615b25390ada5bef3dae0",
			refused: []handleCertificateSpan{{34, 43, "open_like"}, {190, 212, "open_like(path, flags)"}},
			text:    "pragma module::dep;\n@intrinsic fn open_like(path: &string, flags: FsOpenFlags) -> Erring<File, FsError>;\nfn use_open(path: &string, flags: FsOpenFlags) -> Erring<File, FsError> {\n    return open_like(path, flags);\n}\n"},
		// The core name, parameters and result shape, declared outside core.
		{name: "name_control_shard", digest: "3d523715e9c7342141b64a9afae0c755a912d6fdd2b380009253ae7312d61ae8",
			refused: []handleCertificateSpan{{111, 116, "shard"}, {195, 204, "shard(id)"}},
			text:    "pragma module::dep, no_std;\n@nosend type Placement = { __opaque: int64 };\ntype ShardId = uint32;\n@intrinsic fn shard(id: ShardId) -> Placement;\nfn place_on(id: ShardId) -> Placement {\n    return shard(id);\n}\n"},
		{name: "shape_control_task", digest: "9949e9d037b497eabc79be3f4542771f722253656711e839d610bb299fc65bd7",
			refused: []handleCertificateSpan{{34, 43, "tick_like"}, {108, 119, "tick_like()"}},
			text:    "pragma module::dep;\n@intrinsic fn tick_like() -> Task<nothing>;\nfn use_tick() -> Task<nothing> {\n    return tick_like();\n}\n"},
	}
}

func handleCertificateCaseNamed(t *testing.T, name string) handleCertificateCase {
	t.Helper()
	for _, tc := range handleCertificateCases() {
		if tc.name == name {
			return tc
		}
	}
	t.Fatalf("PRECONDITION: no handle certificate case %q", name)
	return handleCertificateCase{}
}

func analyzeHandleCertificate(t *testing.T, stage string, tc handleCertificateCase, mutate func(originalGenericFixture)) (originalGenericFixture, *sema.ReturnOriginAnalysis, []sema.ReturnOriginPending) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.text))); got != tc.digest {
		t.Fatalf("PRECONDITION: frozen dependency source changed: %s", got)
	}
	for _, spans := range [][]handleCertificateSpan{tc.refused, tc.cleared} {
		for _, span := range spans {
			if span.start < 0 || span.end > len(tc.text) || span.start >= span.end || tc.text[span.start:span.end] != span.snippet {
				t.Fatalf("PRECONDITION: frozen span %d:%d is not %q", span.start, span.end, span.snippet)
			}
		}
	}
	f := originalGenericSignatureFixture(t, tc.text, false, true)
	if mutate != nil {
		mutate(f)
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), f.authority, f.inputs.units)
	if err != nil || analysis == nil {
		t.Fatalf("return-origin analysis did not run: %v", err)
	}
	var local []sema.ReturnOriginPending
	for _, pending := range analysis.Pending {
		if pending.SourceKey == f.unit.SourceKey || pending.Span.File == f.owner.File.ID {
			local = append(local, pending)
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": stage, "case": tc.name,
		"source_key": f.unit.SourceKey, "pending": local, "diagnostics": analysis.Diagnostics})
	for _, d := range analysis.Diagnostics {
		if d.Primary.File == f.owner.File.ID {
			t.Errorf("unexpected dependency diagnostic: %+v", d)
		}
	}
	return f, analysis, local
}

func checkHandleCertificate(t *testing.T, tc handleCertificateCase, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis, local []sema.ReturnOriginPending) {
	t.Helper()
	if tc.clean {
		for _, pending := range local {
			t.Errorf("dependency obligation left unfinished: %s at %d:%d", pending.Reason, pending.Span.Start, pending.Span.End)
		}
	}
	at := func(pending sema.ReturnOriginPending, span handleCertificateSpan) bool {
		return pending.SourceKey == f.unit.SourceKey && int(pending.Span.Start) == span.start && int(pending.Span.End) == span.end
	}
	for _, span := range tc.refused {
		found := false
		for _, pending := range local {
			found = found || at(pending, span) && pending.Reason == genericConditionUnsupported
		}
		if !found {
			t.Errorf("lost the unsupported refusal at %d:%d %q: %+v", span.start, span.end, span.snippet, local)
		}
	}
	for _, span := range tc.cleared {
		for _, pending := range local {
			if at(pending, span) {
				t.Errorf("certified call %q at %d:%d still has: %s", span.snippet, span.start, span.end, pending.Reason)
			}
		}
	}
	for _, name := range tc.summaries {
		if s := requireReturnOriginSummary(t, analysis, name); s.NoNormalReturn || s.Unknown || len(s.ParamSlots) != 0 {
			t.Errorf("%s summary = %+v, want a normal result with no sources", name, s)
		}
	}
}

func TestAnalyzeHandleCertificate(t *testing.T) {
	for _, tc := range handleCertificateCases() {
		t.Run(tc.name, func(t *testing.T) {
			f, analysis, local := analyzeHandleCertificate(t, "handle_certificate", tc, nil)
			checkHandleCertificate(t, tc, f, analysis, local)
		})
	}
}

// The core declarations themselves: certified constructors and readers lose their
// refusal, and core intrinsics of neighbouring shapes keep it. Task results keep
// theirs until the task check owns a clone's borrowed spawn arguments. Spans come
// from each unique declaration text, not from frozen offsets into core.
func TestReturnOriginHandleCertificateCoreRows(t *testing.T) {
	f, analysis, _ := analyzeHandleCertificate(t, "handle_certificate_core_rows", handleCertificateCaseNamed(t, "shard_placement"), nil)
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
	rows := []struct {
		pattern, name string
		certified     bool
	}{
		{"@intrinsic fn rt_fs_open(", "rt_fs_open", true},
		{"@intrinsic fn rt_net_listen(", "rt_net_listen", true},
		{"@intrinsic fn rt_net_connect(", "rt_net_connect", true},
		{"@intrinsic fn rt_net_accept(", "rt_net_accept", true},
		{"@intrinsic pub fn shard(", "shard", true},
		{"@intrinsic pub fn new() -> RwLock;", "new", true},
		{"@intrinsic pub fn channel_on<", "channel_on", true},
		{"@intrinsic fn rt_fs_read_dir(", "rt_fs_read_dir", true},
		{"@intrinsic fn rt_fs_read_file(", "rt_fs_read_file", true},
		{"@intrinsic fn rt_net_read_bytes(", "rt_net_read_bytes", true},
		{"pub fn rt_argv() -> string[];", "rt_argv", true},
		{"@intrinsic fn rt_map_keys<", "rt_map_keys", true},
		// Array concatenation carries element contents; the container-content analysis flips it.
		{"@intrinsic fn __add(self: &Array<T>, other: &Array<T>) -> Array<T>;", "__add", false},
		{"@intrinsic pub fn clone(self: &Task<T>) -> Task<T>;", "clone", false},
		{"pub fn checkpoint() -> Task<nothing>;", "checkpoint", false},
		{"@intrinsic pub fn sleep(", "sleep", false},
	}
	observed := make(map[string]any, len(rows))
	for _, row := range rows {
		if strings.Count(content, row.pattern) != 1 || strings.Count(row.pattern, "fn "+row.name) != 1 {
			t.Fatalf("PRECONDITION: core declaration %q is not unique", row.pattern)
		}
		start := strings.Index(content, row.pattern) + strings.Index(row.pattern, "fn "+row.name) + len("fn ")
		end := start + len(row.name)
		refused := false
		for _, pending := range analysis.Pending {
			refused = refused || pending.SourceKey == core.SourceKey && pending.Span.File == file &&
				int(pending.Span.Start) == start && int(pending.Span.End) == end && pending.Reason == genericConditionUnsupported
		}
		observed[row.name] = map[string]any{"start": start, "end": end, "refused": refused}
		if refused == row.certified {
			t.Errorf("core %s at %d:%d: unsupported refusal present=%v, want %v", row.name, start, end, refused, !row.certified)
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": "handle_certificate_core_rows_observed", "rows": observed})
}

// Breaking only the module identity of one core candidate keeps that call's
// refusal; on a wrapped result the sibling constructors in the same file stay certified.
func TestReturnOriginHandleCertificateIdentityMutation(t *testing.T) {
	for _, row := range []struct {
		name, base, candidate string
		refused, cleared      []handleCertificateSpan
	}{
		{"mutated_shard", "shard_placement", "shard", []handleCertificateSpan{{71, 80, "shard(id)"}}, nil},
		{"mutated_net_listen", "net_handles", "rt_net_listen", []handleCertificateSpan{{99, 124, "rt_net_listen(addr, port)"}},
			[]handleCertificateSpan{{204, 230, "rt_net_connect(addr, port)"}, {301, 317, "rt_net_accept(l)"}}},
	} {
		t.Run(row.name, func(t *testing.T) {
			tc := handleCertificateCaseNamed(t, row.base)
			tc.name, tc.clean, tc.refused, tc.cleared, tc.summaries = row.name, false, row.refused, row.cleared, nil
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
			f, analysis, local := analyzeHandleCertificate(t, "handle_certificate_mutation", tc, mutate)
			checkHandleCertificate(t, tc, f, analysis, local)
		})
	}
}
