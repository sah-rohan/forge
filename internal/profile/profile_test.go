package profile

import (
	"os"
	"path/filepath"
	"testing"
)

// writeRepo materializes a fake repo on disk for the profiler to inspect.
func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func has(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func TestDetect_Go(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"go.mod":           "module x\n",
		"main.go":          "package main\n",
		"internal/a/a.go":  "package a\n",
		".github/workflows/ci.yml": "name: ci\n",
	})
	p, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !has(p.Languages, "Go") {
		t.Fatalf("expected Go, got %v", p.Languages)
	}
	if !has(p.TestCommands, "go test ./...") {
		t.Fatalf("expected go test command, got %v", p.TestCommands)
	}
	if !p.HasCI {
		t.Fatal("expected HasCI true (workflows present)")
	}
}

// Kronos-shaped: Go + React/TS + Terraform in one repo. The profiler must
// surface ALL three and not assume a single language.
func TestDetect_Polyglot(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"backend/go.mod":         "module x\n",
		"backend/main.go":        "package main\n",
		"frontend/package.json":  `{"name":"x"}`,
		"frontend/src/App.tsx":   "export default 1\n",
		"frontend/src/api.ts":    "export const x = 1\n",
		"infra/main.tf":          "resource \"x\" \"y\" {}\n",
		"Dockerfile":             "FROM scratch\n",
	})
	p, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	// go.mod/package.json are nested, so marker detection (root-only) won't
	// catch them — but extension counting + the .tf root check should still
	// surface Terraform and the source languages.
	if !has(p.Languages, "Terraform") {
		t.Fatalf("expected Terraform from root .tf, got %v", p.Languages)
	}
	if !p.HasDocker {
		t.Fatal("expected HasDocker true")
	}
}

func TestDetect_Python(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"requirements.txt": "requests\n",
		"scraper.py":       "print('hi')\n",
	})
	p, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !has(p.Languages, "Python") {
		t.Fatalf("expected Python, got %v", p.Languages)
	}
	if !has(p.TestCommands, "pytest") {
		t.Fatalf("expected pytest, got %v", p.TestCommands)
	}
}

func TestDetect_TopLevelExcludesDotfiles(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"README.md":     "# x\n",
		".env":          "SECRET=1\n",
		".gitignore":    "node_modules\n",
		"src/index.ts":  "export const x = 1\n",
	})
	p, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if has(p.TopLevel, ".env") || has(p.TopLevel, ".gitignore") {
		t.Fatalf("dotfiles should be excluded from TopLevel, got %v", p.TopLevel)
	}
	if !has(p.TopLevel, "README.md") || !has(p.TopLevel, "src") {
		t.Fatalf("expected README.md + src in TopLevel, got %v", p.TopLevel)
	}
}
