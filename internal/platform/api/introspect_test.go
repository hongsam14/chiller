package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/platform"
)

// fakeInspector is a scripted QueueInspector for the dashboard tests.
type fakeInspector struct {
	stats platform.QueueStats
	err   error
}

func (f *fakeInspector) Stats(context.Context) (platform.QueueStats, error) {
	return f.stats, f.err
}

func TestServeUI_HeadersAndBody(t *testing.T) {
	h := New(NewBearerAuthorizer("secret"), &fakeOrch{})
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp := do(t, http.MethodGet, srv.URL+"/ui", "", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/ui status = %d, want 200", resp.StatusCode)
	}
	// SECURITY-04 headers must be present on the HTML response.
	for _, hd := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Strict-Transport-Security"} {
		if resp.Header.Get(hd) == "" {
			t.Errorf("missing security header %q on /ui", hd)
		}
	}
	if resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", resp.Header.Get("X-Frame-Options"))
	}
}

func TestIntrospectQueue_AuthzAndStats(t *testing.T) {
	insp := &fakeInspector{stats: platform.QueueStats{Depth: 7, Pending: 2, Drained: 1, Reclaimed: 3,
		Consumers: []platform.ConsumerStat{{Name: "w1", Pending: 2}}}}
	h := New(NewBearerAuthorizer("secret"), &fakeOrch{}, withInspector(insp))
	srv := httptest.NewServer(h)
	defer srv.Close()

	// fail-closed: no token → 401 (SECURITY-08)
	resp := do(t, http.MethodGet, srv.URL+"/api/introspect/queue", "", "")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token introspect = %d, want 401", resp.StatusCode)
	}

	// authorized → stats
	resp = do(t, http.MethodGet, srv.URL+"/api/introspect/queue", "secret", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("introspect = %d, want 200", resp.StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["available"] != true || got["depth"].(float64) != 7 || got["reclaimed"].(float64) != 3 {
		t.Fatalf("stats payload = %+v", got)
	}
}

func TestIntrospectQueue_NoInspector_Degrades(t *testing.T) {
	h := New(NewBearerAuthorizer("secret"), &fakeOrch{}) // no inspector wired
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp := do(t, http.MethodGet, srv.URL+"/api/introspect/queue", "secret", "")
	defer func() { _ = resp.Body.Close() }()
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got["available"] != false {
		t.Fatalf("expected available=false without an inspector, got %+v", got)
	}
}

func TestHealthz_DeepCheck_FailClosed(t *testing.T) {
	failing := HealthCheck{Name: "mongo", Check: func(context.Context) error { return context.DeadlineExceeded }}
	ok := HealthCheck{Name: "valkey", Check: func(context.Context) error { return nil }}
	h := New(NewBearerAuthorizer("secret"), &fakeOrch{}, WithHealthChecks(failing, ok))
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp := do(t, http.MethodGet, srv.URL+"/healthz", "", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("healthz with a failing backend = %d, want 503", resp.StatusCode)
	}
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	checks, _ := got["checks"].(map[string]any)
	if checks["mongo"] != "unreachable" || checks["valkey"] != "ok" {
		t.Fatalf("checks = %+v", checks)
	}
}

// scanLister is a minimal TaskStore for the scan-list test: only the two methods
// the endpoint uses are exercised; the rest panic if called.
type scanLister struct {
	platform.TaskStore // embedded nil — unused methods would panic (none are called)
	running            []domain.ScanID
}

func (s scanLister) ListScansByState(_ context.Context, st platform.ScanState) ([]domain.ScanID, error) {
	if st == platform.StateRunning {
		return s.running, nil
	}
	return nil, nil
}

func TestIntrospectScans_ListsRunning(t *testing.T) {
	orch := &fakeOrch{snap: platform.ProgressSnapshot{ScanID: "s1", State: platform.StateRunning, Total: 3, Done: 1, Inflight: 1}}
	h := New(NewBearerAuthorizer("secret"), orch, WithScanStore(scanLister{running: []domain.ScanID{"s1"}}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	// authz fail-closed
	resp := do(t, http.MethodGet, srv.URL+"/api/introspect/scans", "", "")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token scans = %d, want 401", resp.StatusCode)
	}
	resp = do(t, http.MethodGet, srv.URL+"/api/introspect/scans", "secret", "")
	defer func() { _ = resp.Body.Close() }()
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	scans, _ := got["scans"].([]any)
	if got["available"] != true || len(scans) != 1 {
		t.Fatalf("scans payload = %+v", got)
	}
}

func TestHealthz_NoChecks_OK(t *testing.T) {
	h := New(NewBearerAuthorizer("secret"), &fakeOrch{})
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp := do(t, http.MethodGet, srv.URL+"/healthz", "", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz with no checks = %d, want 200", resp.StatusCode)
	}
}

// withInspector wires a QueueInspector directly (test helper) without needing a
// full WorkQueue impl.
func withInspector(qi platform.QueueInspector) Option {
	return func(h *Handler) { h.inspector = qi }
}
