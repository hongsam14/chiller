// Package probe wraps the 7 external version-pinned CLI tools behind the
// domain.Checker seam (US-007, SR-7, 질문3). The framework (subprocess exec +
// timeout + parse hook) is real; tool-specific output parsers ship as safe stubs
// where the real bundled binary is needed (pinned progressively, C-5). The CLI
// binaries themselves are bundled at Infrastructure/Build (SECURITY-10).
//
// An unwired adapter (no argv, no real parser, no bundled binary) MUST NOT be
// mistaken for a clean run: it fails with ErrNotWired so its owned items land in
// StatusToolFailed — "no verdict" — instead of StatusChecked. Reporting an item
// as checked when nothing ever executed would turn every one of its weaknesses
// into a silent false negative.
package probe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hongsam14/chiller/internal/domain"
)

// ErrNotWired is returned by an adapter whose CLI is not yet wired (no argv, no
// real parser, no bundled binary). It is distinct from a genuine tool failure:
// the tool never ran. Both classify as StatusToolFailed — chiller obtained no
// verdict for the owned items — but the message names the real cause.
var ErrNotWired = errors.New("probe not wired: no argv/parser/binary (infra-plan §3)")

// Adapter is a probe: a domain.Checker plus tool metadata.
type Adapter interface {
	domain.Checker
	ToolName() string
	PinnedVersion() string
	OwnedCWEs() []domain.CWE
	// Wired reports whether this adapter can actually execute its CLI. An
	// unwired adapter always fails with ErrNotWired; callers use this to warn
	// at boot which items cannot be judged this run.
	Wired() bool
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
	// outArg backs file-output tools: most probes (subzy/testssl/dirhunt/corsy/
	// humble) write their JSON to a FILE, not stdout, and pollute stdout with
	// banner/progress text. When set, the runner creates a temp file, builds args
	// via outArg(target, path), execs, and feeds the FILE's bytes to the parser
	// (an empty/absent file = the tool ran and found nothing → clean). Mutually
	// exclusive with argv. Discovered by the first real in-image run (F6).
	outArg  func(t domain.Target, outPath string) []string
	parse   Parser
	timeout time.Duration
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
func (r *runner) Wired() bool                     { return r.argv != nil || r.outArg != nil }

func (r *runner) Check(ctx domain.ScanContext) (domain.CheckResult, error) {
	if r.argv == nil && r.outArg == nil {
		// Not wired: nothing executed, so there is no verdict. Fail loudly →
		// StatusToolFailed, never StatusChecked (see package doc).
		return domain.CheckResult{}, fmt.Errorf("%s: %w", r.tool, ErrNotWired)
	}
	timeout := r.timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	cctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// File-output tools write JSON to a temp file; stdout tools parse stdout.
	var args []string
	var outPath string
	if r.outArg != nil {
		// Use a temp DIR and a path INSIDE it that does not yet exist. Some tools
		// (dirhunt) try to READ an existing --to-file as JSON to merge into, so a
		// pre-created empty file makes them fail with a JSON decode error. Letting
		// the tool create the file fresh avoids that.
		dir, err := os.MkdirTemp("", "probe-*")
		if err != nil {
			return domain.CheckResult{}, fmt.Errorf("%s: scratch dir: %w", r.tool, err)
		}
		defer func() { _ = os.RemoveAll(dir) }()
		outPath = filepath.Join(dir, "out.json")
		args = r.outArg(ctx.Target, outPath)
	} else {
		args = r.argv(ctx.Target)
	}

	run := r.exec
	if run == nil {
		run = defaultExec
	}
	out, err := run(cctx, r.tool, r.proxyEnv, args...)
	if err != nil {
		return domain.CheckResult{}, err // tool-failed for this adapter's items
	}

	data := out
	if r.outArg != nil {
		// Read the tool's JSON file. A missing/empty file means the tool ran and
		// produced no findings → parser returns clean (Checked), NOT tool-failed.
		// #nosec G304 -- outPath is a fixed name inside a temp dir we just created (os.MkdirTemp), not user input.
		if b, rerr := os.ReadFile(outPath); rerr == nil {
			data = b
		} else {
			data = nil
		}
	}
	findings, err := r.parse(ctx.Target, data)
	if err != nil {
		return domain.CheckResult{}, err
	}
	return domain.CheckResult{Findings: findings}, nil
}

// defaultExec is defined per-platform (exec_linux.go / exec_other.go): on Linux
// it runs the CLI in its own process group and kills the whole group on
// timeout/cancel (NFR-P1, no zombie/orphan children); elsewhere it falls back to
// the plain context-kill. Both append any proxy env (financial mode) to the
// inherited environment; an empty env is a no-op — the tool runs direct.

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
