// Command server is Forge's single entrypoint: a GitHub App webhook receiver
// that, on a triggered issue, authenticates as the installation, clones the
// repo into an isolated sandbox, profiles it, and reports what it understands.
// This is the plumbing phase — no code generation yet, by design.
package main

import (
	"log"
	"net/http"

	"github.com/rohansah/forge/internal/config"
	"github.com/rohansah/forge/internal/github"
	"github.com/rohansah/forge/internal/webhook"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	app, err := github.NewApp(cfg.GitHubAppID, cfg.GitHubPrivateKeyPEM)
	if err != nil {
		log.Fatalf("github app: %v", err)
	}

	orch := webhook.NewOrchestrator(app, cfg.WorkDir)
	h := webhook.NewHandler(cfg.GitHubWebhookSecret, cfg.TriggerLabel, orch)

	mux := http.NewServeMux()
	h.Register(mux)

	addr := ":" + cfg.Port
	log.Printf("forge listening on %s (trigger label=%q)", addr, cfg.TriggerLabel)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
