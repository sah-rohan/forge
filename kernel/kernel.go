// Package kernel ties the three seams together. An Agent assembles context and
// turns it into a typed render.Output using a model.Model. The kernel runs the
// agent, enforces the budget, and returns the structured result. Both the PR
// agent and the career-app agents are just implementations of Agent.
package kernel

import (
	"context"

	"github.com/rohansah/forge/kernel/model"
	"github.com/rohansah/forge/kernel/render"
)

// Input is the generic envelope every agent receives. Context is the
// agent-specific payload (a resume, an LC history, a repo ref) as raw JSON —
// each agent unmarshals it into its own typed shape. Options carries run-time
// knobs (target team, difficulty, etc.).
type Input struct {
	Context []byte         `json:"context"`
	Options map[string]any `json:"options,omitempty"`
}

// Agent is the unit of work. It owns its context adapter, its prompt, and its
// output schema. The kernel gives it a Model and gets back a typed Output.
type Agent interface {
	// Name is the route segment: POST /v1/agents/{name}/run.
	Name() string
	// Run assembles context, calls the model, and returns a typed Output.
	Run(ctx context.Context, m model.Model, in Input) (*render.Output, error)
}

// Registry maps agent names to implementations. The HTTP server dispatches on
// it; adding an agent is one Register call.
type Registry struct {
	agents map[string]Agent
}

func NewRegistry() *Registry {
	return &Registry{agents: map[string]Agent{}}
}

func (r *Registry) Register(a Agent) {
	r.agents[a.Name()] = a
}

func (r *Registry) Get(name string) (Agent, bool) {
	a, ok := r.agents[name]
	return a, ok
}

func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.agents))
	for n := range r.agents {
		out = append(out, n)
	}
	return out
}
