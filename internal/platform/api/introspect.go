package api

import (
	"net/http"

	"github.com/enki-offen/chiller/internal/platform"
)

// introspectScans lists running scans plus a bounded number of recently-terminated
// ones, each with its ProgressSnapshot, for the dashboard (Q6=A). Authz-guarded
// (SECURITY-08). Degrades to available=false when no store is wired.
func (h *Handler) introspectScans(w http.ResponseWriter, r *http.Request) {
	setJSONSecurityHeaders(w)
	if h.store == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	const recentCap = 20
	rows := make([]map[string]any, 0)
	add := func(states []platform.ScanState, cap int) {
		for _, st := range states {
			ids, err := h.store.ListScansByState(r.Context(), st)
			if err != nil {
				continue
			}
			n := 0
			for _, id := range ids {
				if cap > 0 && n >= cap {
					break
				}
				snap, err := h.orch.GetStatus(r.Context(), id)
				if err != nil {
					continue
				}
				rows = append(rows, statusDTO(snap))
				n++
			}
		}
	}
	add([]platform.ScanState{platform.StateRunning}, 0) // all running
	add([]platform.ScanState{platform.StateCompleted, platform.StateStopped, platform.StateFailed}, recentCap)
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "scans": rows})
}

// introspectQueue returns the live QueueStats for the ops dashboard. It is
// authz-guarded (SECURITY-08). When the wired queue is not a QueueInspector
// (e.g. a dev mem queue), it reports availability=false rather than erroring.
func (h *Handler) introspectQueue(w http.ResponseWriter, r *http.Request) {
	setJSONSecurityHeaders(w)
	if h.inspector == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	stats, err := h.inspector.Stats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	consumers := make([]map[string]any, 0, len(stats.Consumers))
	for _, c := range stats.Consumers {
		consumers = append(consumers, map[string]any{"name": c.Name, "pending": c.Pending})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available": true,
		"depth":     stats.Depth,
		"pending":   stats.Pending,
		"drained":   stats.Drained,
		"reclaimed": stats.Reclaimed,
		"consumers": consumers,
	})
}

// serveUI serves the dependency-free dashboard shell. The page carries no data
// (it fetches the authz'd endpoints with a token the user supplies), so it is
// served without authz but WITH strict HTML security headers (SECURITY-04).
func (h *Handler) serveUI(w http.ResponseWriter, _ *http.Request) {
	setHTMLSecurityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(dashboardHTML))
}

// setHTMLSecurityHeaders applies the SECURITY-04 baseline to HTML responses. The
// CSP is self-only with no inline-script allowance beyond the single inline
// bundle this page ships (kept minimal; no external origins).
func setHTMLSecurityHeaders(w http.ResponseWriter) {
	hdr := w.Header()
	hdr.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'")
	hdr.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("X-Frame-Options", "DENY")
	hdr.Set("Referrer-Policy", "strict-origin-when-cross-origin")
}

// setJSONSecurityHeaders applies the subset relevant to JSON responses.
func setJSONSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
}

// dashboardHTML is the self-contained ops dashboard: no external CDN, no remote
// assets (CSP-compatible). It polls the authz'd introspection + scan-status
// endpoints using a Bearer token the operator pastes in.
const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>chiller — queue dashboard</title>
<style>
  :root { color-scheme: light dark; font-family: system-ui, sans-serif; }
  body { margin: 0; padding: 1.5rem; max-width: 60rem; }
  h1 { font-size: 1.25rem; margin: 0 0 1rem; }
  .row { display: flex; flex-wrap: wrap; gap: 1rem; margin-bottom: 1rem; }
  .card { border: 1px solid gray; border-radius: 8px; padding: 0.75rem 1rem; min-width: 7rem; }
  .card .n { font-size: 1.6rem; font-weight: 600; }
  .card .l { font-size: 0.75rem; opacity: 0.7; text-transform: uppercase; letter-spacing: 0.04em; }
  input { padding: 0.4rem; border-radius: 6px; border: 1px solid gray; }
  table { border-collapse: collapse; width: 100%; }
  th, td { text-align: left; padding: 0.3rem 0.6rem; border-bottom: 1px solid gray; font-size: 0.9rem; }
  .muted { opacity: 0.6; }
  #scanOut { white-space: pre-wrap; font-family: monospace; font-size: 0.85rem; }
</style>
</head>
<body>
<h1>chiller — queue dashboard</h1>
<p class="muted">Read-only. Paste your API token; it is kept in this tab only.</p>
<div class="row">
  <input id="tok" type="password" placeholder="Bearer token" size="32">
  <button id="go">Connect</button>
  <span id="status" class="muted"></span>
</div>
<div class="row" id="cards">
  <div class="card"><div class="n" id="depth">-</div><div class="l">queue depth</div></div>
  <div class="card"><div class="n" id="pending">-</div><div class="l">pending</div></div>
  <div class="card"><div class="n" id="drained">-</div><div class="l">drained scans</div></div>
  <div class="card"><div class="n" id="reclaimed">-</div><div class="l">reaper reclaims</div></div>
</div>
<h2 style="font-size:1rem">consumers</h2>
<table><thead><tr><th>consumer</th><th>pending</th></tr></thead><tbody id="consumers"></tbody></table>
<h2 style="font-size:1rem">scans</h2>
<table><thead><tr><th>scan</th><th>state</th><th>done/total</th><th>failed</th><th>inflight</th></tr></thead><tbody id="scans"></tbody></table>
<h2 style="font-size:1rem">scan lookup</h2>
<div class="row">
  <input id="scanId" placeholder="scan id" size="32">
  <button id="lookup">Look up</button>
</div>
<div id="scanOut" class="muted">enter a scan id</div>
<script>
(function () {
  var tok = "";
  function hdr() { return { "Authorization": "Bearer " + tok }; }
  function set(id, v) { document.getElementById(id).textContent = v; }
  function refresh() {
    fetch("/api/introspect/queue", { headers: hdr() }).then(function (r) {
      if (r.status === 401) { set("status", "unauthorized"); return null; }
      set("status", "connected");
      return r.json();
    }).then(function (d) {
      if (!d) return;
      if (!d.available) { set("status", "queue introspection n/a (mem queue)"); return; }
      set("depth", d.depth); set("pending", d.pending);
      set("drained", d.drained); set("reclaimed", d.reclaimed);
      var tb = document.getElementById("consumers"); tb.textContent = "";
      (d.consumers || []).forEach(function (c) {
        var tr = document.createElement("tr");
        var a = document.createElement("td"); a.textContent = c.name;
        var b = document.createElement("td"); b.textContent = c.pending;
        tr.appendChild(a); tr.appendChild(b); tb.appendChild(tr);
      });
    }).catch(function () { set("status", "error"); });
    refreshScans();
  }
  function refreshScans() {
    fetch("/api/introspect/scans", { headers: hdr() }).then(function (r) {
      return r.status === 200 ? r.json() : null;
    }).then(function (d) {
      if (!d || !d.available) return;
      var tb = document.getElementById("scans"); tb.textContent = "";
      (d.scans || []).forEach(function (s) {
        var tr = document.createElement("tr");
        [s.scanId, s.state, s.done + "/" + s.total, s.failed, s.inflight].forEach(function (v) {
          var td = document.createElement("td"); td.textContent = v; tr.appendChild(td);
        });
        tb.appendChild(tr);
      });
    }).catch(function () {});
  }
  document.getElementById("go").addEventListener("click", function () {
    tok = document.getElementById("tok").value; refresh();
  });
  document.getElementById("lookup").addEventListener("click", function () {
    var id = document.getElementById("scanId").value;
    if (!id) return;
    fetch("/scans/" + encodeURIComponent(id), { headers: hdr() }).then(function (r) {
      return r.json();
    }).then(function (d) {
      document.getElementById("scanOut").textContent = JSON.stringify(d, null, 2);
    }).catch(function () { document.getElementById("scanOut").textContent = "error"; });
  });
  setInterval(function () { if (tok) refresh(); }, 3000);
})();
</script>
</body>
</html>`

// introspectPersistence reports the persistence counters (Q12=A). The OTel
// exporter of the next increment reads the same numbers from the same place.
func (h *Handler) introspectPersistence(w http.ResponseWriter, r *http.Request) {
	if h.persist == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	snap := h.persist.PersistenceSnapshot()
	writeJSON(w, http.StatusOK, struct {
		Available bool `json:"available"`
		PersistenceSnapshot
	}{Available: true, PersistenceSnapshot: snap})
}
