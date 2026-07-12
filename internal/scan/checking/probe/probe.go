// Package probe wraps the 7 external version-pinned CLI tools behind the
// domain.Checker seam (US-007, SR-7, 질문3). The framework (subprocess exec +
// timeout + parse hook) is real; tool-specific output parsers ship as safe stubs
// where the real bundled binary is needed (pinned progressively, C-5). The CLI
// binaries themselves are bundled at Infrastructure/Build (SECURITY-10).
package probe

import (
	"context"
	"os"
	"os/exec"
	"time"

	"github.com/enki-offen/chiller/internal/domain"
)

// Adapter is a probe: a domain.Checker plus tool metadata.
type Adapter interface {
	domain.Checker
	ToolName() string
	PinnedVersion() string
	OwnedCWEs() []domain.CWE
}

// Parser turns a tool's raw stdout into findings for a target.
type Parser func(target domain.Target, stdout []byte) ([]domain.Finding, error)

// runner runs a version-pinned CLI with a timeout (NFR-12) and parses stdout.
// It backs every adapter; the concrete adapters supply argv + Parser.
type runner struct {
	id        domain.CheckerID
	tool      string
	version   string
	scope     domain.ScopeKind
	cwes      []domain.CWE
	transport domain.TransportKind           // zero value = ProxyEligible (PR-12c)
	argv      func(t domain.Target) []string // nil ⇒ stub (no external binary yet)
	parse     Parser
	timeout   time.Duration
	// proxyEnv holds extra environment (HTTP(S)_PROXY=...) injected into the
	// subprocess in financial mode. Set only on ProxyEligible adapters by
	// ConfigureProxy; DirectOnly adapters (testssl) keep it empty (PR-12c/d).
	proxyEnv []string
	exec     func(ctx context.Context, name string, env []string, args ...string) ([]byte, error)
}

func (r *runner) ID() domain.CheckerID            { return r.id }
func (r *runner) Scope() domain.ScopeKind         { return r.scope }
func (r *runner) ToolName() string                { return r.tool }
func (r *runner) PinnedVersion() string           { return r.version }
func (r *runner) OwnedCWEs() []domain.CWE         { return r.cwes }
func (r *runner) Transport() domain.TransportKind { return r.transport }

func (r *runner) Check(ctx domain.ScanContext) (domain.CheckResult, error) {
	if r.argv == nil { // stub: binary not wired yet — ran, produced nothing (Checked, no finding)
		return domain.CheckResult{}, nil
	}
	timeout := r.timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	cctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := r.argv(ctx.Target)
	run := r.exec
	if run == nil {
		run = defaultExec
	}
	out, err := run(cctx, r.tool, r.proxyEnv, args...)
	if err != nil {
		return domain.CheckResult{}, err // tool-failed for this adapter's items
	}
	findings, err := r.parse(ctx.Target, out)
	if err != nil {
		return domain.CheckResult{}, err
	}
	return domain.CheckResult{Findings: findings}, nil
}

// defaultExec runs the CLI, appending any proxy env (financial mode) to the
// inherited environment. An empty env is a no-op — the tool runs direct.
func defaultExec(ctx context.Context, name string, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd.Output()
}

// ConfigureProxy routes every ProxyEligible adapter's subprocess through the
// authenticated auth-proxy in financial mode by injecting HTTP(S)_PROXY into its
// environment; DirectOnly adapters (testssl — TLS handshake must reach the
// origin directly) are left untouched. Transport-driven (PR-12c): adding a tool
// needs no change here, only its Transport() declaration. Empty proxyURL is a
// no-op. Adapters not backed by the runner are skipped.
func ConfigureProxy(adapters []Adapter, proxyURL string) {
	if proxyURL == "" {
		return
	}
	env := []string{
		"HTTP_PROXY=" + proxyURL, "HTTPS_PROXY=" + proxyURL,
		"http_proxy=" + proxyURL, "https_proxy=" + proxyURL,
	}
	for _, a := range adapters {
		r, ok := a.(*runner)
		if !ok {
			continue
		}
		if r.transport == domain.ProxyEligible {
			r.proxyEnv = env
		}
	}
}
