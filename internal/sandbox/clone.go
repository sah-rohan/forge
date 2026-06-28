// Package sandbox isolates a single agent run. Each run clones the target repo
// into a fresh temp directory using the installation token, does its work, and
// the directory is removed on completion. Nothing about a repo is persisted
// between runs — this is the isolation boundary that keeps one repo's token
// and code from leaking into another's run.
package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Run represents one isolated working directory for a single issue.
type Run struct {
	Dir   string
	Owner string
	Repo  string
}

// Clone shallow-clones owner/repo at the default branch into a fresh directory
// under workDir, authenticating with the installation token. Shallow (depth=1)
// keeps it fast; the profiler + agent only need the current tree, and PR
// history is fetched separately when the pattern-miner phase lands.
func Clone(ctx context.Context, workDir, token, owner, repo string) (*Run, error) {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir workdir: %w", err)
	}
	dir := filepath.Join(workDir, fmt.Sprintf("%s-%s-%d", owner, repo, time.Now().UnixNano()))

	// x-access-token is GitHub's documented username for App installation
	// tokens over HTTPS git.
	url := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", token, owner, repo)

	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", url, dir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git clone: %w: %s", err, out)
	}
	return &Run{Dir: dir, Owner: owner, Repo: repo}, nil
}

// Cleanup removes the run's working directory. Always defer this.
func (r *Run) Cleanup() {
	if r == nil || r.Dir == "" {
		return
	}
	_ = os.RemoveAll(r.Dir)
}
