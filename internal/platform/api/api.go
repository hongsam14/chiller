// Package api is the platform ScanAPI (Q3/Q4=A, PR-7): a stdlib net/http
// handler over the Orchestrator, guarded by a fail-closed Bearer Authorizer.
// The Authorizer is a seam — the static-token MVP here is swapped for OIDC/mTLS
// at Infra without touching the handler.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/platform"
	"github.com/enki-offen/chiller/internal/scan/ingest"
)

// BearerAuthorizer authorizes requests against a single static Bearer token
// (MVP). Empty token ⇒ every request is denied (fail-closed): a missing token is
// a misconfiguration, not an open door.
type BearerAuthorizer struct{ token string }

// NewBearerAuthorizer builds a static-token authorizer.
func NewBearerAuthorizer(token string) *BearerAuthorizer { return &BearerAuthorizer{token: token} }

var _ platform.Authorizer = (*BearerAuthorizer)(nil)

// ErrUnauthorized is returned when the Bearer token is missing or wrong.
var ErrUnauthorized = errors.New("api: unauthorized")

func (a *BearerAuthorizer) Authorize(r *http.Request) error {
	if a.token == "" {
		return ErrUnauthorized // misconfigured ⇒ deny all
	}
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return ErrUnauthorized
	}
	got := strings.TrimPrefix(h, prefix)
	// constant-time compare to avoid leaking the token via timing.
	if subtle.ConstantTimeCompare([]byte(got), []byte(a.token)) != 1 {
		return ErrUnauthorized
	}
	return nil
}

// Handler serves the ScanAPI over an Orchestrator.
type Handler struct {
	authz platform.Authorizer
	orch  platform.Orchestrator
	mux   *http.ServeMux
	// introspection (optional, wired via options) — the ops dashboard.
	inspector platform.QueueInspector
	store     platform.TaskStore // for the scan list (nil → endpoint reports unavailable)
	health    []HealthCheck
}

// HealthCheck is one named backend liveness probe for the deep /healthz
// (RESILIENCY-06). Check returns nil when the backend is reachable.
type HealthCheck struct {
	Name  string
	Check func(context.Context) error
}

// Option configures optional Handler capabilities without changing the base
// New(authz, orch) signature that existing callers use.
type Option func(*Handler)

// WithHealthChecks registers backend liveness probes; /healthz then returns 503
// (fail-closed) if any fails, so a load balancer routes away from an instance
// that lost Mongo/Valkey.
func WithHealthChecks(checks ...HealthCheck) Option {
	return func(h *Handler) { h.health = append(h.health, checks...) }
}

// WithIntrospection enables the read-only ops dashboard (/ui + /api/introspect).
// If the queue does not implement QueueInspector, the dashboard degrades to
// "n/a" rather than failing.
func WithIntrospection(q platform.WorkQueue) Option {
	return func(h *Handler) {
		if qi, ok := q.(platform.QueueInspector); ok {
			h.inspector = qi
		}
	}
}

// WithScanStore enables the dashboard scan list (/api/introspect/scans) by giving
// the handler read access to the task store for enumerating scans by state.
func WithScanStore(store platform.TaskStore) Option {
	return func(h *Handler) { h.store = store }
}

// scanRequestDTO is the wire shape for POST /scans (JSON-tagged, so the scan
// unit's ingest structs stay free of transport concerns).
type scanRequestDTO struct {
	URLs     []string `json:"urls"`
	Approved []string `json:"approved"`
	Options  struct {
		RatePerHost int      `json:"ratePerHost"`
		Burst       int      `json:"burst"`
		MaxURLs     int      `json:"maxURLs"`
		Tools       []string `json:"tools"`
	} `json:"options"`
}

func (d scanRequestDTO) toRequest() ingest.ScanRequest {
	return ingest.ScanRequest{
		URLs:     d.URLs,
		Approved: d.Approved,
		Options: ingest.ScanOptions{
			RatePerHost: d.Options.RatePerHost,
			Burst:       d.Options.Burst,
			MaxURLs:     d.Options.MaxURLs,
			Tools:       d.Options.Tools,
		},
	}
}

// New builds the ScanAPI handler.
func New(authz platform.Authorizer, orch platform.Orchestrator, opts ...Option) *Handler {
	h := &Handler{authz: authz, orch: orch, mux: http.NewServeMux()}
	for _, opt := range opts {
		opt(h)
	}
	h.mux.HandleFunc("GET /healthz", h.healthz)
	h.mux.HandleFunc("GET /livez", h.livez)
	h.mux.HandleFunc("POST /scans", h.guard(h.postScan))
	h.mux.HandleFunc("GET /scans/{id}", h.guard(h.getStatus))
	h.mux.HandleFunc("GET /scans/{id}/report", h.guard(h.getReport))
	// Read-only ops dashboard (introspection UI). The data endpoint is authz'd
	// (SECURITY-08); the static shell carries no data and is served with strict
	// security headers (SECURITY-04).
	h.mux.HandleFunc("GET /api/introspect/queue", h.guard(h.introspectQueue))
	h.mux.HandleFunc("GET /api/introspect/scans", h.guard(h.introspectScans))
	h.mux.HandleFunc("GET /ui", h.serveUI)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

// guard wraps a handler with fail-closed authorization.
func (h *Handler) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h.authz.Authorize(r); err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

// healthz is the deep health check (RESILIENCY-06): it runs every registered
// backend probe and fails closed (503) if any is unreachable. With no probes
// registered (mem dev run) it is always ok.
func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	checks := map[string]string{}
	ok := true
	for _, c := range h.health {
		if err := c.Check(ctx); err != nil {
			ok = false
			checks[c.Name] = "unreachable" // no error detail (SECURITY-09: no internals leaked)
		} else {
			checks[c.Name] = "ok"
		}
	}
	code := http.StatusOK
	status := "ok"
	if !ok {
		code = http.StatusServiceUnavailable
		status = "degraded"
	}
	writeJSON(w, code, map[string]any{"status": status, "checks": checks})
}

// livez is the shallow liveness probe (process is up).
func (h *Handler) livez(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) postScan(w http.ResponseWriter, r *http.Request) {
	var dto scanRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed json"})
		return
	}
	id, err := h.orch.CreateScan(r.Context(), dto.toRequest())
	if err != nil {
		if errors.Is(err, platform.ErrBadRequest) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	if err := h.orch.StartScan(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"scanId": string(id)})
}

func (h *Handler) getStatus(w http.ResponseWriter, r *http.Request) {
	id := domain.ScanID(r.PathValue("id"))
	snap, err := h.orch.GetStatus(r.Context(), id)
	if err != nil {
		h.writeOrchErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statusDTO(snap))
}

func (h *Handler) getReport(w http.ResponseWriter, r *http.Request) {
	id := domain.ScanID(r.PathValue("id"))
	rep, ready, err := h.orch.GetReport(r.Context(), id)
	if err != nil {
		h.writeOrchErr(w, err)
		return
	}
	if !ready {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "running"})
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (h *Handler) writeOrchErr(w http.ResponseWriter, err error) {
	if errors.Is(err, platform.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
}

// statusDTO renders a ProgressSnapshot with the state as a string.
func statusDTO(s platform.ProgressSnapshot) map[string]any {
	perChecker := make(map[string]map[string]int, len(s.PerChecker))
	for cid, cc := range s.PerChecker {
		perChecker[string(cid)] = map[string]int{"ok": cc.OK, "failed": cc.Failed}
	}
	return map[string]any{
		"scanId":     string(s.ScanID),
		"state":      s.State.String(),
		"total":      s.Total,
		"done":       s.Done,
		"failed":     s.Failed,
		"inflight":   s.Inflight,
		"perChecker": perChecker,
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
