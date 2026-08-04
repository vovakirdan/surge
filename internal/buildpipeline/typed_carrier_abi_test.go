package buildpipeline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/abimanifest"
)

func TestTypedCarrierMissingSentinelCollapsesOnlyDefaultLinkNoise(t *testing.T) {
	linkNoise := "/usr/bin/ld: out.o: undefined reference to `" + abimanifest.GeneratedSentinelSymbol + "'\nclang: error: linker command failed"
	linkErr := &commandError{name: "clang", stderr: linkNoise, cause: errors.New("exit status 1")}
	if !isMissingTypedCarrierSentinel(linkErr) {
		t.Fatal("exact missing typed-carrier sentinel was not classified")
	}
	err := (&typedCarrierABIMismatchError{
		expectedHash: abimanifest.GeneratedManifestHash,
		actualHash:   strings.Repeat("a", 64),
	}).Error()
	if err != typedCarrierABIRebuildMessage || strings.Contains(err, "undefined reference") || strings.Contains(err, abimanifest.GeneratedManifestHash) {
		t.Fatalf("default diagnostic leaked linker/debug detail: %q", err)
	}
	if strings.Contains(err, "\n") {
		t.Fatalf("default diagnostic must be exactly one line: %q", err)
	}
}

func TestTypedCarrierDebugDiagnosticShowsEvidenceWithoutNegotiation(t *testing.T) {
	actual := strings.Repeat("b", 64)
	err := (&typedCarrierABIMismatchError{
		expectedHash: abimanifest.GeneratedManifestHash,
		actualHash:   actual,
		debug:        true,
	}).Error()
	for _, required := range []string{typedCarrierABIRebuildMessage, "expected=" + abimanifest.GeneratedManifestHash, "actual=" + actual, "missing_symbol=" + abimanifest.GeneratedSentinelSymbol} {
		if !strings.Contains(err, required) {
			t.Fatalf("debug evidence missing %q: %q", required, err)
		}
	}
	for _, forbidden := range []string{"fallback", "selecting", "negotiat"} {
		if strings.Contains(strings.ToLower(err), forbidden) {
			t.Fatalf("debug evidence suggests alternate acceptance: %q", err)
		}
	}
	absent := (&typedCarrierABIMismatchError{expectedHash: abimanifest.GeneratedManifestHash, debug: true}).Error()
	if !strings.Contains(absent, "actual=absent") {
		t.Fatalf("absent runtime identity not explicit: %q", absent)
	}
}

func TestTypedCarrierClassifierDoesNotMaskOtherLinkFailures(t *testing.T) {
	tests := map[string]string{
		"unrelated undefined": "/usr/bin/ld: undefined reference to `other_symbol'",
		"sentinel suffix":     "/usr/bin/ld: undefined reference to `" + abimanifest.GeneratedSentinelSymbol + "_extra'",
		"sentinel prefix":     "/usr/bin/ld: undefined reference to `extra_" + abimanifest.GeneratedSentinelSymbol + "'",
		"non-link mention":    "compiler note: " + abimanifest.GeneratedSentinelSymbol,
		"different hash":      "/usr/bin/ld: undefined reference to `" + abimanifest.SentinelPrefix + strings.Repeat("0", 64) + "'",
	}
	for name, stderr := range tests {
		t.Run(name, func(t *testing.T) {
			err := &commandError{name: "clang", stderr: stderr, cause: errors.New("exit status 1")}
			if isMissingTypedCarrierSentinel(err) {
				t.Fatalf("unrelated linker failure was masked: %q", stderr)
			}
			if err.Error() == typedCarrierABIRebuildMessage || !strings.Contains(err.Error(), stderr) {
				t.Fatalf("original linker evidence was not preserved: %q", err.Error())
			}
		})
	}
}

func TestDiscoverRuntimeABIHashIsStrictDebugEvidence(t *testing.T) {
	dir := t.TempDir()
	header := filepath.Join(dir, "rt_typed_carrier_abi.generated.h")
	valid := "#define SURGE_TYPED_CARRIER_ABI_MANIFEST_HASH \"" + abimanifest.GeneratedManifestHash + "\"\n"
	if err := os.WriteFile(header, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := discoverRuntimeABIHash(dir); got != abimanifest.GeneratedManifestHash {
		t.Fatalf("discovered hash = %q", got)
	}
	for _, invalid := range []string{
		"#define SURGE_TYPED_CARRIER_ABI_MANIFEST_HASH \"short\"\n",
		"#define SURGE_TYPED_CARRIER_ABI_MANIFEST_HASH \"" + strings.Repeat("G", 64) + "\"\n",
		"#define OTHER \"" + abimanifest.GeneratedManifestHash + "\"\n",
	} {
		if err := os.WriteFile(header, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := discoverRuntimeABIHash(dir); got != "" {
			t.Fatalf("invalid debug identity accepted: %q", got)
		}
	}
}
