// Package github wraps the slice of the GitHub REST API that Forge needs:
// authenticating AS a GitHub App, exchanging that for a short-lived
// per-installation token, and the handful of issue/PR/git operations the
// orchestrator performs. Keeping this thin and dependency-free (stdlib +
// golang-jwt) keeps the agent portable across any repo that installs the App.
package github

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const apiBase = "https://api.github.com"

// App authenticates as a GitHub App. The App ID + RSA private key let us mint
// a short-lived JWT; that JWT is exchanged per-installation for an access
// token scoped to exactly the repos that installation can see. This is the
// mechanism that makes Forge installable on ANY repo without per-repo secrets.
type App struct {
	appID      int64
	privateKey *rsa.PrivateKey
	http       *http.Client
}

func NewApp(appID int64, pemKey string) (*App, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(pemKey))
	if err != nil {
		return nil, fmt.Errorf("parse app private key: %w", err)
	}
	return &App{
		appID:      appID,
		privateKey: key,
		http:       &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// appJWT mints a ~10-minute JWT signed with the App's private key. GitHub
// accepts this as proof we are the App for the /app/installations endpoints.
func (a *App) appJWT() (string, error) {
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		// Backdate iat by 60s to tolerate clock skew (GitHub recommendation).
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
		Issuer:    fmt.Sprintf("%d", a.appID),
	})
	return tok.SignedString(a.privateKey)
}

// InstallationToken exchanges the App JWT for an access token scoped to a
// single installation. These tokens expire in ~1 hour and are what we use for
// all repo operations during a run.
func (a *App) InstallationToken(ctx context.Context, installationID int64) (string, error) {
	jwtStr, err := a.appJWT()
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", apiBase, installationID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	req.Header.Set("Authorization", "Bearer "+jwtStr)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := a.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("installation token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("installation token failed (%d): %s", resp.StatusCode, body)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}
	return out.Token, nil
}

// CommentOnIssue posts a comment using an installation token. owner/repo are
// the target, number is the issue (or PR) number.
func (a *App) CommentOnIssue(ctx context.Context, token, owner, repo string, number int, body string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", apiBase, owner, repo, number)
	payload, _ := json.Marshal(map[string]string{"body": body})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("comment request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("comment failed (%d): %s", resp.StatusCode, b)
	}
	return nil
}
