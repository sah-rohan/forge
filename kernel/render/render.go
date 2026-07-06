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
	KindTutorReply        = "tutor_reply"
	KindError             = "error"
)

// TutorReply — one turn from the study tutor. Say is the prose answer; Show is
// an optional directive the frontend renders with its OWN components (Kronos'
// SVG diagrams/animations) - the model never generates graphics, it selects
// and parameterizes them, which is what keeps this cheap and correct.
type TutorReply struct {
	Say       string     `json:"say"`
	Show      *TutorShow `json:"show,omitempty"`
	FollowUps []string   `json:"follow_ups,omitempty"` // suggested next questions
}

// TutorShow selects a frontend visual. Exactly one family of fields applies
// per type.
type TutorShow struct {
	// "system_diagram" | "walkthrough" | "concept" | "algo"
	Type string `json:"type"`
	// system_diagram / walkthrough: which problem's diagram, and (walkthrough)
	// which hop to highlight.
	ProblemSlug string `json:"problem_slug,omitempty"`
	Step        int    `json:"step,omitempty"`
	// concept: id of a concept illustration ("trie", "hash_ring", ...).
	ConceptID string `json:"concept_id,omitempty"`
	// algo: named animation with its input, e.g. "two_pointer" on an array.
	AlgoID string `json:"algo_id,omitempty"`
	Input  []int  `json:"input,omitempty"`
	Target int    `json:"target,omitempty"`
}

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
