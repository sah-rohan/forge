package resume

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rohansah/forge/kernel"
	"github.com/rohansah/forge/kernel/model"
	"github.com/rohansah/forge/kernel/render"
)

// fakeModel returns canned JSON so the agent is tested without any network /
// API key. It also records the request so we can assert the agent built the
// prompt correctly.
type fakeModel struct {
	lastReq model.Request
	reply   string
}

func (f *fakeModel) Name() string { return "fake" }
func (f *fakeModel) Complete(_ context.Context, req model.Request) (*model.Response, error) {
	f.lastReq = req
	return &model.Response{Text: f.reply, Model: "fake"}, nil
}

func TestResumeGrill_Run(t *testing.T) {
	f := &fakeModel{reply: `{
		"summary": "Focus on the translation cache and payments.",
		"probes": [
			{"from_line": "Built a Redis→Postgres translation cache",
			 "question": "How do you invalidate on edit?",
			 "why": "tests cache coherency",
			 "model_answer": "Delete the key + the row on PUT.",
			 "category": "system_design", "difficulty": 4}
		]
	}`}

	ctxJSON, _ := json.Marshal(contextShape{
		Resume:     "Founding engineer at ISF. Built a Redis→Postgres translation cache. Hardened Stripe webhooks.",
		TargetRole: "Backend SWE",
	})

	out, err := New().Run(context.Background(), f, kernel.Input{Context: ctxJSON})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Kind != render.KindResumeGrill {
		t.Fatalf("kind = %q, want %q", out.Kind, render.KindResumeGrill)
	}

	var grill render.ResumeGrill
	if err := json.Unmarshal(out.Data, &grill); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if len(grill.Probes) != 1 || grill.Probes[0].Category != "system_design" {
		t.Fatalf("unexpected probes: %+v", grill.Probes)
	}

	// The agent must have forced structured output and included the resume +
	// target role in the prompt.
	if f.lastReq.JSONSchema == nil {
		t.Fatal("expected JSONSchema to be set (structured output)")
	}
	if len(f.lastReq.Messages) == 0 ||
		!contains(f.lastReq.Messages[0].Content, "translation cache") ||
		!contains(f.lastReq.Messages[0].Content, "Backend SWE") {
		t.Fatalf("prompt missing resume/role: %q", f.lastReq.Messages[0].Content)
	}
}

func TestResumeGrill_RejectsShortResume(t *testing.T) {
	ctxJSON, _ := json.Marshal(contextShape{Resume: "too short"})
	_, err := New().Run(context.Background(), &fakeModel{}, kernel.Input{Context: ctxJSON})
	if err == nil {
		t.Fatal("expected error for too-short resume")
	}
}

func TestResumeGrill_RejectsBadModelJSON(t *testing.T) {
	f := &fakeModel{reply: `not json`}
	ctxJSON, _ := json.Marshal(contextShape{
		Resume: "A sufficiently long resume describing real engineering work and projects.",
	})
	_, err := New().Run(context.Background(), f, kernel.Input{Context: ctxJSON})
	if err == nil {
		t.Fatal("expected error when model returns invalid JSON")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
