// Package webhook verifies and parses the GitHub App webhook events Forge
// cares about. Right now that's just issues being labeled with the trigger
// label — the event that starts an agent run.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"strings"
)

// IssueEvent is the slice of GitHub's `issues` webhook payload we need.
type IssueEvent struct {
	Action string `json:"action"`
	Issue  struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	} `json:"issue"`
	Label struct {
		Name string `json:"name"`
	} `json:"label"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// VerifySignature checks the X-Hub-Signature-256 header against the raw body
// using the webhook secret. GitHub signs every delivery; rejecting bad
// signatures is what stops anyone from POSTing fake events to the endpoint.
func VerifySignature(secret string, body []byte, sigHeader string) bool {
	if !strings.HasPrefix(sigHeader, "sha256=") {
		return false
	}
	want := strings.TrimPrefix(sigHeader, "sha256=")
	var mac hash.Hash = hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	got := hex.EncodeToString(mac.Sum(nil))
	// constant-time compare
	return hmac.Equal([]byte(got), []byte(want))
}

// ParseIssueEvent decodes an `issues` event payload.
func ParseIssueEvent(body []byte) (*IssueEvent, error) {
	var ev IssueEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, fmt.Errorf("decode issue event: %w", err)
	}
	return &ev, nil
}

// IsTrigger reports whether this event should start a run: an issue was just
// labeled with the trigger label, OR an already-labeled issue was opened.
func (e *IssueEvent) IsTrigger(triggerLabel string) bool {
	switch e.Action {
	case "labeled":
		return e.Label.Name == triggerLabel
	case "opened", "reopened":
		for _, l := range e.Issue.Labels {
			if l.Name == triggerLabel {
				return true
			}
		}
	}
	return false
}
