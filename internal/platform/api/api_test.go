package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/findings/report"
	"github.com/hongsam14/chiller/internal/platform"
)

// fakeOrch is a scripted Orchestrator for handler tests.
type fakeOrch struct {
	created   domain.ScanID
	started   bool
	createErr error
	snap      platform.ProgressSnapshot
	statusErr error
	rep       report.Report
	repReady  bool
	repErr    error
	createdFW domain.Framework
}

func (f *fakeOrch) CreateScan(_ context.Context, _ platform.ScanRequest, fw ...domain.Framework) (domain.ScanID, error) {
	if len(fw) > 0 {
		f.createdFW = fw[0]
	}
	return f.created, f.createErr
}
func (f *fakeOrch) StartScan(_ context.Context, _ domain.ScanID) error  { f.started = true; return nil }
func (f *fakeOrch) ResumeScan(_ context.Context, _ domain.ScanID) error { return nil }
func (f *fakeOrch) StopScan(_ context.Context, _ domain.ScanID) error   { return nil }
func (f *fakeOrch) GetStatus(_ context.Context, _ domain.ScanID) (platform.ProgressSnapshot, error) {
	return f.snap, f.statusErr
}
func (f *fakeOrch) GetReport(_ context.Context, _ domain.ScanID) (report.Report, bool, error) {
	return f.rep, f.repReady, f.repErr
}

func newTestServer(orch platform.Orchestrator) *httptest.Server {
	return httptest.NewServer(New(NewBearerAuthorizer("secret"), orch))
}

func do(t *testing.T, method, url, token, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestPostScan_Unauthorized_FailClosed(t *testing.T) {
	orch := &fakeOrch{}
	srv := newTestServer(orch)
	defer srv.Close()

	// no token
	resp := do(t, http.MethodPost, srv.URL+"/scans", "", `{"urls":["http://x/"]}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token status = %d, want 401", resp.StatusCode)
	}
	// wrong token
	resp = do(t, http.MethodPost, srv.URL+"/scans", "wrong", `{"urls":["http://x/"]}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong-token status = %d, want 401", resp.StatusCode)
	}
	if orch.started {
		t.Fatal("orchestrator was invoked despite failed authz")
	}
}

func TestPostScan_HappyPath_202(t *testing.T) {
	orch := &fakeOrch{created: "scan-42"}
	srv := newTestServer(orch)
	defer srv.Close()

	resp := do(t, http.MethodPost, srv.URL+"/scans", "secret",
		`{"urls":["http://x/"],"approved":["x"]}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	if !orch.started {
		t.Fatal("StartScan not called")
	}
}

func TestPostScan_BadRequest_400(t *testing.T) {
	orch := &fakeOrch{createErr: fmt.Errorf("bad url: %w", platform.ErrBadRequest)}
	srv := newTestServer(orch)
	defer srv.Close()

	resp := do(t, http.MethodPost, srv.URL+"/scans", "secret", `{"urls":["::bad::"]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestPostScan_MalformedJSON_400(t *testing.T) {
	srv := newTestServer(&fakeOrch{})
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/scans", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Authorization", "Bearer secret")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetReport_RunningYields202(t *testing.T) {
	orch := &fakeOrch{repReady: false}
	srv := newTestServer(orch)
	defer srv.Close()
	resp := do(t, http.MethodGet, srv.URL+"/scans/s/report", "secret", "")
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("running report status = %d, want 202", resp.StatusCode)
	}
}

func TestGetStatus_NotFound_404(t *testing.T) {
	orch := &fakeOrch{statusErr: platform.ErrNotFound}
	srv := newTestServer(orch)
	defer srv.Close()
	resp := do(t, http.MethodGet, srv.URL+"/scans/missing", "secret", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHealthz_NoAuth(t *testing.T) {
	srv := newTestServer(&fakeOrch{})
	defer srv.Close()
	resp := do(t, http.MethodGet, srv.URL+"/healthz", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", resp.StatusCode)
	}
}
