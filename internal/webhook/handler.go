package webhook

import (
	"io"
	"log"
	"net/http"
)

// Handler is the HTTP surface: GitHub posts deliveries to /webhook. We verify
// the signature, filter to trigger events, and hand off to the orchestrator in
// a goroutine so GitHub gets a fast 202 (deliveries time out at 10s).
type Handler struct {
	secret       string
	triggerLabel string
	orch         *Orchestrator
}

func NewHandler(secret, triggerLabel string, orch *Orchestrator) *Handler {
	return &Handler{secret: secret, triggerLabel: triggerLabel, orch: orch}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /webhook", h.webhook)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20)) // 5MB cap
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	if !VerifySignature(h.secret, body, r.Header.Get("X-Hub-Signature-256")) {
		log.Printf("forge: rejected delivery — bad signature")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// We only act on `issues` events; everything else is acknowledged and
	// ignored so GitHub doesn't retry.
	if r.Header.Get("X-GitHub-Event") != "issues" {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	ev, err := ParseIssueEvent(body)
	if err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}

	if ev.IsTrigger(h.triggerLabel) {
		// Run in the background; respond fast.
		go h.orch.Handle(ev)
	}
	w.WriteHeader(http.StatusAccepted)
}
