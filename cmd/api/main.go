// Command api is Forge's kernel HTTP server: the uniform agent-run API that
// the career app, Kronos, and other consumers call. It registers all agents
// against one model provider and serves POST /v1/agents/{name}/run.
//
// (The GitHub PR agent has its own webhook entrypoint, cmd/server, because it
// is event-driven rather than request/response. Both share the kernel.)
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/rohansah/forge/agents/resume"
	"github.com/rohansah/forge/agents/tutor"
	"github.com/rohansah/forge/kernel"
	"github.com/rohansah/forge/kernel/model"
	"github.com/rohansah/forge/kernel/server"
)

func main() {
	m, err := model.FromEnv()
	if err != nil {
		log.Fatalf("model: %v", err)
	}

	reg := kernel.NewRegistry()
	reg.Register(resume.New())
	reg.Register(tutor.New())
	// future: reg.Register(skillgraph.New()), reg.Register(mockinterview.New()), …

	srv := server.New(reg, m, os.Getenv("FORGE_API_KEY"))

	mux := http.NewServeMux()
	srv.Register(mux)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8090"
	}
	log.Printf("forge kernel API on :%s (model=%s, agents=%v)", port, m.Name(), reg.Names())
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
