package version

import (
	"testing"
)

func TestVersion_DefaultValues(t *testing.T) {
	// Test that default values are set
	if Version == "" {
		t.Error("Version should have a default value")
	}
	
	// GitCommit and BuildDate can be empty (optional)
	// Just verify they exist as variables
	_ = GitCommit
	_ = BuildDate
}

func TestVersion_CanBeOverridden(t *testing.T) {
	// Save original values
	origVersion := Version
	origGitCommit := GitCommit
	origBuildDate := BuildDate
	
	// Override values (simulating build-time ldflags)
	Version = "1.2.3"
	GitCommit = "abc123def456"
	BuildDate = "2024-01-15T10:30:00Z"
	
	// Verify overrides worked
	if Version != "1.2.3" {
		t.Errorf("Version = %q, want %q", Version, "1.2.3")
	}
	if GitCommit != "abc123def456" {
		t.Errorf("GitCommit = %q, want %q", GitCommit, "abc123def456")
	}
	if BuildDate != "2024-01-15T10:30:00Z" {
		t.Errorf("BuildDate = %q, want %q", BuildDate, "2024-01-15T10:30:00Z")
	}
	
	// Restore original values
	Version = origVersion
	GitCommit = origGitCommit
	BuildDate = origBuildDate
}

func TestVersion_EmptyOptionalFields(t *testing.T) {
	// Save original values
	origGitCommit := GitCommit
	origBuildDate := BuildDate
	
	// Set to empty
	GitCommit = ""
	BuildDate = ""
	
	// Verify they can be empty
	if GitCommit != "" {
		t.Errorf("GitCommit should be empty, got %q", GitCommit)
	}
	if BuildDate != "" {
		t.Errorf("BuildDate should be empty, got %q", BuildDate)
	}
	
	// Restore
	GitCommit = origGitCommit
	BuildDate = origBuildDate
}

func TestVersion_SemanticVersionFormat(t *testing.T) {
	// Save original
	origVersion := Version
	
	// Test various semantic version formats
	validVersions := []string{
		"0.1.0",
		"1.0.0",
		"1.2.3",
		"2.0.0-alpha",
		"1.0.0-beta.1",
		"0.1.0-dev",
		"1.2.3-rc.1+build.123",
	}
	
	for _, v := range validVersions {
		Version = v
		if Version != v {
			t.Errorf("Failed to set version to %q, got %q", v, Version)
		}
	}
	
	// Restore
	Version = origVersion
}

func TestVersion_GitCommitFormat(t *testing.T) {
	// Save original
	origGitCommit := GitCommit
	
	// Test various git commit formats
	validCommits := []string{
		"abc123",
		"a1b2c3d4e5f6",
		"1234567890abcdef1234567890abcdef12345678",
		"short",
		"",
	}
	
	for _, c := range validCommits {
		GitCommit = c
		if GitCommit != c {
			t.Errorf("Failed to set GitCommit to %q, got %q", c, GitCommit)
		}
	}
	
	// Restore
	GitCommit = origGitCommit
}

func TestVersion_BuildDateFormat(t *testing.T) {
	// Save original
	origBuildDate := BuildDate
	
	// Test various build date formats (ISO-8601 and others)
	validDates := []string{
		"2024-01-15",
		"2024-01-15T10:30:00Z",
		"2024-01-15T10:30:00+00:00",
		"2024-01-15T10:30:00.123Z",
		"20240115",
		"",
	}
	
	for _, d := range validDates {
		BuildDate = d
		if BuildDate != d {
			t.Errorf("Failed to set BuildDate to %q, got %q", d, BuildDate)
		}
	}
	
	// Restore
	BuildDate = origBuildDate
}

// BenchmarkVersionAccess benchmarks accessing version variables
func BenchmarkVersionAccess(b *testing.B) {
	b.Run("Version", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = Version
		}
	})
	
	b.Run("GitCommit", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = GitCommit
		}
	})
	
	b.Run("BuildDate", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = BuildDate
		}
	})
}