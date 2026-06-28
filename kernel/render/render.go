// Package render defines the typed-output contract between agents and any
// frontend. Every agent returns an Output: a `kind` discriminator plus a
// payload. The frontend has one component per kind. This is the "generative
// UI" seam — the AI is constrained to emit these shapes, so the React side
// never parses prose and the AI never knows about the UI.
package render

import "encoding/json"

// Output is what every agent run returns and every frontend consumes. Kind
// selects the React component; Data is that component's typed props as JSON.
type Output struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// New builds an Output, marshaling the typed payload into Data.
func New(kind string, payload any) (*Output, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Output{Kind: kind, Data: b}, nil
}

// ---- Known output kinds (the discriminated union the SDK mirrors in TS) ----

const (
	KindResumeGrill       = "resume_grill"
	KindSkillGraph        = "skill_graph"
	KindProblemRecs       = "problem_recs"
	KindMockInterview     = "mock_interview"
	KindBehavioralFeedback = "behavioral_feedback"
	KindPRPlan            = "pr_plan"
	KindError             = "error"
)

// ResumeGrill — interviewer-style probes generated from a resume. Each probe
// ties back to the exact resume line it interrogates, so the UI can highlight
// the claim being tested.
type ResumeGrill struct {
	Summary string  `json:"summary"`
	Probes  []Probe `json:"probes"`
}

type Probe struct {
	FromLine   string `json:"from_line"`   // the resume claim being interrogated
	Question   string `json:"question"`    // what an interviewer would ask
	Why        string `json:"why"`         // what they're really testing
	ModelAnswer string `json:"model_answer"` // a strong answer to study
	Category   string `json:"category"`    // "system_design" | "coding" | "behavioral" | "project"
	Difficulty int    `json:"difficulty"`  // 1-5
}
