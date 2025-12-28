package project

import "testing"

func TestResolveImportPath(t *testing.T) {
	tests := []struct {
		name       string
		modulePath string
		basePath   string
		segments   []string
		want       string
		wantErr    bool
	}{
		{
			name:       "simple",
			modulePath: "core/main",
			basePath:   "",
			segments:   []string{"std", "io"},
			want:       "core/std/io",
		},
		{
			name:       "relative same dir",
			modulePath: "core/main",
			basePath:   "",
			segments:   []string{".", "util"},
			want:       "core/util",
		},
		{
			name:       "relative parent",
			modulePath: "included/d",
			basePath:   "",
			segments:   []string{"..", "a"},
			want:       "a",
		},
		{
			name:       "multiple parent",
			modulePath: "a/b/c",
			basePath:   "",
			segments:   []string{"..", "..", "d"},
			want:       "d",
		},
		{
			name:       "escape root",
			modulePath: "a",
			basePath:   "",
			segments:   []string{"..", "b"},
			wantErr:    true,
		},
		{
			name:       "sibling module",
			modulePath: "examples/imports/a",
			basePath:   "",
			segments:   []string{"b"},
			want:       "examples/imports/b",
		},
		{
			name:       "absolute from base",
			modulePath: "included/d",
			basePath:   "examples/imports",
			segments:   []string{"examples", "imports", "a"},
			want:       "examples/imports/a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveImportPath(tt.modulePath, tt.basePath, tt.segments)
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
