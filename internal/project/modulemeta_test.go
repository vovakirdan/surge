package project

import "testing"

func TestResolveImportPath(t *testing.T) {
	tests := []struct {
		name       string
		modulePath string
		segments   []string
		want       string
		wantErr    bool
	}{
		{
			name:       "simple",
			modulePath: "core/main",
			segments:   []string{"std", "io"},
			want:       "std/io",
		},
		{
			name:       "relative same dir",
			modulePath: "core/main",
			segments:   []string{".", "util"},
			want:       "core/util",
		},
		{
			name:       "relative parent",
			modulePath: "included/d",
			segments:   []string{"..", "a"},
			want:       "a",
		},
		{
			name:       "multiple parent",
			modulePath: "a/b/c",
			segments:   []string{"..", "..", "d"},
			want:       "d",
		},
		{
			name:       "escape root",
			modulePath: "a",
			segments:   []string{"..", "b"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveImportPath(tt.modulePath, tt.segments)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveImportPath returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ResolveImportPath = %q, want %q", got, tt.want)
			}
		})
	}
}
