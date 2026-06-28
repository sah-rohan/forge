// Package resume implements the resume-grill agent: given a resume (and
// optionally a target role/team), it produces the questions an interviewer
// would actually ask about that resume, each tied to the line it interrogates,
// with a model answer to study. It is the highest-leverage first agent because
// it needs only one input and dogfoods on the builder's own resume.
package resume

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rohansah/forge/kernel"
	"github.com/rohansah/forge/kernel/model"
	"github.com/rohansah/forge/kernel/render"
)

type Agent struct{}

func New() *Agent { return &Agent{} }

func (a *Agent) Name() string { return "resume-grill" }

// contextShape is what the caller posts in Input.Context.
type contextShape struct {
	Resume     string `json:"resume"`      // raw resume text
	TargetRole string `json:"target_role"` // optional, e.g. "Backend SWE, Stripe"
	Focus      string `json:"focus"`       // optional: "system_design" | "coding" | "behavioral" | ""
}

const systemPrompt = `You are a senior technical interviewer at a top company. You are given a
candidate's resume. Generate the questions you would actually ask in an
interview, derived directly from claims on the resume.

Rules:
- Each question must tie to a SPECIFIC line/claim on the resume (quote it in from_line).
- Mix categories: project deep-dives, system_design, coding, and behavioral —
  weighted toward whatever the resume emphasizes (and the focus hint if given).
- "why" states what the question really tests.
- model_answer is a strong, concrete answer a candidate should be able to give,
  grounded in the resume's actual content (not generic).
- difficulty is 1-5, calibrated so the candidate is challenged but could
  plausibly answer given what the resume claims they've done.
- Produce 8-12 probes.`

// outputSchema is handed to the model to force structured JSON.
var outputSchema = []byte(`{
  "summary": "one-paragraph read on what an interviewer will focus on",
  "probes": [
    {"from_line": "string", "question": "string", "why": "string",
     "model_answer": "string", "category": "project|system_design|coding|behavioral",
     "difficulty": 1}
  ]
}`)

func (a *Agent) Run(ctx context.Context, m model.Model, in kernel.Input) (*render.Output, error) {
	var c contextShape
	if err := json.Unmarshal(in.Context, &c); err != nil {
		return nil, fmt.Errorf("resume-grill: bad context: %w", err)
	}
	if len(c.Resume) < 30 {
		return nil, fmt.Errorf("resume-grill: resume text too short")
	}

	user := "RESUME:\n" + c.Resume
	if c.TargetRole != "" {
		user += "\n\nTARGET ROLE/TEAM: " + c.TargetRole
	}
	if c.Focus != "" {
		user += "\n\nFOCUS THIS GRILL ON: " + c.Focus
	}

	resp, err := m.Complete(ctx, model.Request{
		System:      systemPrompt,
		Messages:    []model.Message{{Role: "user", Content: user}},
		JSONSchema:  outputSchema,
		MaxTokens:   4096,
		Temperature: 0.4,
	})
	if err != nil {
		return nil, err
	}

	// Validate + reshape into the typed render payload. If the model returned
	// malformed JSON we fail loudly rather than shipping garbage to the UI.
	var grill render.ResumeGrill
	if err := json.Unmarshal([]byte(resp.Text), &grill); err != nil {
		return nil, fmt.Errorf("resume-grill: model returned invalid JSON: %w", err)
	}
	return render.New(render.KindResumeGrill, grill)
}
