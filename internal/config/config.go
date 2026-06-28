// Package config centralizes environment-variable loading for Forge.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds everything the orchestrator needs to run. A GitHub App is
// identified by its numeric App ID + a private key (PEM); each repo that
// installs the App gets an installation ID we resolve per-event.
type Config struct {
	Port string

	// GitHub App identity. The private key signs the JWT we exchange for
	// short-lived, per-installation access tokens.
	GitHubAppID         int64
	GitHubPrivateKeyPEM string
	GitHubWebhookSecret string

	// Where repos are cloned into during a run. Each run gets an isolated
	// subdirectory that is removed on completion.
	WorkDir string

	// TriggerLabel is the default issue label that starts a run when a repo's
	// own config doesn't override it.
	TriggerLabel string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:                getEnvOr("PORT", "8080"),
		GitHubPrivateKeyPEM: os.Getenv("GITHUB_APP_PRIVATE_KEY"),
		GitHubWebhookSecret: os.Getenv("GITHUB_WEBHOOK_SECRET"),
		WorkDir:             getEnvOr("FORGE_WORKDIR", os.TempDir()+"/forge-runs"),
		TriggerLabel:        getEnvOr("FORGE_TRIGGER_LABEL", "agent"),
	}

	appID := os.Getenv("GITHUB_APP_ID")
	if appID != "" {
		id, err := strconv.ParseInt(appID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("GITHUB_APP_ID must be a number: %w", err)
		}
		cfg.GitHubAppID = id
	}

	if cfg.GitHubAppID == 0 {
		return nil, fmt.Errorf("GITHUB_APP_ID is required")
	}
	if cfg.GitHubPrivateKeyPEM == "" {
		return nil, fmt.Errorf("GITHUB_APP_PRIVATE_KEY is required")
	}
	if cfg.GitHubWebhookSecret == "" {
		return nil, fmt.Errorf("GITHUB_WEBHOOK_SECRET is required")
	}
	return cfg, nil
}

func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
