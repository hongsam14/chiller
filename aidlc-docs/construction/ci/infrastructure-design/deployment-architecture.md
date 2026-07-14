# Deployment Architecture — unit `ci`

**Date**: 2026-07-13

> **Scope caveat, stated up front**: this increment deploys **nothing**. Q12=A scoped it to CI only — no registry push, no k8s apply, no release artifacts. The runtime deployment architecture (single image + role flag, single-region multi-AZ, rolling upgrade with version-pinned rollback, Mongo replica set, Valkey queue + separate Valkey cache) was designed in the `platform` and `persistence` increments and is **unchanged**. See `construction/platform/infrastructure-design/deployment-architecture.md`.
>
> What follows is the deployment architecture **of the pipeline itself** — where it runs, what it touches, and what it is deliberately not allowed to touch.

---

## 1. Execution environment

| Property | Value | Source |
|---|---|---|
| Platform | GitHub Actions | Q1=A |
| Repository | `github.com/hongsam14/Service-Passive-Scanner` (**private**) | created 2026-07-13 |
| Default branch | `main` | local `master` renamed to match remote |
| Runners | GitHub-hosted, `ubuntu-latest` | Q2=A |
| Triggers | `pull_request` → `main`; `push` → `main` | Q3=A |
| Nightly / tag triggers | **none** | C1=D, Q3=A |
| Repository secrets required | **zero** | CI-NFR-05 |

**On `ubuntu-latest` and CI-NFR-01 ("no floating tags")**: the runner *label* is a GitHub-managed environment, not an image we pull, and GitHub provides no pinnable digest for it. The pinning rule is satisfied where it is actionable — every image, action, and tool the pipeline *fetches* is pinned. The runner label is documented here as the one unpinnable input, rather than silently ignored.

## 2. Privilege and blast radius

This is the security-relevant property of the design, so it is stated explicitly:

```
+-------------------------------------------------------------+
|  GitHub Actions runner (ephemeral, GitHub-hosted)           |
|                                                             |
|   reads:   this repository (read-only checkout)             |
|   fetches: pinned Go modules, pinned container images       |
|   starts:  throwaway Mongo + Valkey containers (tests)      |
|   writes:  workflow logs + build artifacts (SBOM, reports)  |
|                                                             |
|   CANNOT:  push an image        (no registry configured)    |
|   CANNOT:  deploy anything      (no cluster credentials)    |
|   CANNOT:  read any secret      (no repository secrets set) |
|   CANNOT:  reach production     (no network path, no creds) |
+-------------------------------------------------------------+
```

**The pipeline holds no credentials at all.** This is not an accident of scope — it is the strongest available guarantee, and it follows directly from Q9=A/Q12=A (build the image, never push it; verify, never deploy). A compromised workflow file, a malicious dependency, or a poisoned action in this pipeline **has nothing to steal and nowhere to deploy**. Every future increment that adds a registry push, a signing key, or a deploy step trades some of this away, and should say so when it does.

`permissions: contents: read` is set at the workflow level, dropping the default write-capable `GITHUB_TOKEN` scopes.

## 3. Job topology

Three independent jobs, no dependency edges, all triggered together:

```
                    +--------------------------------+
                    |  pull_request -> main          |
                    |  push -> main                  |
                    +----------------+---------------+
                                     |
              +----------------------+----------------------+
              |                      |                      |
        +-----v------+        +------v-------+       +------v---------+
        |   fast     |        | integration  |       |  supply-chain  |
        | ~2-4 min   |        | ~1-3 min     |       |  ~2-3 min      |
        +------------+        +--------------+       +----------------+
        | gofmt      |        | testcontainers      | gitleaks (full  |
        | go vet     |        |   real Mongo        |   history,      |
        | arch-check |        |   real Valkey       |   fetch-depth 0)|
        | golangci   |        |                     | docker build    |
        | go build   |        | go test             |   (no push)     |
        | go test    |        |   -tags integration | trivy (report)  |
        |   -race    |        |   -timeout 10m      | syft SBOM       |
        | gosec      |        |                     |   (artifact)    |
        | govulncheck|        |                     |                 |
        |   (report) |        |                     |                 |
        | coverage   |        |                     |                 |
        |   (report) |        |                     |                 |
        +------------+        +--------------+      +----------------+
              |                      |                      |
              +----------------------+----------------------+
                                     |
                          all blocking gates green
                                     |
                                 mergeable
```

Wall-clock is bounded by the **slowest single job**, not by their sum. A compile error surfaces from `fast` in ~2 minutes regardless of what the integration job is doing (CI-NFR-03).

**Checkout depth differs by job**, deliberately:
- `fast`, `integration`: shallow (`fetch-depth: 1`) — they only need the tree.
- `supply-chain`: **full** (`fetch-depth: 0`) — `gitleaks detect` scans history, and a shallow clone would silently scan almost nothing while reporting success. That failure mode (a green gate that examined nothing) is exactly what X-1 was.

## 4. Cache topology (Q5 = A + C)

| Trigger | Go module/build cache | Rationale |
|---|---|---|
| `pull_request` | **enabled** (`actions/setup-go` cache, keyed on `go.sum`) | Fast feedback. A wrong cache only ever costs the PR author time, never `main`. |
| `push` → `main` | **disabled** (`cache: false`) | What lands on `main` is built from nothing. A stale or poisoned cache can never become the artifact of record. CI-NFR-04 holds where it counts. |

## 5. Test-fixture containers (integration job)

Started by testcontainers inside the job; **not infrastructure this unit provisions or operates**:

| Container | Purpose | Lifecycle |
|---|---|---|
| MongoDB | real `TaskStore` + `FindingsStore` contract suites | created and destroyed per test suite |
| Valkey | real `WorkQueue` + `ResponseCache` contract suites | created and destroyed per test suite |

These exist because **the `Drain` bug lived in exactly this seam** and no in-memory test could see it: the mem and Valkey implementations disagreed about what `Drain` means, and only the real one was wrong. The contract suite runs against both, so a future divergence fails.

## 6. Artifacts produced

| Artifact | Retention | Blocking? |
|---|---|---|
| SBOM (`sbom.cyclonedx.json`, syft) | workflow artifact | no |
| Trivy image scan report (HIGH/CRITICAL) | workflow artifact + log | no |
| `govulncheck` report | log | no |
| Coverage percentage | log | no |
| **Container image** | **discarded** — built, scanned, never pushed | build itself is blocking |

## 7. What this architecture deliberately does not do

| Not done | Why | Where it goes |
|---|---|---|
| Push an image to a registry | Q9=A / Q12=A — keeps the pipeline credential-free (§2) | future image/release increment |
| Sign images (`cosign`) | depends on a registry existing | same |
| Deploy to k8s | Q12=A; AI-DLC Operations is a placeholder and deployment execution is outside the workflow | Operations, out of band |
| Run on a schedule | C1=D — accepted blind spot: a CVE published during a quiet period waits for the next PR | recorded in §14 of `infrastructure-design.md` |
| Enforce the gates | **Branch protection is a repo setting, not a file.** Until an operator requires these checks on `main`, they are advisory and can be bypassed by pushing directly. | operator action, `infrastructure-design.md` §13 |
