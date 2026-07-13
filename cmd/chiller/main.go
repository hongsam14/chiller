// Command chiller is the passive-scanner composition root (PR-8/11b/12). It is
// the ONLY place that picks concrete seam implementations and wires the data
// plane (scan/findings) to the control plane (platform). It parses the role
// flag, loads config, runs the boot completeness gate (exit non-zero on
// failure), assembles the in-memory MVP seams, and starts by role.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/enki-offen/chiller/internal/crypto"
	"github.com/enki-offen/chiller/internal/domain"
	fconfig "github.com/enki-offen/chiller/internal/findings/config"
	"github.com/enki-offen/chiller/internal/findings/dedup"
	"github.com/enki-offen/chiller/internal/findings/report"
	fstore "github.com/enki-offen/chiller/internal/findings/store"
	"github.com/enki-offen/chiller/internal/platform"
	"github.com/enki-offen/chiller/internal/platform/api"
	"github.com/enki-offen/chiller/internal/platform/orchestrator"
	"github.com/enki-offen/chiller/internal/platform/progress"
	"github.com/enki-offen/chiller/internal/platform/proxy"
	"github.com/enki-offen/chiller/internal/platform/taskstore"
	"github.com/enki-offen/chiller/internal/platform/workqueue"
	"github.com/enki-offen/chiller/internal/scan/checking/observation"
	"github.com/enki-offen/chiller/internal/scan/checking/probe"
	"github.com/enki-offen/chiller/internal/scan/fetch"
	"github.com/enki-offen/chiller/internal/scan/ingest"
	"github.com/enki-offen/chiller/internal/scan/safety"
)

type flags struct {
	role          string
	addr          string
	token         string
	mappingPath   string
	coveragePath  string
	store         string
	queue         string
	mongoURI      string
	mongoDB       string
	valkeyAddr    string
	findingsStore string
	cache         string
	cacheAddr     string
	cacheTTL      time.Duration
	findingsTTL   time.Duration
	workers       int
	financial     bool
	browserTabs   int
	storageState  string
	proxyAddr     string
}

func main() {
	var f flags
	flag.StringVar(&f.role, "role", "all", "role: all|manager|worker|proxy")
	flag.StringVar(&f.addr, "addr", "127.0.0.1:8080", "ScanAPI listen address")
	flag.StringVar(&f.token, "token", "", "static Bearer token (required for API roles)")
	flag.StringVar(&f.mappingPath, "mapping", "configs/mappings/kisa.yaml", "CWE→framework mapping file")
	flag.StringVar(&f.coveragePath, "coverage", "configs/coverage/kisa.yaml", "coverage spec file")
	flag.StringVar(&f.store, "store", "mem", "task store backend: mem|mongo")
	flag.StringVar(&f.queue, "queue", "mem", "work queue backend: mem|valkey")
	flag.StringVar(&f.mongoURI, "mongo-uri", "", "Mongo connection URI (or env CHILLER_MONGO_URI); required for --store=mongo")
	flag.StringVar(&f.mongoDB, "mongo-db", "chiller", "Mongo database name")
	flag.StringVar(&f.valkeyAddr, "valkey-addr", "", "Valkey host:port (or env CHILLER_VALKEY_ADDR); required for --queue=valkey")
	flag.StringVar(&f.findingsStore, "findings-store", "mem", "findings store backend: mem|mongo (mongo reuses --mongo-uri)")
	flag.StringVar(&f.cache, "cache", "mem", "response cache backend: mem|valkey")
	flag.StringVar(&f.cacheAddr, "cache-addr", "", "Valkey CACHE host:port (or env CHILLER_VALKEY_CACHE_ADDR); a SEPARATE instance from the queue")
	flag.DurationVar(&f.cacheTTL, "cache-ttl", fetch.DefaultCacheTTL, "TTL on cached responses — the backstop if a process dies before it can wipe them")
	flag.DurationVar(&f.findingsTTL, "findings-ttl", 0, "expire stored findings after this long; 0 = keep indefinitely")
	flag.IntVar(&f.workers, "workers", 16, "worker pool size W")
	flag.BoolVar(&f.financial, "financial", false, "financial mode: route ProxyEligible checkers through the auth-proxy")
	flag.IntVar(&f.browserTabs, "browser-tabs", 6, "auth-proxy WORK tab pool size T (login keep-alive tab excluded)")
	flag.StringVar(&f.storageState, "storage-state", "", "path to the pre-authenticated Playwright storageState")
	flag.StringVar(&f.proxyAddr, "proxy-addr", "127.0.0.1:8889", "auth-proxy listen address (financial mode)")
	flag.Parse()

	if err := run(f); err != nil {
		log.Fatalf("chiller: %v", err)
	}
}

func run(f flags) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The proxy role runs only the auth-proxy — no boot gate / orchestrator.
	if f.role == "proxy" {
		return runProxy(ctx, f)
	}

	// --- config + boot gate (PR-6): refuse to start unless all 26 items covered.
	mt, err := fconfig.LoadMappingFile(f.mappingPath)
	if err != nil {
		return fmt.Errorf("load mapping: %w", err)
	}
	spec, err := fconfig.LoadCoverageSpecFile(f.coveragePath)
	if err != nil {
		return fmt.Errorf("load coverage spec: %w", err)
	}
	// In financial mode, route ProxyEligible probes' subprocesses through the
	// auth-proxy via HTTP(S)_PROXY (testssl stays direct — transport-driven, PR-12c).
	// The native URL-scoped fetch is routed separately via the orchestrator's
	// ProxyURL below; here we handle the external-CLI probes.
	probeAdapters := probe.Default()
	if f.financial {
		probe.ConfigureProxy(probeAdapters, "http://"+f.proxyAddr)
	}
	checkers := append(observation.Checkers(observation.Default()), toCheckers(probeAdapters)...)
	acc, mapper, err := orchestrator.BootGate(checkers, mt, spec)
	if err != nil {
		return err // boot completeness failed → exit non-zero (fail-closed)
	}
	log.Printf("boot gate OK: 26 KISA items covered")

	// --- select store/queue backends (PR-11b: composition root is the ONLY place
	// that picks concrete seam impls). mem is the default; mongo/valkey are the
	// durable Infra backends behind the identical interfaces.
	be, closeBackends, err := buildBackends(ctx, f)
	if err != nil {
		return err
	}
	defer closeBackends()
	health := be.health

	// --- assemble seams + findings pipeline.
	metrics := &platform.Metrics{}
	cfg := orchestrator.Config{
		Store:         be.store,
		Queue:         be.queue,
		Progress:      progress.New(),
		Ingestor:      ingest.New(),
		Checkers:      checkers,
		Accountant:    acc,
		Mapper:        mapper,
		Deduper:       dedup.New(),
		ReportBuilder: report.New(mapper),
		FindingsStore: be.findings,
		Cache:         be.cache,
		Metrics:       metrics,
		CacheStats:    be.stats,
		Frameworks:    []domain.Framework{domain.FrameworkKISA},
		HTTPClient:    &http.Client{},
		Workers:       f.workers,
		Clock:         func() domain.Timestamp { return domain.Timestamp(time.Now().UnixMilli()) },
		NewID:         newScanID,
		Financial:     f.financial,
	}
	if f.financial {
		cfg.ProxyURL = "http://" + f.proxyAddr
	}
	orch := orchestrator.New(cfg)

	switch f.role {
	case "all":
		if f.financial {
			go func() {
				if err := runProxy(ctx, f); err != nil {
					log.Printf("auth-proxy exited: %v", err)
				}
			}()
		}
		// Boot recovery BEFORE workers start: no live workers yet, so a FULL resume
		// (all unfinished, immediate) is safe and recovers prior-crash orphans
		// without waiting for their leases to expire.
		recoverOrphans(ctx, orch, be.store)
		orch.Start(ctx) // worker pool in-process
		// Periodic lease-safe sweep: reclaims ONLY expired-lease inflight items, so
		// it is safe to run alongside live local workers (BR-L4).
		startExpiredSweep(ctx, orch, be.store, 30*time.Second)
		return serveAPI(ctx, f, orch, be, metrics, health)
	case "manager":
		// API + FSM only; workers live elsewhere (the durable Valkey queue feeds
		// them). The lease-safe periodic sweep now recovers a dead worker's items
		// mid-run without clobbering a live worker's (expired-only, BR-L4) —
		// closing the split-topology recovery gap. No full boot resume here, since
		// other workers may be live.
		startExpiredSweep(ctx, orch, be.store, 30*time.Second)
		return serveAPI(ctx, f, orch, be, metrics, health)
	case "worker":
		// Worker pool only; a shared durable queue (Infra) feeds it. With the mem
		// queue this is a no-op process, kept for topology parity.
		orch.Start(ctx)
		<-ctx.Done()
		return nil
	default:
		return fmt.Errorf("unknown role %q (want all|manager|worker|proxy)", f.role)
	}
}

// persistenceStats joins the two halves of the persistence counters. They live in
// two packages that cannot see each other — the scan unit must not import platform
// (NFR-02) — so this, the composition root, is the only place that holds both.
type persistenceStats struct {
	metrics *platform.Metrics
	stats   *fetch.CacheStats
	retry   *fstore.Retry
}

func (p persistenceStats) PersistenceSnapshot() api.PersistenceSnapshot {
	m := p.metrics.Snapshot()
	c := p.stats.Snapshot()
	snap := api.PersistenceSnapshot{
		CacheHits:         c.Hits,
		CacheMisses:       c.Misses,
		CacheErrors:       c.Errors,
		CacheWipes:        m.CacheWipes,
		CacheWipeErrors:   m.CacheWipeErrors,
		LedgerPurgeErrors: m.LedgerPurgeErrors,
	}
	if p.retry != nil {
		snap.FindingsSaveRetries = p.retry.Retries.Load()
		snap.FindingsSaveFailures = p.retry.Failures.Load()
		snap.FindingsSaveLastMS = p.retry.LastMS.Load()
	}
	return snap
}

func serveAPI(ctx context.Context, f flags, orch *orchestrator.Orchestrator, be backends, metrics *platform.Metrics, health []api.HealthCheck) error {
	if f.token == "" {
		return fmt.Errorf("role %q needs --token (fail-closed: an empty token denies all)", f.role)
	}
	h := api.New(api.NewBearerAuthorizer(f.token), orch,
		api.WithHealthChecks(health...),
		api.WithIntrospection(be.queue),
		api.WithScanStore(be.store),
		api.WithPersistenceStats(persistenceStats{metrics: metrics, stats: be.stats, retry: be.retry}),
	)
	// ReadHeaderTimeout bounds header-read time so a client that dribbles headers
	// cannot hold a connection open indefinitely (Slowloris). 10s is generous
	// next to the 20s per-request scan deadline (SR-8).
	srv := &http.Server{Addr: f.addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	log.Printf("ScanAPI listening on %s (role=%s)", f.addr, f.role)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// runProxy starts the single authenticated auth-proxy (financial mode). It holds
// THE one BrowserSession; all workers egress through it (single-session
// invariant, PR-12). Chromium provisioning is a build/Infra concern.
func runProxy(ctx context.Context, f flags) error {
	sess, err := proxy.NewPlaywrightSession(proxy.SessionConfig{
		StorageStatePath: f.storageState,
		Tabs:             f.browserTabs,
		ReserveKeepAlive: true, // reserve a tab for the teammate's login keeper (excluded from T)
	})
	if err != nil {
		return fmt.Errorf("auth-proxy session: %w", err)
	}
	defer func() { _ = sess.Close(context.Background()) }()

	lim := safety.NewRateLimiter(0, 0) // per-scan rate is enforced on the worker side; proxy defaults open
	ap := proxy.NewAuthProxy(sess, lim)
	log.Printf("auth-proxy listening on %s (tabs=%d + 1 keep-alive)", f.proxyAddr, f.browserTabs)
	return ap.Serve(ctx, f.proxyAddr)
}

// buildBackends selects the TaskStore and WorkQueue impls from the flags (PR-11b).
// Secrets prefer environment variables over flags (SECURITY-12). It returns the
// deep-health probes for each durable backend (RESILIENCY-06) and a closer.
// backends is what the composition root resolved from the flags: the four seam
// implementations, the health checks that go with them, and the cache counters the
// introspection endpoint reads.
type backends struct {
	store    platform.TaskStore
	queue    platform.WorkQueue
	findings fstore.FindingsStore
	cache    fetch.ResponseCache
	stats    *fetch.CacheStats
	retry    *fstore.Retry // nil unless a findings store is wrapped
	health   []api.HealthCheck
}

// buildBackends is the ONLY place concrete seam implementations are chosen
// (PR-11b). Everything defaults to the in-memory implementation; the durable ones
// are opted into explicitly, and if one is opted into without what it needs, the
// process refuses to start rather than quietly running with less durability or less
// encryption than the operator asked for (SECURITY-15).
func buildBackends(ctx context.Context, f flags) (backends, func(), error) {
	var (
		b       backends
		closers []func()
	)
	closeAll := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}

	// The durable backends persist finding evidence and whole response bodies, so
	// they need a key. One keyring serves all of them: the AAD binding is what keeps
	// a value sealed for one sink from being opened as another's.
	needsKey := f.store == "mongo" || f.findingsStore == "mongo" || f.cache == "valkey"
	var kr crypto.Keyring
	if needsKey {
		k, err := crypto.NewKeyring(os.Getenv("CHILLER_EVIDENCE_KEY"))
		if err != nil {
			return b, closeAll, fmt.Errorf("a durable backend is enabled, so evidence must be encrypted: %w", err)
		}
		kr = k
	}

	mongoURI := func() string { return firstNonEmpty(os.Getenv("CHILLER_MONGO_URI"), f.mongoURI) }

	switch f.store {
	case "mem":
		b.store = taskstore.New()
	case "mongo":
		uri := mongoURI()
		if uri == "" {
			return b, closeAll, fmt.Errorf("--store=mongo needs --mongo-uri or CHILLER_MONGO_URI")
		}
		ms, err := taskstore.NewMongo(ctx, uri, f.mongoDB, kr)
		if err != nil {
			return b, closeAll, err
		}
		b.store = ms
		b.health = append(b.health, api.HealthCheck{Name: "mongo", Check: ms.Ping})
		closers = append(closers, func() { _ = ms.Close(context.Background()) })
	default:
		return b, closeAll, fmt.Errorf("unknown --store %q (want mem|mongo)", f.store)
	}

	// The findings store shares the Mongo instance and separates by collection
	// (shared-infrastructure Q9=C), so it reuses --mongo-uri rather than taking one
	// of its own.
	switch f.findingsStore {
	case "mem":
		b.findings = fstore.NewInMemory()
	case "mongo":
		uri := mongoURI()
		if uri == "" {
			closeAll()
			return b, func() {}, fmt.Errorf("--findings-store=mongo needs --mongo-uri or CHILLER_MONGO_URI")
		}
		fs, err := fstore.NewMongo(ctx, uri, f.mongoDB, kr, f.findingsTTL)
		if err != nil {
			closeAll()
			return b, func() {}, err
		}
		b.findings = fs
		b.health = append(b.health, api.HealthCheck{Name: "mongo-findings", Check: fs.Ping})
		closers = append(closers, func() { _ = fs.Close(context.Background()) })
	default:
		closeAll()
		return b, func() {}, fmt.Errorf("unknown --findings-store %q (want mem|mongo)", f.findingsStore)
	}
	// The save happens once, at the end of a scan, after all the work is done — so a
	// transient blip there would throw the whole scan away. Retry it.
	b.retry = fstore.WithRetry(b.findings, fstore.DefaultAttempts, fstore.DefaultBackoff)
	b.findings = b.retry

	b.stats = &fetch.CacheStats{}
	switch f.cache {
	case "mem":
		b.cache = fetch.NewInMemoryCache()
	case "valkey":
		addr := firstNonEmpty(os.Getenv("CHILLER_VALKEY_CACHE_ADDR"), f.cacheAddr)
		if addr == "" {
			closeAll()
			return b, func() {}, fmt.Errorf("--cache=valkey needs --cache-addr or CHILLER_VALKEY_CACHE_ADDR")
		}
		if addr == firstNonEmpty(os.Getenv("CHILLER_VALKEY_ADDR"), f.valkeyAddr) {
			closeAll()
			return b, func() {}, fmt.Errorf("--cache-addr must not be the queue's Valkey: the cache evicts under memory pressure and would drop queued work items")
		}
		vc, err := fetch.NewValkeyCache(ctx, addr, kr, fetch.ValkeyOptions{
			Password: os.Getenv("CHILLER_VALKEY_CACHE_PASSWORD"),
			TTL:      f.cacheTTL,
		})
		if err != nil {
			closeAll()
			return b, func() {}, err
		}
		b.cache = vc
		b.health = append(b.health, api.HealthCheck{Name: "valkey-cache", Check: vc.Ping})
		closers = append(closers, func() { _ = vc.Close() })
	default:
		closeAll()
		return b, func() {}, fmt.Errorf("unknown --cache %q (want mem|valkey)", f.cache)
	}
	if f.cache == "mem" && f.role == "worker" {
		// Each worker process would get its own cache, so nothing is ever reused
		// across them — the target gets fetched once per worker instead of once
		// (FR-02). Correct, but not what the flag combination suggests.
		log.Printf("WARNING: --role=worker with --cache=mem: the response cache is per-process, " +
			"so cross-worker fetch reuse (FR-02) does not happen. Point every worker at the same --cache=valkey.")
	}

	switch f.queue {
	case "mem":
		b.queue = workqueue.New()
	case "valkey":
		addr := firstNonEmpty(os.Getenv("CHILLER_VALKEY_ADDR"), f.valkeyAddr)
		if addr == "" {
			closeAll()
			return b, func() {}, fmt.Errorf("--queue=valkey needs --valkey-addr or CHILLER_VALKEY_ADDR")
		}
		host, _ := os.Hostname()
		// Only consuming roles run the queue reader/reaper; a producer-only manager
		// must not steal items it cannot dispatch (it only Enqueues + resumes).
		consume := f.role == "all" || f.role == "worker"
		vq, err := workqueue.NewValkey(ctx, addr, host, workqueue.Options{
			Password: os.Getenv("CHILLER_VALKEY_PASSWORD"),
			Consume:  consume,
		})
		if err != nil {
			closeAll()
			return b, func() {}, err
		}
		b.queue = vq
		b.health = append(b.health, api.HealthCheck{Name: "valkey", Check: vq.Ping})
		closers = append(closers, func() { _ = vq.Close() })
	default:
		closeAll()
		return b, func() {}, fmt.Errorf("unknown --queue %q (want mem|valkey)", f.queue)
	}

	return b, closeAll, nil
}

// recoverOrphans performs BOOT-TIME crash recovery: it lists scans still marked
// running (from a durable store that survived a prior crash) and ResumeScans each
// — re-enqueuing pending∪inflight items and resetting orphaned inflight→pending —
// so a scan whose process died mid-flight is picked up instead of stalling
// forever (the durability gap [1] the durable backends exist to close).
//
// It MUST run BEFORE the local worker pool starts, and is used only in the `all`
// role (single process): there, a crash is a full restart, so every inflight
// ledger row is a genuine orphan and resetting it cannot race a live worker.
//
// NOTE (documented limitation): in the manager/worker SPLIT topology a worker can
// die while the manager lives, and safely recovering that mid-run orphan without
// resetting an item a *different* live worker is actively processing requires
// lease/heartbeat timestamps on the ledger (not yet implemented). The split
// topology therefore does not run this reset; that is a tracked follow-up.
func recoverOrphans(ctx context.Context, orch *orchestrator.Orchestrator, store platform.TaskStore) {
	ids, err := store.ListScansByState(ctx, platform.StateRunning)
	if err != nil {
		log.Printf("boot recovery: list running: %v", err)
		return
	}
	for _, id := range ids {
		if err := orch.ResumeScan(ctx, id); err != nil {
			log.Printf("boot recovery: resume %s: %v", id, err)
			continue
		}
		log.Printf("boot recovery: resumed scan %s", id)
	}
}

// startExpiredSweep periodically reclaims expired-lease inflight items across all
// running scans (BR-L4). It is safe to run in both the `all` and `manager` roles
// because ReclaimExpired only touches items whose worker missed its heartbeats
// (presumed dead) — never one a live worker is still processing. This is what
// makes split-topology mid-run recovery safe.
func startExpiredSweep(ctx context.Context, orch *orchestrator.Orchestrator, store platform.TaskStore, every time.Duration) {
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				ids, err := store.ListScansByState(ctx, platform.StateRunning)
				if err != nil {
					log.Printf("sweep: list running: %v", err)
					continue
				}
				for _, id := range ids {
					if n, err := orch.ReclaimExpired(ctx, id); err != nil {
						log.Printf("sweep: reclaim %s: %v", id, err)
					} else if n > 0 {
						log.Printf("sweep: reclaimed %d expired item(s) for scan %s", n, id)
					}
				}
			}
		}
	}()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func toCheckers(ads []probe.Adapter) []domain.Checker {
	out := make([]domain.Checker, len(ads))
	for i, a := range ads {
		out[i] = a
	}
	return out
}

func newScanID() domain.ScanID {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return domain.ScanID(fmt.Sprintf("scan-%d", time.Now().UnixNano()))
	}
	return domain.ScanID("scan-" + hex.EncodeToString(b[:]))
}
