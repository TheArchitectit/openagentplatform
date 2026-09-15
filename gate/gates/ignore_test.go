package gates

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIgnorePatternsSkipsCommentsAndBlanks(t *testing.T) {
	root := t.TempDir()
	body := "# a comment\n\nvendor/\n*.pb.go\n   \n"
	if err := os.WriteFile(filepath.Join(root, ".guardrailsignore"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	patterns, err := LoadIgnorePatterns(root)
	if err != nil {
		t.Fatalf("LoadIgnorePatterns: %v", err)
	}
	if len(patterns) != 2 || patterns[0] != "vendor/" || patterns[1] != "*.pb.go" {
		t.Fatalf("patterns = %#v", patterns)
	}
}

func TestLoadIgnorePatternsMissingFileIsNotAnError(t *testing.T) {
	patterns, err := LoadIgnorePatterns(t.TempDir())
	if err != nil {
		t.Fatalf("LoadIgnorePatterns: %v", err)
	}
	if len(patterns) != 0 {
		t.Fatalf("patterns = %#v, want none", patterns)
	}
}

func TestIgnoredPath(t *testing.T) {
	patterns := []string{"vendor/", "*.pb.go", "web/src/routeTree.gen.ts"}
	cases := map[string]bool{
		"vendor/lib/thing.go":         true,  // directory prefix
		"internal/api/foo.pb.go":      true,  // bare-extension glob reaches nested files
		"web/src/routeTree.gen.ts":    true,  // exact relative match
		"internal/api/foo.go":         false, // ordinary source
		"web/src/routes/dashboard.ts": false,
	}
	for rel, want := range cases {
		if got := IgnoredPath(patterns, rel); got != want {
			t.Errorf("IgnoredPath(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestGlobMatchSpansSeparators(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"*.go", "internal/api/cloud.go", true},  // '*' crosses '/'
		{"*.go", "web/src/app.tsx", false},
		{"web/src/**/*.tsx", "web/src/routes/a.tsx", true},
		{"web/src/**/*.tsx", "web/src/a.tsx", true},  // '**/' matches zero dirs
		{"web/src/**/*.tsx", "other/a.tsx", false},
		{"Dockerfile", "Dockerfile", true},
		{"Dockerfile.*", "Dockerfile.prod", true},
	}
	for _, c := range cases {
		if got := globMatch(c.glob, c.path); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.glob, c.path, got, c.want)
		}
	}
}

func TestIsTestRelPath(t *testing.T) {
	cases := map[string]bool{
		"internal/api/cloud_test.go":       true,
		"tests/helpers.py":                 true,
		"pkg/test/thing.go":                true,
		"web/src/app.test.tsx":             true,
		"internal/api/cloud.go":            false,
		"web/src/contest.tsx":              false, // "conftest" prefix must not overreach
	}
	for rel, want := range cases {
		if got := isTestRelPath(rel); got != want {
			t.Errorf("isTestRelPath(%q) = %v, want %v", rel, got, want)
		}
	}
}