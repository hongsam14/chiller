package api

import "net/http"

// serveReportUI serves the customer-facing compliance report shell. Like /ui it
// carries no data (it fetches the authz'd /api/report/{id} with a token the user
// supplies), so it is served without authz but WITH strict HTML security headers.
func (h *Handler) serveReportUI(w http.ResponseWriter, _ *http.Request) {
	setHTMLSecurityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(reportHTML))
}

// reportHTML is the self-contained compliance report (no CDN, CSP-compatible). It
// tells the compliance story the ops dashboard cannot: (1) every KISA item is
// accounted for — the partition bar is the evidence that detection is
// framework-driven, not ad-hoc; (2) each item carries its standard badges
// (KISA PS-ID + CWE); (3) items chiller does not judge here are shown as ROUTING
// to another engine (Nuclei/ASM) or as honestly not-applicable — never as a
// silent gap.
const reportHTML = `<!doctype html>
<html lang="ko">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>chiller — 컴플라이언스 리포트</title>
<style>
  :root {
    color-scheme: light dark;
    --bg: #fff; --fg: #1a1a1a; --muted: #6e7781; --line: #d0d7de; --card: #f6f8fa;
    --vuln: #cf222e; --clean: #1a7f37; --deleg: #0969da; --na: #8c959f; --fail: #bc4c00;
    --crit: #a40e26; --high: #cf222e; --med: #bc4c00; --low: #9a6700;
    font-family: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  }
  @media (prefers-color-scheme: dark) {
    :root { --bg: #0d1117; --fg: #e6edf3; --muted: #8b949e; --line: #30363d; --card: #161b22; }
  }
  body { margin: 0; padding: 1.5rem; max-width: 68rem; background: var(--bg); color: var(--fg); }
  h1 { font-size: 1.3rem; margin: 0; }
  h2 { font-size: 1rem; margin: 1.4rem 0 0.6rem; }
  .sub { color: var(--muted); font-size: 0.85rem; margin: 0.2rem 0 0; }
  .row { display: flex; flex-wrap: wrap; gap: 0.6rem; align-items: center; margin: 0.8rem 0; }
  input { padding: 0.4rem; border-radius: 6px; border: 1px solid var(--line); background: var(--bg); color: var(--fg); }
  button { padding: 0.4rem 0.8rem; border-radius: 6px; border: 1px solid var(--line); background: var(--card); color: var(--fg); cursor: pointer; }
  .muted { color: var(--muted); }
  .hero { border: 1px solid var(--line); border-radius: 12px; padding: 1rem 1.2rem; background: var(--card); }
  .hero .big { font-size: 1.5rem; font-weight: 700; }
  .bar { display: flex; height: 1.4rem; border-radius: 7px; overflow: hidden; margin: 0.7rem 0 0.5rem; border: 1px solid var(--line); }
  .bar > span { display: block; }
  .legend { display: flex; flex-wrap: wrap; gap: 0.8rem; font-size: 0.82rem; }
  .legend span::before { content: ""; display: inline-block; width: 0.7rem; height: 0.7rem; border-radius: 2px; margin-right: 0.3rem; vertical-align: -1px; }
  .sw-vuln::before { background: var(--vuln); } .sw-clean::before { background: var(--clean); }
  .sw-deleg::before { background: var(--deleg); } .sw-na::before { background: var(--na); }
  .sw-fail::before { background: var(--fail); }
  .sev { display: inline-flex; gap: 0.5rem; flex-wrap: wrap; font-size: 0.8rem; margin-top: 0.5rem; }
  .sev b { font-weight: 700; }
  .pipe { display: grid; grid-template-columns: 1fr; gap: 0.4rem; }
  .stage { display: flex; align-items: baseline; gap: 0.6rem; padding: 0.4rem 0.6rem; border: 1px solid var(--line); border-radius: 8px; font-size: 0.85rem; }
  .stage .who { font-weight: 600; min-width: 13rem; }
  .stage .cnt { font-variant-numeric: tabular-nums; color: var(--muted); }
  table { border-collapse: collapse; width: 100%; margin-top: 0.4rem; }
  th, td { text-align: left; padding: 0.4rem 0.6rem; border-bottom: 1px solid var(--line); font-size: 0.86rem; vertical-align: top; }
  th { color: var(--muted); font-weight: 600; }
  .badge { display: inline-block; font-size: 0.68rem; padding: 0.05rem 0.35rem; border: 1px solid var(--line); border-radius: 999px; margin-right: 0.25rem; color: var(--muted); white-space: nowrap; }
  .chip { display: inline-block; font-size: 0.78rem; font-weight: 600; padding: 0.06rem 0.45rem; border-radius: 999px; white-space: nowrap; }
  .c-vuln { color: #fff; background: var(--vuln); } .c-clean { color: #fff; background: var(--clean); }
  .c-deleg { color: #fff; background: var(--deleg); } .c-na { color: var(--fg); background: transparent; border: 1px solid var(--na); }
  .c-fail { color: #fff; background: var(--fail); }
  .hint { color: var(--deleg); font-size: 0.74rem; display: block; margin-top: 0.2rem; }
  .cat { color: var(--muted); font-size: 0.75rem; }
  .toggle { color: var(--deleg); font-size: 0.74rem; cursor: pointer; user-select: none; white-space: nowrap; }
  .detail-row td { background: var(--card); }
  .finding { border-left: 3px solid var(--vuln); padding: 0.4rem 0.7rem; margin: 0.35rem 0; }
  .f-head { display: flex; flex-wrap: wrap; gap: 0.4rem; align-items: center; }
  .f-tool { font-size: 0.78rem; font-weight: 600; }
  .f-url { font-size: 0.78rem; color: var(--deleg); word-break: break-all; margin: 0.25rem 0; }
  .f-ev { margin: 0.2rem 0 0; padding: 0.4rem 0.6rem; background: var(--bg); border: 1px solid var(--line); border-radius: 6px; font-size: 0.76rem; white-space: pre-wrap; word-break: break-word; overflow-x: auto; }
  .f-evmeta { margin: 0.15rem 0 0; font-size: 0.72rem; color: var(--deleg); word-break: break-all; }
  .f-evhyp { margin: 0.1rem 0 0.3rem; font-size: 0.72rem; font-style: italic; color: var(--deleg); }
  @media print { .noprint { display: none; } body { padding: 0; } .hero, .stage { break-inside: avoid; } }
</style>
</head>
<body>
<div class="row noprint" style="justify-content:space-between">
  <h1>컴플라이언스 정찰 리포트</h1>
  <button id="print">인쇄 / PDF</button>
</div>
<div class="row noprint">
  <input id="tok" type="password" placeholder="API 토큰 (Bearer)" size="26">
  <input id="scanId" placeholder="scan id" size="26">
  <button id="load">조회</button>
  <span id="status" class="muted"></span>
</div>

<div id="report" style="display:none">
  <div class="hero">
    <div class="big"><span id="hTitle">—</span></div>
    <div class="sub" id="hMeta"></div>
    <div class="bar" id="bar"></div>
    <div class="legend" id="legend"></div>
    <div class="sev" id="sev"></div>
  </div>

  <div id="trunc" class="muted" style="display:none;border:1px solid #b58900;background:#fdf6e3;color:#7a5c00;padding:10px 12px;border-radius:6px;margin:12px 0"></div>

  <h2>탐지 파이프라인</h2>
  <div class="pipe" id="pipe"></div>

  <h2 id="itemsHdr">점검 항목</h2>
  <table>
    <thead><tr><th>항목</th><th>제목</th><th>표준 매핑</th><th>결과</th></tr></thead>
    <tbody id="rows"></tbody>
  </table>
</div>
<div id="empty" class="muted">토큰과 scan id를 입력한 뒤 조회하세요.</div>

<script>
(function () {
  var ENGINE = { "Nuclei": "Nuclei 템플릿 분석 엔진", "ASM": "ASM 공격표면 관리 엔진", "ActiveScan": "능동/AI 동적 스캐너" };
  var RESULT = {
    vulnerable:    { chip: "취약 발견", cls: "c-vuln",  sw: "vuln"  },
    clean:         { chip: "양호",      cls: "c-clean", sw: "clean" },
    delegated:     { chip: "이관",      cls: "c-deleg", sw: "deleg" },
    not_applicable:{ chip: "해당없음",  cls: "c-na",    sw: "na"    },
    tool_failed:   { chip: "점검불가",  cls: "c-fail",  sw: "fail"  }
  };
  var SEG = [
    ["vulnerable","취약 발견","var(--vuln)","sw-vuln"],
    ["clean","양호","var(--clean)","sw-clean"],
    ["delegated","이관","var(--deleg)","sw-deleg"],
    ["not_applicable","해당없음","var(--na)","sw-na"],
    ["tool_failed","점검불가","var(--fail)","sw-fail"]
  ];
  function el(t, cls, txt) { var e = document.createElement(t); if (cls) e.className = cls; if (txt != null) e.textContent = txt; return e; }
  function set(id, v) { document.getElementById(id).textContent = v; }

  function render(d) {
    document.getElementById("empty").style.display = "none";
    document.getElementById("report").style.display = "";
    var s = d.summary || {};
    var counts = { vulnerable: s.vulnerable||0, clean: s.clean||0, delegated: s.delegated||0, not_applicable: s.notApplicable||0, tool_failed: s.toolFailed||0 };
    var total = s.total || 0;
    set("hTitle", (counts.vulnerable > 0 ? "취약점 " + counts.vulnerable + "건 발견" : "발견된 취약점 없음"));
    document.getElementById("hMeta").textContent =
      total + "/" + total + " 항목 전수 회계 · scan " + (d.scanId||"") + (d.mappingRevision ? " · mapping " + d.mappingRevision : "");

    // r17: warn on truncated/partial bodies — a body-based "clean" on these URLs is
    // only a partial result and must not be read as an authoritative clean scan.
    var tr = d.truncatedUrls || [];
    var trDiv = document.getElementById("trunc");
    if (tr.length) {
      trDiv.textContent = "";
      var b = el("b"); b.textContent = "⚠ 일부 응답 바디가 잘림/미디코드 (" + tr.length + "건) — 아래 URL의 바디 기반 판정은 부분 결과입니다:";
      trDiv.appendChild(b);
      var ul = el("ul"); ul.style.margin = "6px 0 0 18px";
      tr.forEach(function (u) { var li = el("li"); li.textContent = u; ul.appendChild(li); });
      trDiv.appendChild(ul);
      trDiv.style.display = "";
    } else {
      trDiv.style.display = "none";
    }

    // partition bar
    var bar = document.getElementById("bar"); bar.textContent = "";
    var legend = document.getElementById("legend"); legend.textContent = "";
    SEG.forEach(function (seg) {
      var n = counts[seg[0]] || 0;
      if (n > 0) { var sp = el("span"); sp.style.background = seg[2]; sp.style.width = (n/total*100) + "%"; sp.title = seg[1] + " " + n; bar.appendChild(sp); }
      var lg = el("span", "sw-" + seg[3].split("-")[1]); lg.textContent = seg[1] + " " + n; legend.appendChild(lg);
    });

    // severity breakdown (only if any vuln)
    var sev = document.getElementById("sev"); sev.textContent = "";
    var bs = s.bySeverity || {};
    if ((s.findings||0) > 0) {
      sev.appendChild(el("span", "muted", "심각도:"));
      ["critical","high","medium","low","info"].forEach(function (k) {
        if (bs[k]) { var e = el("span"); e.innerHTML = "<b>" + bs[k] + "</b> " + k; sev.appendChild(e); }
      });
    }

    // pipeline routing
    var items = d.items || [];
    var pipe = document.getElementById("pipe"); pipe.textContent = "";
    function stage(who, list) {
      if (!list.length) return;
      var st = el("div", "stage");
      st.appendChild(el("span", "who", who));
      st.appendChild(el("span", "cnt", list.length + "개 — " + list.map(function (i){return i.psid;}).join(", ")));
      pipe.appendChild(st);
    }
    stage("🟢 관측 엔진(chiller) 직접 판정", items.filter(function (i){return i.result==="vulnerable"||i.result==="clean";}));
    Object.keys(ENGINE).forEach(function (eng) {
      stage("🔀 " + ENGINE[eng] + " 이관", items.filter(function (i){return i.result==="delegated" && i.delegateTo===eng;}));
    });
    stage("🔀 기타 엔진 이관", items.filter(function (i){return i.result==="delegated" && !ENGINE[i.delegateTo];}));
    stage("⚪ 해당없음 (이 대상에 조건 부재)", items.filter(function (i){return i.result==="not_applicable";}));
    stage("🟠 점검 불가 (도구 미가용)", items.filter(function (i){return i.result==="tool_failed";}));

    // item table
    var fw = d.framework || "KISA";
    set("itemsHdr", "점검 항목 (" + total + ") · " + fw);
    var rows = document.getElementById("rows"); rows.textContent = "";
    items.forEach(function (i) {
      var tr = el("tr");
      var c1 = el("td"); c1.appendChild(el("strong", null, i.psid)); if (i.category) { var cat = el("div","cat", i.category); c1.appendChild(cat); }
      var c2 = el("td"); c2.appendChild(document.createTextNode(i.title || i.psid));
      if (i.description) { var ds = el("div","sub", i.description); c2.appendChild(ds); }
      var c3 = el("td");
      c3.appendChild(el("span","badge", fw + " " + i.psid));
      (i.cwes||[]).forEach(function (c){ if (c && c !== "no-canonical-cwe") c3.appendChild(el("span","badge", c)); });
      var c4 = el("td");
      var r = RESULT[i.result] || RESULT.tool_failed;
      var hasDetail = i.result === "vulnerable" && (i.details||[]).length;
      var chip = el("span", "chip " + r.cls, r.chip + (i.result==="vulnerable" && i.topSeverity ? " · " + i.topSeverity : "") + (i.result==="delegated" && ENGINE[i.delegateTo] ? " → " + i.delegateTo : ""));
      c4.appendChild(chip);
      if (hasDetail) { var tog = el("span","toggle", " ▶ 상세 " + i.details.length); c4.appendChild(tog); }
      if (i.result==="not_applicable" && i.activeHint) c4.appendChild(el("span","hint","능동/AI 스캐너 단계에서 재확인 권장"));
      tr.appendChild(c1); tr.appendChild(c2); tr.appendChild(c3); tr.appendChild(c4);
      rows.appendChild(tr);

      // Drill-down: a full-width detail row per vulnerable item, toggled by the chip.
      if (hasDetail) {
        var dr = el("tr", "detail-row"); dr.style.display = "none";
        var dc = el("td"); dc.colSpan = 4;
        i.details.forEach(function (f) {
          var box = el("div", "finding");
          var head = el("div", "f-head");
          head.appendChild(el("span", "chip c-vuln", f.severity));
          head.appendChild(el("span", "f-tool", f.sourceTool || ""));
          (f.cwes||[]).forEach(function (c){ if (c && c !== "no-canonical-cwe") head.appendChild(el("span","badge", c)); });
          box.appendChild(head);
          if (f.targetUrl) { var u = el("div","f-url"); u.textContent = f.targetUrl; box.appendChild(u); }
          // Evidence items are structured (r20): render the snippet, with small badges
          // for the firing rule and match location when present (probe items have none).
          (f.evidence||[]).forEach(function (ev) {
            box.appendChild(el("pre","f-ev", ev.snippet || ""));
            var meta = [];
            if (ev.rule) meta.push(ev.rule);
            if (ev.location && ev.location.part) {
              meta.push(ev.location.part + (ev.location.name ? ":" + ev.location.name : "")
                + (ev.location.sourceUrl ? " @" + ev.location.sourceUrl : ""));
            }
            if (meta.length) box.appendChild(el("div","f-evmeta", meta.join(" · ")));
            if (ev.hypothesis) box.appendChild(el("div","f-evhyp", ev.hypothesis));
          });
          dc.appendChild(box);
        });
        dr.appendChild(dc); rows.appendChild(dr);
        (function (row, toggle) {
          c4.style.cursor = "pointer";
          c4.addEventListener("click", function () {
            var open = row.style.display === "none";
            row.style.display = open ? "" : "none";
            if (toggle) toggle.textContent = (open ? " ▼ 상세 " : " ▶ 상세 ") + i.details.length;
          });
        })(dr, c4.querySelector(".toggle"));
      }
    });
  }

  function load() {
    var tok = document.getElementById("tok").value.trim();
    var id = document.getElementById("scanId").value.trim();
    if (!tok || !id) return;
    set("status", "조회 중…");
    fetch("/api/report/" + encodeURIComponent(id), { headers: { "Authorization": "Bearer " + tok } })
      .then(function (r) {
        if (r.status === 401) { set("status", "인증 실패 (토큰 확인)"); return null; }
        if (r.status === 202) { set("status", "스캔 진행 중 — 완료 후 다시 조회"); return null; }
        if (r.status === 404) { set("status", "scan을 찾을 수 없음"); return null; }
        if (r.status !== 200) { set("status", "오류 " + r.status); return null; }
        set("status", ""); return r.json();
      })
      .then(function (d) { if (d && d.ready) render(d); })
      .catch(function () { set("status", "네트워크 오류"); });
  }
  document.getElementById("load").addEventListener("click", load);
  document.getElementById("print").addEventListener("click", function () { window.print(); });
})();
</script>
</body>
</html>`
