// Package orchestrator is the platform control plane (PR-1..6, PR-3a): it wires
// the finished data plane (scan Executor + findings pipeline) behind the
// injected seams and drives the Scan FSM. It creates no judgement logic — every
// decision (fetch, check, severity, coverage) belongs to scan/findings; this
// package only schedules, folds, and records.
package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/findings/compliance"
	"github.com/enki-offen/chiller/internal/findings/coverage"
	"github.com/enki-offen/chiller/internal/findings/dedup"
	"github.com/enki-offen/chiller/internal/findings/report"
	"github.com/enki-offen/chiller/internal/findings/store"
	"github.com/enki-offen/chiller/internal/platform"
	"github.com/enki-offen/chiller/internal/scan/exec"
	"github.com/enki-offen/chiller/internal/scan/fetch"
	"github.com/enki-offen/chiller/internal/scan/ingest"
	"github.com/enki-offen/chiller/internal/scan/safety"
)

// Config injects every dependency the orchestrator composes. The boot gate
// (boot.go) produces the Accountant; the composition root (cmd/chiller) picks
// the seam impls and passes them here.
type Config struct {
	Store         platform.TaskStore
	Queue         platform.WorkQueue
	Progress      platform.ProgressReporter
	Ingestor      ingest.Ingestor
	Checkers      []domain.Checker
	Accountant    *coverage.Accountant
	Mapper        compliance.Mapper
	Deduper       dedup.Deduplicator
	ReportBuilder report.Builder
	FindingsStore store.FindingsStore
	Cache         fetch.ResponseCache
	Frameworks    []domain.Framework

	// CacheStats is handed to each per-scan Fetcher so cache hits, misses and
	// backend errors are counted where they happen. nil ⇒ not counted.
	CacheStats *fetch.CacheStats

	// Metrics counts the cleanup the orchestrator performs at the end of a scan
	// (cache wipe, ledger evidence purge). Both are best-effort — a failure does
	// not fail the scan — so these counters are the only signal that they are
	// working. nil ⇒ a throwaway counter set, so tests need not wire one.
	Metrics *platform.Metrics

	// GlobalAllowlist intersects every scan's Approved set (fail-closed, SR-3).
	GlobalAllowlist []string
	// HTTPClient backs the per-scan Fetcher; nil ⇒ a default client.
	HTTPClient *http.Client

	// Workers is the worker-pool size W (PR-3a); <=0 ⇒ 1.
	Workers int

	// Clock and NewID keep the orchestrator pure (time/randomness injected).
	Clock func() domain.Timestamp
	NewID func() domain.ScanID

	// Financial routes ProxyEligible checkers' outbound traffic through ProxyURL
	// (the single authenticated auth-proxy, PR-12). All URL-scoped checkers are
	// ProxyEligible, so the shared per-URL Fetcher uses the proxy uniformly.
	Financial bool
	ProxyURL  string
}

// Orchestrator implements platform.Orchestrator over the injected seams.
type Orchestrator struct {
	cfg        Config
	urlScoped  []domain.Checker
	hostScoped []domain.Checker

	mu         sync.Mutex
	execByID   map[domain.ScanID]exec.Executor
	cancelByID map[domain.ScanID]context.CancelFunc
	ctxByID    map[domain.ScanID]context.Context
	finalizing map[domain.ScanID]bool
}

var _ platform.Orchestrator = (*Orchestrator)(nil)

// New builds an Orchestrator and splits the checkers by scope once (the split is
// scan-independent). Call Start to launch the worker pool.
func New(cfg Config) *Orchestrator {
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.Metrics == nil {
		cfg.Metrics = &platform.Metrics{}
	}
	o := &Orchestrator{
		cfg:        cfg,
		execByID:   map[domain.ScanID]exec.Executor{},
		cancelByID: map[domain.ScanID]context.CancelFunc{},
		ctxByID:    map[domain.ScanID]context.Context{},
	}
	for _, c := range cfg.Checkers {
		if c.Scope() == domain.URLScoped {
			o.urlScoped = append(o.urlScoped, c)
		} else {
			o.hostScoped = append(o.hostScoped, c)
		}
	}
	return o
}

func (o *Orchestrator) CreateScan(ctx context.Context, req platform.ScanRequest) (domain.ScanID, error) {
	// Validate + bound the request now (fail-closed) so a bad request is a 400,
	// not a late worker crash. The validated set is recomputed at fan-out.
	if _, err := o.cfg.Ingestor.Ingest(req); err != nil {
		return "", fmt.Errorf("%w: %v", platform.ErrBadRequest, err)
	}
	id := o.cfg.NewID()
	now := o.cfg.Clock()
	s := platform.Scan{
		ID:         id,
		State:      platform.StateCreated,
		Request:    req,
		Frameworks: o.cfg.Frameworks,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := o.cfg.Store.SaveScan(ctx, s); err != nil {
		return "", err
	}
	return id, nil
}

func (o *Orchestrator) StartScan(ctx context.Context, id domain.ScanID) error {
	s, err := o.cfg.Store.LoadScan(ctx, id)
	if err != nil {
		return fmt.Errorf("%w: %v", platform.ErrNotFound, err)
	}
	ok, err := o.cfg.Store.CompareAndSetState(ctx, id, platform.StateCreated, platform.StateRunning)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("start: scan %q not in created state", id)
	}

	set, err := o.cfg.Ingestor.Ingest(s.Request)
	if err != nil {
		return fmt.Errorf("%w: %v", platform.ErrBadRequest, err)
	}

	// Build the per-scan Executor (approval set + rate + proxy transport are
	// scan-scoped). Register a scan-scoped ctx so StopScan can cancel in-flight I/O.
	o.buildExecutor(id, s.Request, set.Options)

	items := fanOut(id, set, len(o.urlScoped) > 0, len(o.hostScoped) > 0)
	if err := o.cfg.Store.AppendWorkItems(ctx, id, items); err != nil {
		return err
	}
	if err := o.cfg.Progress.Init(ctx, id, len(items)); err != nil {
		return err
	}
	for _, it := range items {
		if err := o.cfg.Queue.Enqueue(ctx, it); err != nil {
			return err
		}
	}
	return nil
}

func (o *Orchestrator) GetStatus(ctx context.Context, id domain.ScanID) (platform.ProgressSnapshot, error) {
	s, err := o.cfg.Store.LoadScan(ctx, id)
	if err != nil {
		return platform.ProgressSnapshot{}, fmt.Errorf("%w: %v", platform.ErrNotFound, err)
	}
	snap, err := o.cfg.Progress.Snapshot(ctx, id)
	if err != nil {
		return platform.ProgressSnapshot{}, err
	}
	snap.ScanID = id
	snap.State = s.State // overlay the FSM state (the reporter tracks only counts)
	return snap, nil
}

// GetReport assembles the report a caller sees by joining its two owners: the
// TaskStore holds the skeleton (coverage, framework labels, mapping revision) and
// the FindingsStore holds the findings. This is the ONLY place the two are joined.
//
// The findings from the FindingsStore always win, even if the TaskStore handed
// back a report that already had some. The in-memory TaskStore keeps whatever it
// was given, while the Mongo one strips the findings before writing — so without
// an unconditional overwrite, what a caller got back would depend on which pair of
// backends happened to be wired, and the mem/Mongo mix would disagree with itself.
// One owner, one answer.
func (o *Orchestrator) GetReport(ctx context.Context, id domain.ScanID) (report.Report, bool, error) {
	if _, err := o.cfg.Store.LoadScan(ctx, id); err != nil {
		return report.Report{}, false, fmt.Errorf("%w: %v", platform.ErrNotFound, err)
	}
	rep, ok, err := o.cfg.Store.LoadReport(ctx, id)
	if err != nil || !ok {
		return rep, ok, err
	}
	findings, err := o.cfg.FindingsStore.LoadFindings(ctx, id)
	if err != nil {
		return report.Report{}, false, fmt.Errorf("report %q: load findings: %w", id, err)
	}
	rep.Findings = findings
	return rep, true, nil
}

// buildExecutor constructs and registers the per-scan Executor + scan ctx.
func (o *Orchestrator) buildExecutor(id domain.ScanID, req platform.ScanRequest, opts ingest.ScanOptions) {
	gate := safety.NewApprovalGate(req.Approved, o.cfg.GlobalAllowlist)
	lim := safety.NewRateLimiter(opts.RatePerHost, opts.Burst)

	var fetchOpts []fetch.Option
	if o.cfg.Financial && o.cfg.ProxyURL != "" {
		// All URL-scoped checkers are ProxyEligible, so the shared Fetcher routes
		// through the single auth-proxy uniformly (PR-12c). A mixed URL-scoped set
		// would require splitting the shared fetch — not the case in the MVP.
		fetchOpts = append(fetchOpts, fetch.WithProxy(o.cfg.ProxyURL))
	}
	if o.cfg.CacheStats != nil {
		fetchOpts = append(fetchOpts, fetch.WithCacheStats(o.cfg.CacheStats))
	}
	f := fetch.NewFetcher(gate, lim, o.cfg.Cache, o.cfg.HTTPClient, fetchOpts...)
	disc := fetch.NewStaticDiscoverer(gate)
	executor := exec.New(o.cfg.Checkers, f, disc, gate)

	scanCtx, cancel := context.WithCancel(context.Background())
	o.mu.Lock()
	o.execByID[id] = executor
	o.ctxByID[id] = scanCtx
	o.cancelByID[id] = cancel
	o.mu.Unlock()
}

func (o *Orchestrator) executorFor(id domain.ScanID) (exec.Executor, context.Context, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.execByID[id]
	return e, o.ctxByID[id], ok
}
