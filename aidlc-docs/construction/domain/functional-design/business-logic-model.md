# Business Logic Model — Unit `domain` (U0)

**Stage**: 🟢 CONSTRUCTION → `domain` → Functional Design (artifact 3/3) · **Date**: 2026-07-08
**Purpose**: show how the domain types compose across one scan (contract-level, no algorithms), and restate the §7 PBT properties at the type level so the downstream units implement against a fixed spec.

---

## 1. Type composition across a scan (data, not control)

```
 scan unit                         findings unit                         platform unit
 ─────────                         ─────────────                         ─────────────
 Checker.Check(ScanContext)
   ├ ScanContext{Target,Response,DiscoveredJS}
   └▶ []Finding{CWEs,TargetURL,SourceTool,Severity,Evidence}
        │                    (R1 normalize, R2 identity)
        ▼
      Deduplicate  ──(R2 idempotent/commutative, R3 collapse)──▶ []Finding
        ▼
      MapSeverity  ──(R4 total; unknown→Info)─────────────────▶ []Finding (valid Severity)
        ▼
      Project(CWEs,"KISA") ─(R8 totality/N:M/round-trip)──────▶ framework items
        │
   WorkItemOutcome{Findings, Failed[]CheckerID, NotApplicable}
        ▼
      CoverageAccountant ─(R5 totality+disjoint, R6 delegate, R7 tool-failed)─▶ Coverage
        ▼
   Report{ []Finding (CWE axis), Coverage (PSID axis), mappingRevision }
        ▲
   ScanAPI serves Report (platform)
```

Two axes stay separate end to end: **CWE on `Finding`**, **PS-ID on `Coverage`**; `Project` is the only bridge (R8) and cannot move accounting (R5).

---

## 2. Lifecycle of a single item (worked, PS-009 example — r4)

1. Fetcher fetches the page → `HAREntry{Body: html}`; parses `<script src>` (approved hosts) → `DiscoveredJS[]`.
2. `ScanContext{Target, Response: html, DiscoveredJS}` handed to `ObservationEngine.Check`.
3. The PS-009 signature scans `html.Body` + each `DiscoveredJS[i].Body` for sensitive comments/secrets → emits `Finding{CWEs:["CWE-615","CWE-540"], TargetURL, SourceTool:"observation:CWE-615", Severity, Evidence(bounded)}`.
4. If that signature errors → PS-009 = `ToolFailed` (R7 engine exception); other signatures unaffected.
5. findings unit: dedup (R2, tool-scoped) → severity (R4) → `Project(["CWE-615","CWE-540"],"KISA")` yields `{PS-009}`.
6. `CoverageAccountant`: PS-009 → `Checked` (owning checker ran; FQ2). All other PS-IDs get their status; the 26 partition (R5).

---

## 3. PBT property seeds (§7 restated at type level — for downstream units)

| # | Property | Type-level statement | Owner unit |
|---|---|---|---|
| P1 | Normalization oracle/total | `Normalize(fixture)` → known `[]Finding`; never panics on malformed input | findings |
| P2 | Dedup idempotent+commutative | `dedup∘dedup = dedup`; `dedup(a++b) ≡ dedup(b++a)` as sets (R2) | findings |
| P3 | Severity total | `∀ raw: MapSeverity(raw) ∈ {Info..Critical}` (R4; unknown→Info) | findings |
| P4 | Coverage partition | `∀ scan: Checked⊎NotApplicable⊎Delegated⊎ToolFailed = {PS-001..026}` (R5) | findings |
| P5 | Projection totality | `∀ cwe: Project(cwe,F) ≠ ∅ ∨ = {no-canonical}` (R8) | findings |
| P6 | Projection ⇏ coverage | adding framework table ⇒ `Coverage` unchanged (R8) | findings |
| P7 | Mapping round-trip | `∀ item∈Project(f,F): ∃ cwe∈f.CWEs mapping to item` (R8) | findings |

The domain unit **owns the property statements** (this doc); the findings unit implements the generators + assertions at its Functional Design / Code Gen. P1–P7 anchor stories US-014/015/016/020/017/018.

---

## 4. What domain does NOT contain (boundaries)

- **No algorithms**: dedup strategy, anchor regexes, severity tables, mapping-file contents — all in scan/findings units.
- **No I/O**: no fetch, no store, no queue — those are interfaces the data/control units implement.
- **No framework knowledge in `Finding`**: PS-ID/OWASP/CAPEC never appear on `Finding`.
- **Stability contract**: changing any type here is a cross-unit breaking change (unit-dependency §2) — reviewed deliberately.

---

## 5. Construction handoff

With domain fixed, the next unit is **findings** (Q3=A order): it consumes `Finding`, implements P1–P7, and produces `Coverage`/`Report`. The `Checker`/`Finding`/`ScanContext`/`HAREntry`/`Coverage` contracts here are its inputs and outputs — no renegotiation needed.
