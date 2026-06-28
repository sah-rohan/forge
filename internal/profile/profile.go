// Package profile performs zero-assumption discovery of a cloned repository.
// This is the heart of Forge's versatility: nothing about a target repo is
// hardcoded. Languages, package managers, test commands, and structure are all
// inferred at runtime from what's actually on disk, then merged with any
// explicit overrides from the repo's .github/agent.yml. The same engine
// therefore profiles a Go+React+Terraform repo (Kronos) and a Python scraper
// identically — it just emits a different Profile.
package profile

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Profile is the discovered "shape" of a repository — the data every later
// phase (context retrieval, the agent loop, the verifier) consumes instead of
// assuming anything.
type Profile struct {
	Languages    []string `json:"languages"`     // ranked by file count
	PackageMgrs  []string `json:"package_mgrs"`  // npm, go, pip, cargo, …
	TestCommands []string `json:"test_commands"` // inferred unless config overrides
	HasDocker    bool     `json:"has_docker"`
	HasCI        bool     `json:"has_ci"`
	TopLevel     []string `json:"top_level"` // top-level dirs/files (for the intro comment)
}

// markerFile maps a sentinel file to (language, package-manager, test-command).
// The first match per category wins; multiple can apply (a polyglot repo like
// Kronos hits both go.mod and package.json).
type marker struct {
	file    string
	lang    string
	pkgMgr  string
	testCmd string
}

var markers = []marker{
	{"go.mod", "Go", "go", "go test ./..."},
	{"package.json", "JavaScript/TypeScript", "npm", "npm test"},
	{"requirements.txt", "Python", "pip", "pytest"},
	{"pyproject.toml", "Python", "pip", "pytest"},
	{"Cargo.toml", "Rust", "cargo", "cargo test"},
	{"pom.xml", "Java", "maven", "mvn test"},
	{"build.gradle", "Java/Kotlin", "gradle", "gradle test"},
	{"Gemfile", "Ruby", "bundler", "bundle exec rspec"},
}

// Detect walks the repo root (shallow — top level + one level deep for
// markers) and produces a Profile. It deliberately does NOT read file contents
// beyond marker filenames; deep semantic analysis is the job of the later
// context-engine phase, not profiling.
func Detect(root string) (*Profile, error) {
	p := &Profile{}

	langCount := map[string]int{}
	seenPkg := map[string]bool{}
	seenTest := map[string]bool{}

	// Top-level listing for the human-facing intro comment + marker detection.
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && name != ".github" {
			continue
		}
		p.TopLevel = append(p.TopLevel, name)
		if name == ".github" {
			p.HasCI = dirHasWorkflows(filepath.Join(root, ".github"))
		}
		if strings.EqualFold(name, "Dockerfile") {
			p.HasDocker = true
		}
	}
	sort.Strings(p.TopLevel)

	// Marker files: find each known sentinel ANYWHERE in the tree (bounded
	// walk). Polyglot/monorepo layouts (Kronos: backend/go.mod,
	// frontend/package.json) put markers in subdirectories, so a root-only
	// check would miss the test commands.
	markerByName := map[string]marker{}
	for _, m := range markers {
		markerByName[m.file] = m
	}
	for _, m := range findMarkers(root, markerByName) {
		if !contains(p.Languages, m.lang) {
			p.Languages = append(p.Languages, m.lang)
		}
		if !seenPkg[m.pkgMgr] {
			p.PackageMgrs = append(p.PackageMgrs, m.pkgMgr)
			seenPkg[m.pkgMgr] = true
		}
		if !seenTest[m.testCmd] {
			p.TestCommands = append(p.TestCommands, m.testCmd)
			seenTest[m.testCmd] = true
		}
	}

	// Always count source files across the tree. This catches languages whose
	// marker files are nested (Kronos: backend/go.mod, frontend/package.json)
	// and extension-only languages like Terraform (infra/main.tf) that have no
	// root marker. Any language with source files is surfaced.
	countSourceFiles(root, langCount)
	for lang, n := range langCount {
		if n > 0 && !contains(p.Languages, lang) {
			p.Languages = append(p.Languages, lang)
		}
	}

	// Rank languages by source-file count for a stable "primary language".
	sort.SliceStable(p.Languages, func(i, j int) bool {
		return langCount[p.Languages[i]] > langCount[p.Languages[j]]
	})

	return p, nil
}

// ---- helpers ----

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirHasWorkflows(githubDir string) bool {
	wf := filepath.Join(githubDir, "workflows")
	info, err := os.Stat(wf)
	return err == nil && info.IsDir()
}

// findMarkers does a bounded walk and returns the markers whose sentinel files
// appear anywhere in the tree (skipping vendored/build dirs). Order follows the
// `markers` slice so the primary test command is deterministic.
func findMarkers(root string, byName map[string]marker) []marker {
	hit := map[string]bool{}
	seen := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || seen > 20000 {
			return filepath.SkipDir
		}
		if d.IsDir() {
			n := d.Name()
			if n == "node_modules" || n == ".git" || n == "vendor" || n == "dist" || n == ".terraform" {
				return filepath.SkipDir
			}
			return nil
		}
		seen++
		if _, ok := byName[d.Name()]; ok {
			hit[d.Name()] = true
		}
		return nil
	})
	out := make([]marker, 0, len(hit))
	for _, m := range markers { // stable order
		if hit[m.file] {
			out = append(out, m)
		}
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// countSourceFiles does a bounded walk to rank languages. Bounded so a giant
// repo doesn't stall the profiler — we only need a rough ranking.
func countSourceFiles(root string, counts map[string]int) {
	extLang := map[string]string{
		".go": "Go", ".ts": "JavaScript/TypeScript", ".tsx": "JavaScript/TypeScript",
		".js": "JavaScript/TypeScript", ".jsx": "JavaScript/TypeScript",
		".py": "Python", ".rs": "Rust", ".java": "Java", ".kt": "Java/Kotlin",
		".rb": "Ruby", ".tf": "Terraform",
	}
	seen := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || seen > 5000 {
			return filepath.SkipDir
		}
		if d.IsDir() {
			n := d.Name()
			if n == "node_modules" || n == ".git" || n == "vendor" || n == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if lang, ok := extLang[filepath.Ext(path)]; ok {
			counts[lang]++
			seen++
		}
		return nil
	})
}
