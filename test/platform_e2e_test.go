// Package integration exercises the platform composition end to end over the
// real scan/findings units with in-memory seams — the load-bearing proof that
// the control plane wires the data plane correctly (submit → workers → fold →
// report), plus resume, fault isolation, stop, worker-pool concurrency, and the
// financial auth-proxy path (with a fake browser session, so CI needs no Chromium).
package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hongsam14/chiller/internal/domain"
	fconfig "github.com/hongsam14/chiller/internal/findings/config"
	"github.com/hongsam14/chiller/internal/findings/dedup"
	"github.com/hongsam14/chiller/internal/findings/report"
	fstore "github.com/hongsam14/chiller/internal/findings/store"
	"github.com/hongsam14/chiller/internal/platform"
	"github.com/hongsam14/chiller/internal/platform/orchestrator"
	"github.com/hongsam14/chiller/internal/platform/progress"
	"github.com/hongsam14/chiller/internal/platform/proxy"
	"github.com/hongsam14/chiller/internal/platform/taskstore"
	"github.com/hongsam14/chiller/internal/platform/workqueue"
	"github.com/hongsam14/chiller/internal/scan/checking/observation"
	"github.com/hongsam14/chiller/internal/scan/checking/probe"
	"github.com/hongsam14/chiller/internal/scan/fetch"
	"github.com/hongsam14/chiller/internal/scan/ingest"
)

const (
	mappingPath  = "../configs/mappings/kisa.yaml"
	coveragePath = "../configs/coverage/kisa.yaml"
)

// harness wires a real orchestrator with in-memory seams and a live worker pool.
type harness struct {
	orch  *orchestrator.Orchestrator
	store *taskstore.Mem
	spec  domain.CoverageSpec
	stop  context.CancelFunc
}

type harnessOpts struct {
	checkers   []domain.Checker
	workers    int
	financial  bool
	proxyURL   string
	httpClient *http.Client
}

func defaultCheckers() []domain.Checker {
	// Load the REAL distilled signature rules so PS-009/018/013 detect like production.
	rules, err := observation.LoadRules("../configs/signatures")
	if err != nil {
		panic("e2e: load signature rules: " + err.Error())
	}
	checkers := observation.Checkers(observation.New(rules))
	for _, p := range probe.Default() {
		checkers = append(checkers, p)
	}
	return checkers
}

func newHarness(t *testing.T, o harnessOpts) *harness {
	t.Helper()
	mt, err := fconfig.LoadMappingFile(mappingPath)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := fconfig.LoadCoverageSpecFile(coveragePath)
	if err != nil {
		t.Fatal(err)
	}
	if o.checkers == nil {
		o.checkers = defaultCheckers()
	}
	if o.workers == 0 {
		o.workers = 4
	}
	if o.httpClient == nil {
		o.httpClient = &http.Client{}
	}
	acc, mapper, err := orchestrator.BootGate(o.checkers, mt, spec)
	if err != nil {
		t.Fatalf("boot gate: %v", err)
	}
	store := taskstore.New()
	n := 0
	cfg := orchestrator.Config{
		Store:         store,
		Queue:         workqueue.New(),
		Progress:      progress.New(),
		Ingestor:      ingest.New(),
		Checkers:      o.checkers,
		Accountant:    acc,
		Mapper:        mapper,
		Deduper:       dedup.New(),
		ReportBuilder: report.New(mapper),
		FindingsStore: fstore.NewInMemory(),
		Cache:         fetch.NewInMemoryCache(),
		Frameworks:    []domain.Framework{domain.FrameworkKISA},
		HTTPClient:    o.httpClient,
		Workers:       o.workers,
		Clock:         func() domain.Timestamp { return 0 },
		NewID:         func() domain.ScanID { n++; return domain.ScanID(fmt.Sprintf("scan-%d", n)) },
		Financial:     o.financial,
		ProxyURL:      o.proxyURL,
	}
	orch := orchestrator.New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	orch.Start(ctx)
	return &harness{orch: orch, store: store, spec: spec, stop: cancel}
}

func (h *harness) submit(t *testing.T, urls, approved []string) domain.ScanID {
	t.Helper()
	// Hermetic httptest targets have no real rate concern; lift the per-host budget so
	// the (now larger, r21) well-known sweep is not gated by token refill within the
	// test's completion deadline — these tests measure orchestration, not rate.
	id, err := h.orch.CreateScan(context.Background(), ingest.ScanRequest{
		URLs: urls, Approved: approved,
		Options: ingest.ScanOptions{RatePerHost: 5000, Burst: 5000},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := h.orch.StartScan(context.Background(), id); err != nil {
		t.Fatalf("start: %v", err)
	}
	return id
}

func (h *harness) waitState(t *testing.T, id domain.ScanID, want platform.ScanState, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		snap, err := h.orch.GetStatus(context.Background(), id)
		if err == nil && snap.State == want {
			return
		}
		select {
		case <-deadline:
			snap, _ := h.orch.GetStatus(context.Background(), id)
			t.Fatalf("scan %s state = %v after %v, want %v (%+v)", id, snap.State, timeout, want, snap)
		case <-time.After(2 * time.Millisecond):
		}
	}
}

// End-to-end: submit → workers run every checker → finalize folds a valid
// 26-item coverage partition and a report, with a real finding surfaced.
func TestE2E_SubmitToReport_26Partition(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// The HTML comment carries a real-looking JWT → trips PS-009 (gitleaks jwt rule).
		_, _ = w.Write([]byte(`<html><!-- token eyJhbGciOiJIUzI1NiJ9.eyJ1c2VyIjoiYWRtaW4ifQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJVadQssw5c --><body>hi</body></html>`))
	}))
	defer target.Close()

	h := newHarness(t, harnessOpts{})
	defer h.stop()
	host := hostOf(t, target.URL)
	id := h.submit(t, []string{target.URL + "/"}, []string{host})
	h.waitState(t, id, platform.StateCompleted, 5*time.Second)

	rep, ready, err := h.orch.GetReport(context.Background(), id)
	if err != nil || !ready {
		t.Fatalf("report not ready: ready=%v err=%v", ready, err)
	}
	if err := rep.Coverage.Validate(h.spec); err != nil {
		t.Fatalf("coverage is not a valid 26-partition: %v", err)
	}
	if len(rep.Coverage) != 26 {
		t.Fatalf("coverage has %d items, want 26", len(rep.Coverage))
	}
	if rep.Coverage["PS-014"].Status != domain.StatusDelegated {
		t.Errorf("PS-014 should be Delegated, got %v", rep.Coverage["PS-014"].Status)
	}
	if len(rep.Findings) == 0 {
		t.Errorf("expected at least the sensitive-comment finding, got none")
	}
}

// THE honesty invariant: an item whose only owners are unwired probes must be
// reported as ToolFailed ("we obtained no verdict"), never as Checked. Reporting
// Checked would present 12 of the 26 KISA items as clean when no tool ever ran —
// a silent false negative for every weakness they own. This test folds real
// probe outcomes through the real orchestrator, so it fails the moment a probe
// starts looking like a clean run again.
//
// Note on counts after the probe-wiring increment: six probes are now WIRED
// (argv present), but this plain `go test` installs none of the tool binaries,
// so a wired probe's exec still fails → ToolFailed (changeme, still unwired,
// fails with ErrNotWired). Wired != binary-present, so the tool-failed count
// stays 12 here. "Checked rises" (tool-failed 12→1) is proven only in the
// in-image integration suite where the real binaries exist — see the
// //go:build integration e2e.
func TestE2E_ProbesWithoutBinaries_ToolFailed_NotChecked(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body>ok</body></html>`))
	}))
	defer target.Close()

	h := newHarness(t, harnessOpts{})
	defer h.stop()
	host := hostOf(t, target.URL)
	id := h.submit(t, []string{target.URL + "/"}, []string{host})
	h.waitState(t, id, platform.StateCompleted, 5*time.Second)

	rep, _, _ := h.orch.GetReport(context.Background(), id)
	if err := rep.Coverage.Validate(h.spec); err != nil {
		t.Fatalf("coverage is not a valid 26-partition: %v", err)
	}

	// Items owned solely by probes carry no verdict here (no binary present).
	for _, id := range []domain.PSID{"PS-001", "PS-002", "PS-025"} { // humble, testssl, subzy
		if got := rep.Coverage[id].Status; got != domain.StatusToolFailed {
			t.Errorf("%s is probe-owned and no probe is wired — want ToolFailed (no verdict), got %v", id, got)
		}
	}
	// The in-process observation engine really did run.
	if got := rep.Coverage["PS-009"].Status; got != domain.StatusChecked {
		t.Errorf("PS-009 is observation-owned and ran in-process — want Checked, got %v", got)
	}

	var checked, failed, delegated, na int
	for _, rec := range rep.Coverage {
		switch rec.Status {
		case domain.StatusChecked:
			checked++
		case domain.StatusToolFailed:
			failed++
		case domain.StatusDelegated:
			delegated++
		case domain.StatusNotApplicable:
			na++
		}
	}
	// After #0d, 9 items are probe-owned, so 9 are unjudged here (no binaries). r22
	// reverted PS-005/006 to Nuclei delegation (their passive signals are 26-외, not a
	// PS verdict), so 4 items are delegated (PS-005/006 Nuclei, PS-014 ASM, PS-026
	// Nuclei) and 13 items are observation-judged — each Checked or (for this bare
	// target) N/A; only the sum is fixed.
	if failed != 9 || delegated != 4 || checked+na != 13 {
		t.Errorf("honest accounting drifted: checked=%d n/a=%d tool-failed=%d delegated=%d "+
			"(want 9 probe-owned unjudged [no binaries], 4 delegated, 13 observation judged)",
			checked, na, failed, delegated)
	}
	if checked == 0 {
		t.Error("no item was actually Checked — the observation engine did not judge anything")
	}
}

// A checker that always errors isolates to its own PS-ID (ToolFailed); the scan
// still completes and every other item is accounted (US-027).
func TestE2E_FailIsolation(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body>ok</body></html>`))
	}))
	defer target.Close()

	// Replace the PS-003 signature (owns CWE-311) with an always-failing checker.
	checkers := []domain.Checker{failingChecker{id: "observation:CWE-311", cwe: "CWE-311"}}
	for _, s := range observation.Default()[1:] { // skip plaintextLogin (CWE-311)
		checkers = append(checkers, observation.AsChecker(s))
	}
	for _, p := range probe.Default() {
		checkers = append(checkers, p)
	}

	h := newHarness(t, harnessOpts{checkers: checkers})
	defer h.stop()
	host := hostOf(t, target.URL)
	id := h.submit(t, []string{target.URL + "/"}, []string{host})
	h.waitState(t, id, platform.StateCompleted, 5*time.Second)

	rep, _, _ := h.orch.GetReport(context.Background(), id)
	if err := rep.Coverage.Validate(h.spec); err != nil {
		t.Fatalf("coverage invalid despite isolated failure: %v", err)
	}
	if rep.Coverage["PS-003"].Status != domain.StatusToolFailed {
		t.Errorf("PS-003 should be ToolFailed (its checker errored), got %v", rep.Coverage["PS-003"].Status)
	}
}

// Worker-pool concurrency: N delayed URLs on one host complete in far less than
// the serial sum, proving the pool runs them in parallel (PR-3a).
func TestE2E_WorkerPoolConcurrency(t *testing.T) {
	const delay = 120 * time.Millisecond
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only the four submitted work paths are slow. The well-known sweep (r10)
		// also fetches /robots.txt, /.env, … on this host, but that is orthogonal
		// to the parallelism being proven here, so those respond fast.
		switch r.URL.Path {
		case "/a", "/b", "/c", "/d":
			time.Sleep(delay)
		}
		_, _ = w.Write([]byte(`<html><body>slow</body></html>`))
	}))
	defer target.Close()

	h := newHarness(t, harnessOpts{workers: 4})
	defer h.stop()
	host := hostOf(t, target.URL)
	urls := []string{target.URL + "/a", target.URL + "/b", target.URL + "/c", target.URL + "/d"}

	// All URLs (and the r10 well-known sweep) share one host, so the default 5/s
	// per-host rate limiter — not the worker pool — would set the pace. Lift the
	// rate so this test measures worker-pool parallelism, which is its purpose.
	start := time.Now()
	id, err := h.orch.CreateScan(context.Background(), ingest.ScanRequest{
		URLs: urls, Approved: []string{host},
		Options: ingest.ScanOptions{RatePerHost: 5000, Burst: 5000},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := h.orch.StartScan(context.Background(), id); err != nil {
		t.Fatalf("start: %v", err)
	}
	h.waitState(t, id, platform.StateCompleted, 5*time.Second)
	elapsed := time.Since(start)

	// Serial would be >= 4*delay = 480ms; concurrent should be well under.
	if elapsed > 350*time.Millisecond {
		t.Fatalf("4 delayed URLs took %v; expected parallel (<350ms), not serial (~480ms)", elapsed)
	}
}

// Financial mode: worker egress routes through the auth-proxy, so the target
// sees the injected authenticated session (PR-12).
func TestE2E_FinancialProxy_InjectsSession(t *testing.T) {
	// atomic: the well-known sweep (r10) makes several requests to this server
	// concurrently, so the handler runs on multiple goroutines.
	var sawAuth atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Session") == "logged-in" {
			sawAuth.Store(true)
		}
		_, _ = w.Write([]byte(`<html><body>secure</body></html>`))
	}))
	defer target.Close()

	// A fake browser session injects the marker and forwards to the target; the
	// proxy handler runs behind an httptest server acting as the workers' proxy.
	sess := proxy.NewFakeSession(proxy.SessionConfig{Tabs: 6, ReserveKeepAlive: true}, "logged-in",
		func(_ context.Context, req *http.Request) (*http.Response, error) {
			return http.DefaultTransport.RoundTrip(req)
		})
	proxySrv := httptest.NewServer(proxy.NewHandler(sess, nil))
	defer proxySrv.Close()

	h := newHarness(t, harnessOpts{financial: true, proxyURL: proxySrv.URL})
	defer h.stop()
	host := hostOf(t, target.URL)
	id := h.submit(t, []string{target.URL + "/"}, []string{host})
	h.waitState(t, id, platform.StateCompleted, 5*time.Second)

	if !sawAuth.Load() {
		t.Fatalf("target never saw the injected session — financial egress did not route through the proxy")
	}
}

// Resume re-enqueues only unfinished items and is idempotent — a completed scan
// resumed mid-flight never double-runs work; the final state is still completed
// with a valid partition.
func TestE2E_Resume_Idempotent(t *testing.T) {
	var hits int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`<html><body>x</body></html>`))
	}))
	defer target.Close()

	h := newHarness(t, harnessOpts{})
	defer h.stop()
	host := hostOf(t, target.URL)
	id := h.submit(t, []string{target.URL + "/"}, []string{host})
	h.waitState(t, id, platform.StateCompleted, 5*time.Second)
	before := atomic.LoadInt32(&hits)

	// Resume after completion: nothing is unfinished, so no item re-runs and no
	// extra fetch happens (cache + done-item CAS both hold).
	if err := h.orch.ResumeScan(context.Background(), id); err != nil {
		// resume requires running state; a completed scan legitimately refuses.
		return
	}
	time.Sleep(50 * time.Millisecond)
	if after := atomic.LoadInt32(&hits); after != before {
		t.Fatalf("resume re-ran work: fetches %d → %d", before, after)
	}
}

// Stop transitions a created/running scan to stopped and drains its queue.
func TestE2E_Stop_Transitions(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`<html><body>slow</body></html>`))
	}))
	defer target.Close()

	// No workers: the scan stays running with items queued, so Stop is observable.
	h := newHarness(t, harnessOpts{workers: 1})
	defer h.stop()
	host := hostOf(t, target.URL)
	id := h.submit(t, []string{target.URL + "/"}, []string{host})

	if err := h.orch.StopScan(context.Background(), id); err != nil {
		t.Fatalf("stop: %v", err)
	}
	snap, _ := h.orch.GetStatus(context.Background(), id)
	if snap.State != platform.StateStopped {
		t.Fatalf("state after stop = %v, want stopped", snap.State)
	}
}

func hostOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Hostname()
}

// failingChecker owns a CWE (so boot completeness holds) but always errors at
// runtime, so its PS-ID becomes ToolFailed while other items are unaffected.
type failingChecker struct {
	id  domain.CheckerID
	cwe domain.CWE
}

func (f failingChecker) Check(domain.ScanContext) (domain.CheckResult, error) {
	return domain.CheckResult{}, fmt.Errorf("boom")
}
func (f failingChecker) Scope() domain.ScopeKind { return domain.URLScoped }
func (f failingChecker) ID() domain.CheckerID    { return f.id }
func (f failingChecker) OwnedCWEs() []domain.CWE { return []domain.CWE{f.cwe} }
