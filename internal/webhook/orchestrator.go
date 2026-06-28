package webhook

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/rohansah/forge/internal/github"
	"github.com/rohansah/forge/internal/profile"
	"github.com/rohansah/forge/internal/sandbox"
)

// Orchestrator runs the end-to-end (AI-less, for now) loop for one triggered
// issue: authenticate as the installation → clone → profile → comment what it
// understands. The AI agent loop will slot in where the "comment" step is once
// the plumbing is proven on real repos.
type Orchestrator struct {
	app     *github.App
	workDir string
}

func NewOrchestrator(app *github.App, workDir string) *Orchestrator {
	return &Orchestrator{app: app, workDir: workDir}
}

// Handle processes a single trigger event. It is intentionally synchronous and
// self-contained so it can run in a goroutine per delivery; the HTTP handler
// returns 202 immediately and this runs in the background.
func (o *Orchestrator) Handle(ev *IssueEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	owner := ev.Repository.Owner.Login
	repo := ev.Repository.Name
	num := ev.Issue.Number
	log.Printf("forge: run start %s/%s#%d (installation=%d)", owner, repo, num, ev.Installation.ID)

	// 1. Mint an installation-scoped token (the versatility primitive: this
	//    works for ANY repo that installed the App).
	token, err := o.app.InstallationToken(ctx, ev.Installation.ID)
	if err != nil {
		log.Printf("forge: token error for %s/%s: %v", owner, repo, err)
		return
	}

	// 2. Clone into an isolated sandbox; always clean up.
	run, err := sandbox.Clone(ctx, o.workDir, token, owner, repo)
	if err != nil {
		log.Printf("forge: clone error for %s/%s: %v", owner, repo, err)
		_ = o.app.CommentOnIssue(ctx, token, owner, repo, num,
			"⚠️ Forge couldn't clone this repository. Check the App has Contents read access.")
		return
	}
	defer run.Cleanup()

	// 3. Zero-assumption profiling — discover what this repo IS.
	prof, err := profile.Detect(run.Dir)
	if err != nil {
		log.Printf("forge: profile error for %s/%s: %v", owner, repo, err)
		return
	}

	// 4. Report understanding back on the issue. (This step becomes the agent
	//    loop in the next phase.)
	if err := o.app.CommentOnIssue(ctx, token, owner, repo, num, introComment(ev, prof)); err != nil {
		log.Printf("forge: comment error for %s/%s: %v", owner, repo, err)
		return
	}
	log.Printf("forge: run done %s/%s#%d (langs=%v)", owner, repo, num, prof.Languages)
}

// introComment is the human-facing proof-of-life: it shows the maintainer that
// Forge connected, understood the repo's shape, and read their issue. Replaced
// by a planned-changes summary once the agent lands.
func introComment(ev *IssueEvent, p *profile.Profile) string {
	var b strings.Builder
	b.WriteString("👋 **Forge here.** I picked up this issue and had a look at the repository.\n\n")

	if len(p.Languages) > 0 {
		b.WriteString(fmt.Sprintf("**Languages:** %s\n", strings.Join(p.Languages, ", ")))
	}
	if len(p.PackageMgrs) > 0 {
		b.WriteString(fmt.Sprintf("**Package managers:** %s\n", strings.Join(p.PackageMgrs, ", ")))
	}
	if len(p.TestCommands) > 0 {
		b.WriteString(fmt.Sprintf("**Test command(s) I'll verify against:** `%s`\n",
			strings.Join(p.TestCommands, "` , `")))
	}
	b.WriteString(fmt.Sprintf("**CI configured:** %v · **Dockerized:** %v\n", p.HasCI, p.HasDocker))

	if len(p.TopLevel) > 0 {
		shown := p.TopLevel
		if len(shown) > 12 {
			shown = shown[:12]
		}
		b.WriteString(fmt.Sprintf("\n**Top level:** `%s`\n", strings.Join(shown, "`, `")))
	}

	b.WriteString("\n---\n_Plumbing phase: I can authenticate, clone, and profile any repo I'm installed on. " +
		"Code generation lands in the next phase. No changes were made._")
	return b.String()
}
