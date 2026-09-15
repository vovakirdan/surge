package driver

import (
	"slices"
	"testing"

	"surge/internal/sema"
	"surge/internal/types"
)

// A core reader that returns a fresh array borrows nothing from its inputs, but
// only the exact core declaration is believed, and only the array itself: its
// element and the error beside it are still walked. Each mutation edits one
// candidate or descriptor of this fixture's own program before the analysis. The
// edit is shared by every use of that type in the fixture, including core's own
// declaration rows; every assertion below is local to the dependency source.

const containerBorrowedState = "opaque result type may carry borrowed state"

func containerCertificateCases() []handleCertificateCase {
	return []handleCertificateCase{
		{name: "dir_listing", clean: true, digest: "caeae80958c0d325ff64b550a53ff9803ea750d0e646fde75d8b39784932d72b", summaries: []string{"list_dir"},
			cleared: []handleCertificateSpan{{91, 111, "rt_fs_read_dir(path)"}},
			text:    "pragma module::dep;\nfn list_dir(path: &string) -> Erring<DirEntry[], FsError> {\n    return rt_fs_read_dir(path);\n}\n"},
		{name: "file_bytes", clean: true, digest: "b6b45c9ac336fb11755feabe9a8e0dd14c5763ed8d50f76a38a52593b2e275c4", summaries: []string{"read_all"},
			cleared: []handleCertificateSpan{{87, 108, "rt_fs_read_file(path)"}},
			text:    "pragma module::dep;\nfn read_all(path: &string) -> Erring<byte[], FsError> {\n    return rt_fs_read_file(path);\n}\n"},
		{name: "socket_bytes", clean: true, digest: "c2714319f7807e4995d69e44251e0f98801bf483e0f14f91dcb3c6e877e4ded3", summaries: []string{"read_some"},
			cleared: []handleCertificateSpan{{91, 116, "rt_net_read_bytes(c, cap)"}},
			text:    "pragma module::dep;\nfn read_some(c: &TcpConn, cap: uint) -> NetResult<byte[]> {\n    return rt_net_read_bytes(c, cap);\n}\n"},
		{name: "argv_strings", clean: true, digest: "69d8d3e58b2162e126a5755a4b9d684829518cfc7674b4f5472f36296e077752", summaries: []string{"argv_copy"},
			cleared: []handleCertificateSpan{{60, 69, "rt_argv()"}},
			text:    "pragma module::dep;\nfn argv_copy() -> string[] {\n    return rt_argv();\n}\n"},
		// A table name miss: the reader's signature and result, declared outside core.
		{name: "container_name_miss", digest: "a899788347ccb66f220ed38f290f5bd34137c1748a3ab8b268623562bbaf4881",
			refused: []handleCertificateSpan{{34, 43, "read_like"}, {154, 169, "read_like(path)"}},
			text:    "pragma module::dep;\n@intrinsic fn read_like(path: &string) -> Erring<byte[], FsError>;\nfn use_read(path: &string) -> Erring<byte[], FsError> {\n    return read_like(path);\n}\n"},
	}
}

func TestAnalyzeContainerCertificate(t *testing.T) {
	for _, tc := range containerCertificateCases() {
		t.Run(tc.name, func(t *testing.T) {
			f, analysis, local := analyzeHandleCertificate(t, "container_certificate", tc, nil)
			checkHandleCertificate(t, tc, f, analysis, local)
		})
	}
}

// containerCandidate is the unique retained core candidate of that name.
func containerCandidate(t *testing.T, f originalGenericFixture, name string) *sema.CallableCandidate {
	t.Helper()
	var found *sema.CallableCandidate
	for i := range f.authority.CallableCandidates {
		if c := &f.authority.CallableCandidates[i]; c.Name == name && c.SourceKey == "builtin" && c.ModulePath == "core/intrinsics" {
			if found != nil {
				t.Fatalf("PRECONDITION: core %s candidate is not unique", name)
			}
			found = c
		}
	}
	if found == nil {
		t.Fatalf("PRECONDITION: core %s candidate is missing", name)
	}
	return found
}

// containerResultMember is the one member of that kind in a core reader's two-member result.
func containerResultMember(t *testing.T, f originalGenericFixture, reader string, kind types.UnionMemberKind) *types.UnionMember {
	t.Helper()
	info, found := f.authority.TypeInterner.UnionInfo(containerCandidate(t, f, reader).ResultType)
	if !found || info == nil || len(info.Members) != 2 {
		t.Fatalf("PRECONDITION: %s result is not a two-member union", reader)
	}
	var member *types.UnionMember
	for i := range info.Members {
		if info.Members[i].Kind == kind {
			if member != nil {
				t.Fatalf("PRECONDITION: %s result repeats a member kind", reader)
			}
			member = &info.Members[i]
		}
	}
	if member == nil {
		t.Fatalf("PRECONDITION: %s result lacks the member kind", reader)
	}
	return member
}

// containerStruct is the core struct at id, checked by name, field count and first field.
func containerStruct(t *testing.T, f originalGenericFixture, id types.TypeID, name string, fields int, first string) *types.StructInfo {
	t.Helper()
	var core *sema.ReturnOriginUnit
	for i := range f.inputs.units {
		if f.inputs.units[i].SourceKey == "core/intrinsics.sg" {
			core = &f.inputs.units[i]
		}
	}
	info, found := f.authority.TypeInterner.StructInfo(id)
	if core == nil || !found || info == nil || len(info.Fields) != fields {
		t.Fatalf("PRECONDITION: %s is not a core struct with %d fields", name, fields)
	}
	lookup := core.Builder.StringsInterner.Lookup
	got, _ := lookup(info.Name)
	field, _ := lookup(info.Fields[0].Name)
	if got != name || field != first {
		t.Fatalf("PRECONDITION: struct %q with first field %q, want %s.%s", got, field, name, first)
	}
	return info
}

// A broken identity, or an element the table does not name, keeps the unsupported
// refusal at the call; a borrow in the element or in the error member is refuted by
// the ordinary walk instead.
func TestReturnOriginContainerCertificateMutations(t *testing.T) {
	for _, row := range []struct {
		name, base     string
		site           handleCertificateSpan
		present, other string
		mutate         func(t *testing.T, f originalGenericFixture)
	}{
		{"argv_identity_mutation", "argv_strings", handleCertificateSpan{60, 69, "rt_argv()"}, genericConditionUnsupported, "",
			func(t *testing.T, f originalGenericFixture) {
				containerCandidate(t, f, "rt_argv").ModulePath = "core/intrinsics_shadow"
			}},
		{"read_file_shape_mutation", "file_bytes", handleCertificateSpan{87, 108, "rt_fs_read_file(path)"}, genericConditionUnsupported, containerBorrowedState,
			func(t *testing.T, f originalGenericFixture) {
				argv := containerCandidate(t, f, "rt_argv").ResultType
				containerResultMember(t, f, "rt_fs_read_file", types.UnionMemberTag).TagArgs = []types.TypeID{argv}
			}},
		{"dir_entry_borrow_mutation", "dir_listing", handleCertificateSpan{91, 111, "rt_fs_read_dir(path)"}, containerBorrowedState, genericConditionUnsupported,
			func(t *testing.T, f originalGenericFixture) {
				tag := containerResultMember(t, f, "rt_fs_read_dir", types.UnionMemberTag)
				if len(tag.TagArgs) != 1 {
					t.Fatal("PRECONDITION: rt_fs_read_dir success does not hold one array")
				}
				array, found := f.authority.TypeInterner.StructInfo(tag.TagArgs[0])
				if !found || array == nil || len(array.TypeArgs) != 1 {
					t.Fatal("PRECONDITION: rt_fs_read_dir success is not an array of one element type")
				}
				entry := containerStruct(t, f, array.TypeArgs[0], "DirEntry", 3, "name")
				entry.Fields[0].Type = containerCandidate(t, f, "rt_fs_read_dir").ParamTypes[0]
			}},
		{"fs_error_borrow_mutation", "file_bytes", handleCertificateSpan{87, 108, "rt_fs_read_file(path)"}, containerBorrowedState, genericConditionUnsupported,
			func(t *testing.T, f originalGenericFixture) {
				failure := containerResultMember(t, f, "rt_fs_read_file", types.UnionMemberType)
				info := containerStruct(t, f, failure.Type, "FsError", 2, "message")
				info.Fields[0].Type = containerCandidate(t, f, "rt_fs_read_file").ParamTypes[0]
			}},
	} {
		t.Run(row.name, func(t *testing.T) {
			var tc handleCertificateCase
			for _, c := range containerCertificateCases() {
				if c.name == row.base {
					tc = c
				}
			}
			if tc.name == "" || tc.text[row.site.start:row.site.end] != row.site.snippet {
				t.Fatalf("PRECONDITION: no container case %q with site %q", row.base, row.site.snippet)
			}
			tc.name, tc.clean, tc.refused, tc.cleared, tc.summaries = row.name, false, nil, nil, nil
			f, _, local := analyzeHandleCertificate(t, "container_certificate_mutation", tc, func(f originalGenericFixture) { row.mutate(t, f) })
			present, other := false, false
			for _, pending := range local {
				if pending.SourceKey != f.unit.SourceKey || int(pending.Span.Start) != row.site.start || int(pending.Span.End) != row.site.end {
					continue
				}
				present = present || pending.Reason == row.present
				other = other || row.other != "" && pending.Reason == row.other
			}
			if !present || other {
				t.Errorf("%q at %d:%d: %q present=%v, %q present=%v, want true and false: %+v",
					row.site.snippet, row.site.start, row.site.end, row.present, present, row.other, other, local)
			}
		})
	}
	// An array behind a reference makes the result reference-bearing, so the call takes
	// the ordinary reference path before any certificate: it borrows its path argument.
	t.Run("read_file_reference_result", func(t *testing.T) {
		tc := containerCertificateCases()[1]
		if tc.name != "file_bytes" || !tc.clean {
			t.Fatal("PRECONDITION: the clean file_bytes case moved")
		}
		tc.name, tc.cleared, tc.summaries = "read_file_reference_result", nil, nil
		f, analysis, local := analyzeHandleCertificate(t, "container_certificate_reference_result", tc, func(f originalGenericFixture) {
			params := containerCandidate(t, f, "rt_net_write_bytes").ParamTypes
			if len(params) != 4 {
				t.Fatalf("PRECONDITION: rt_net_write_bytes has %d parameters", len(params))
			}
			if typ, ok := f.authority.TypeInterner.Lookup(params[1]); !ok || typ.Kind != types.KindReference {
				t.Fatal("PRECONDITION: rt_net_write_bytes data is not a reference")
			}
			containerResultMember(t, f, "rt_fs_read_file", types.UnionMemberTag).TagArgs = []types.TypeID{params[1]}
		})
		checkHandleCertificate(t, tc, f, analysis, local)
		if s := requireReturnOriginSummary(t, analysis, "read_all"); s.Unknown || s.NoNormalReturn || !slices.Equal(s.ParamSlots, []uint32{0}) {
			t.Errorf("read_all summary = %+v, want a normal result borrowing only its path argument", s)
		}
	})
}
