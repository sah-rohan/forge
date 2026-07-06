package tutor

import (
	"strings"
	"testing"

	"github.com/rohansah/forge/kernel/render"
)

func TestSanitizeShowDropsUnknownVisuals(t *testing.T) {
	c := contextShape{Concepts: []string{"trie"}, Algos: []string{"two_pointer"}}

	cases := []struct {
		name string
		show render.TutorShow
		keep bool
	}{
		{"known concept", render.TutorShow{Type: "concept", ConceptID: "trie"}, true},
		{"hallucinated concept", render.TutorShow{Type: "concept", ConceptID: "btree"}, false},
		{"known algo", render.TutorShow{Type: "algo", AlgoID: "two_pointer"}, true},
		{"hallucinated algo", render.TutorShow{Type: "algo", AlgoID: "quantum_sort"}, false},
		{"diagram with slug", render.TutorShow{Type: "system_diagram", ProblemSlug: "design-url-shortener"}, true},
		{"diagram without slug", render.TutorShow{Type: "system_diagram"}, false},
		{"unknown type", render.TutorShow{Type: "hologram"}, false},
	}
	for _, tc := range cases {
		r := render.TutorReply{Say: "x", Show: &tc.show}
		sanitizeShow(&r, c)
		if got := r.Show != nil; got != tc.keep {
			t.Errorf("%s: kept=%v, want %v", tc.name, got, tc.keep)
		}
	}
}

func TestBuildMessagesGroundsAndCaps(t *testing.T) {
	c := contextShape{
		Question: "How does the two-pointer trick work?",
		Solved:   []SolvedProblem{{Slug: "two-sum", Title: "Two Sum", Difficulty: "Easy", Code: "def f(): pass", Lang: "python"}},
		Modules:  []Module{{Slug: "design-url-shortener", Title: "Design a URL Shortener", Kind: "design", Solved: true}},
	}
	// 20 turns of history must be capped to the last 8.
	for i := 0; i < 20; i++ {
		c.History = append(c.History, Turn{Role: "user", Content: "turn"})
	}

	msgs := buildMessages(c)
	if len(msgs) != 9 { // 8 history + 1 final user message
		t.Fatalf("messages = %d, want 9", len(msgs))
	}
	final := msgs[len(msgs)-1].Content
	for _, want := range []string{"Two Sum", "their python solution", "Design a URL Shortener", "completed", "QUESTION:"} {
		if !strings.Contains(final, want) {
			t.Errorf("final message missing %q", want)
		}
	}
}
