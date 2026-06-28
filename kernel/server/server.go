// Package server exposes the kernel over HTTP: the uniform agent-run endpoint
// every consumer (the career app, Kronos, internal agents) calls.
//
//	POST /v1/agents/{name}/run   { "context": {...}, "options": {...} }
//	  -> { "kind": "...", "data": {...} }   // typed, frontend renders by kind
package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/rohansah/forge/kernel"
	"github.com/rohansah/forge/kernel/model"
)

type Server struct {
	reg   *kernel.Registry
	model model.Model
	// apiKey gates the API. Consumers send it as X-Forge-Key.
	apiKey string
}

func New(reg *kernel.Registry, m model.Model, apiKey string) *Server {
	return &Server{reg: reg, model: m, apiKey: apiKey}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/agents/{name}/run", s.runAgent)
	mux.HandleFunc("GET /v1/agents", s.listAgents)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func (s *Server) authorized(r *http.Request) bool {
	if s.apiKey == "" {
		return true // unset = open (local dev)
	}
	return r.Header.Get("X-Forge-Key") == s.apiKey
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": s.reg.Names()})
}

func (s *Server) runAgent(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	name := r.PathValue("name")
	agent, ok := s.reg.Get(name)
	if !ok {
		http.Error(w, "unknown agent", http.StatusNotFound)
		return
	}

	var in kernel.Input
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}

	// Agent runs are model-bound; give them a generous but bounded deadline.
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	start := time.Now()
	out, err := agent.Run(ctx, s.model, in)
	if err != nil {
		log.Printf("forge: agent %s failed: %v", name, err)
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"kind": "error",
			"data": map[string]string{"message": err.Error()},
		})
		return
	}
	log.Printf("forge: agent %s ok in %s (model=%s)", name, time.Since(start), s.model.Name())
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
