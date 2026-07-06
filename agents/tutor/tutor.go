// Package tutor implements the Kronos study tutor: a grounded teaching agent
// that answers with the caller's OWN progress as context (LeetCode solves,
// system-design modules completed, the app's resource catalog) and drives the
// frontend's existing SVG diagrams/animations via typed directives instead of
// generating graphics.
//
// Cost design: the caller retrieves and sends only the RELEVANT slice of
// context (a few solutions, the touched modules) - never the whole history -
// and the reply is a few hundred tokens of prose plus a tiny directive. That
// keeps a turn at fractions of a cent on a mini-class model.
package tutor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rohansah/forge/kernel"
	"github.com/rohansah/forge/kernel/model"
	"github.com/rohansah/forge/kernel/render"
)

type Agent struct{}

func New() *Agent { return &Agent{} }

func (a *Agent) Name() string { return "tutor" }

// SolvedProblem is one LeetCode problem the user solved, with their solution
// when the caller judged it relevant to the question.
type SolvedProblem struct {
	Slug       string `json:"slug"`
	Title      string `json:"title"`
	Difficulty string `json:"difficulty"`
	Code       string `json:"code,omitempty"`
	Lang       string `json:"lang,omitempty"`
}

// Module is a system-design / AI-system-design module and whether the user
// completed it.
type Module struct {
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Kind   string `json:"kind"` // "design" | "genai"
	Solved bool   `json:"solved"`
}

// Turn is one prior exchange, so the tutor stays conversational.
type Turn struct {
	Role    string `json:"role"` // "user" | "tutor"
	Content string `json:"content"`
}

// contextShape is what Kronos posts in Input.Context.
type contextShape struct {
	Question string          `json:"question"`
	History  []Turn          `json:"history,omitempty"`  // last few turns only
	Solved   []SolvedProblem `json:"solved,omitempty"`   // relevant solves (retrieved, not all)
	Modules  []Module        `json:"modules,omitempty"`  // SD/GenAI modules + status
	Concepts []string        `json:"concepts,omitempty"` // available concept-diagram ids
	Algos    []string        `json:"algos,omitempty"`    // available algo-animation ids
}

const systemPrompt = `You are the study tutor inside Kronos, a group LeetCode +
system-design dashboard. You are given the student's question, their recent
conversation, the problems they have ACTUALLY solved (sometimes with their
code), and the system-design modules they have completed.

Teach like a great human tutor:
- Ground every answer in THEIR work: reference their solutions and completed
  modules by name when relevant ("in your Two Sum solution you used...").
- Explain every term the first time you use it; assume no prior knowledge.
- Be concrete and short: one idea at a time, then offer where to go next.
- Never invent problems, modules, or solutions not present in the context.

Visuals: you may attach at most ONE "show" directive per reply, chosen from the
frontend's own components (you never draw):
- {"type":"system_diagram","problem_slug":X} - a module's architecture diagram.
- {"type":"walkthrough","problem_slug":X,"step":N} - highlight hop N of that
  module's request/response flow (0-indexed).
- {"type":"concept","concept_id":X} - a concept illustration; only ids from the
  provided list.
- {"type":"algo","algo_id":X,"input":[...],"target":N} - an algorithm
  animation; only ids from the provided list.
Attach a visual only when it genuinely helps; omit "show" otherwise.
Suggest up to 3 short follow_ups the student would naturally ask next.`

// outputSchema constrains the model to the TutorReply shape.
var outputSchema = []byte(`{
  "say": "the tutoring answer, plain prose",
  "show": {"type": "system_diagram|walkthrough|concept|algo",
           "problem_slug": "string?", "step": 0,
           "concept_id": "string?", "algo_id": "string?",
           "input": [0], "target": 0},
  "follow_ups": ["string"]
}`)

func (a *Agent) Run(ctx context.Context, m model.Model, in kernel.Input) (*render.Output, error) {
	var c contextShape
	if err := json.Unmarshal(in.Context, &c); err != nil {
		return nil, fmt.Errorf("tutor: bad context: %w", err)
	}
	if strings.TrimSpace(c.Question) == "" {
		return nil, fmt.Errorf("tutor: empty question")
	}

	resp, err := m.Complete(ctx, model.Request{
		System:      systemPrompt,
		Messages:    buildMessages(c),
		JSONSchema:  outputSchema,
		MaxTokens:   1200,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, err
	}

	var reply render.TutorReply
	if err := json.Unmarshal([]byte(resp.Text), &reply); err != nil {
		return nil, fmt.Errorf("tutor: model returned invalid JSON: %w", err)
	}
	if strings.TrimSpace(reply.Say) == "" {
		return nil, fmt.Errorf("tutor: model returned empty answer")
	}
	sanitizeShow(&reply, c)
	return render.New(render.KindTutorReply, reply)
}

// buildMessages folds the student context into one user message plus the
// recent turns, keeping the prompt small and cache-friendly.
func buildMessages(c contextShape) []model.Message {
	var b strings.Builder

	if len(c.Solved) > 0 {
		b.WriteString("PROBLEMS THE STUDENT SOLVED (relevant to this question):\n")
		for _, p := range c.Solved {
			fmt.Fprintf(&b, "- %s (%s, %s)\n", p.Title, p.Slug, p.Difficulty)
			if p.Code != "" {
				fmt.Fprintf(&b, "  their %s solution:\n%s\n", p.Lang, truncate(p.Code, 2000))
			}
		}
	}
	if len(c.Modules) > 0 {
		b.WriteString("\nSYSTEM-DESIGN MODULES:\n")
		for _, mod := range c.Modules {
			state := "not started"
			if mod.Solved {
				state = "completed"
			}
			fmt.Fprintf(&b, "- %s (%s, %s): %s\n", mod.Title, mod.Slug, mod.Kind, state)
		}
	}
	if len(c.Concepts) > 0 {
		fmt.Fprintf(&b, "\nAVAILABLE concept_id values: %s\n", strings.Join(c.Concepts, ", "))
	}
	if len(c.Algos) > 0 {
		fmt.Fprintf(&b, "AVAILABLE algo_id values: %s\n", strings.Join(c.Algos, ", "))
	}

	msgs := make([]model.Message, 0, len(c.History)+1)
	// Cap history defensively even if the caller sends more.
	hist := c.History
	if len(hist) > 8 {
		hist = hist[len(hist)-8:]
	}
	for _, t := range hist {
		role := "user"
		if t.Role == "tutor" {
			role = "assistant"
		}
		msgs = append(msgs, model.Message{Role: role, Content: truncate(t.Content, 1500)})
	}
	msgs = append(msgs, model.Message{Role: "user", Content: b.String() + "\nQUESTION: " + c.Question})
	return msgs
}

// sanitizeShow drops directives referencing visuals the frontend didn't offer,
// so a hallucinated id degrades to prose instead of a broken UI.
func sanitizeShow(r *render.TutorReply, c contextShape) {
	if r.Show == nil {
		return
	}
	switch r.Show.Type {
	case "system_diagram", "walkthrough":
		if r.Show.ProblemSlug == "" {
			r.Show = nil
		}
	case "concept":
		if !contains(c.Concepts, r.Show.ConceptID) {
			r.Show = nil
		}
	case "algo":
		if !contains(c.Algos, r.Show.AlgoID) {
			r.Show = nil
		}
	default:
		r.Show = nil
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n…(truncated)"
}
