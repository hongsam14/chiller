# Code Generation Plan — subzy fingerprints DB provisioning (#0e, r18)

Branch `feat/subzy-db`. Implements `subzy-db-infrastructure-design.md` (operator-provisioned:
`make subzy-db` + read-only volume + `--vuln_list`). Execute Part 2 in this order.

## A. Provisioning target
- [x] **A1** `Makefile` — `subzy-db` target: pinned `curl -fsSL <RAW_URL@COMMIT>` of the
      subzy-compatible `fingerprints.json` → `deploy/subzy/fingerprints.json`; create the dir; print the
      pinned URL/commit as provenance; fail loudly (`-f`) on a dead/moved URL. `.PHONY`. The exact
      canonical URL + commit is resolved in Part 2 (subzy v1.1.0-compatible schema — from
      `LukaSikic/subzy` or EdOverflow/can-i-take-over-xyz, whichever the v1.1.0 binary parses).
- [x] **A2** `.gitignore` — add `deploy/subzy/` (the DB is operator-provisioned, never committed).

## B. Dockerfile
- [x] **B1** `ENV SUBZY_VULN_LIST=/opt/subzy/fingerprints.json` + a doc comment (subzy reads the DB from
      this path; the operator MOUNTS it read-only there — the image bakes no data). No `COPY`.

## C. Deploy wiring
- [x] **C1** `deploy/compose/docker-compose.yml` — mount
      `./deploy/subzy/fingerprints.json:/opt/subzy/fingerprints.json:ro` on the worker/all service; a
      comment pointing at `make subzy-db`.
- [x] **C2** `deploy/k8s` — a read-only volume from an operator-provisioned ConfigMap (or a
      `subzy-db.example`-style note), mirroring how storageState/secrets are documented as template-only.

## D. Adapter wiring
- [x] **D1** `internal/scan/checking/probe/parsers.go` — `subzyOutArg` appends `--vuln_list <path>`,
      `<path>` = `subzyVulnList()` = `os.Getenv("SUBZY_VULN_LIST")` or default
      `/opt/subzy/fingerprints.json`. **Contingent on the `--vuln_list` flag (verified in F); the
      fallback is a `$HOME/subzy` cache mount, which would change this path.** (review P2)
- [x] **D2 (review P1) — defensive honesty guard.** Before running subzy, the adapter checks the
      `SUBZY_VULN_LIST` file **exists and is non-empty**; if not, it returns a `tool-failed` error
      (ErrNotWired spirit) WITHOUT running subzy. This makes NFR-S1 (never false-clean) hold
      **regardless of subzy's undocumented behavior** on a missing/empty `--vuln_list` (subzy might
      load zero fingerprints and report every subdomain clean = false-clean). Honesty must not depend
      on the tool.

## E. Verification
- [x] **E1** `parsers_test.go` — `subzyOutArg` argv includes `--vuln_list` + the default path; a
      `SUBZY_VULN_LIST` override is respected (t.Setenv).
- [x] **E2** CI `probe-integration` (`.github/workflows/ci.yml`) — run `make subzy-db`, export
      `SUBZY_VULN_LIST=deploy/subzy/fingerprints.json`, so `PROBE_FIXTURE_SUBZY=takeover-mock` exercises
      subzy with the real flag against the fixture (asserts no self-fetch/parse error → PS-025 finding).
      **`probe-integration` is opt-in/non-blocking (r7 IQ2=A), so the new `make subzy-db` fetch cannot
      red the base CI if the source URL dies.** (review P4)
- [x] **E3** Gates: build · vet(+integration) · gofmt · `make arch-check` · `go test -race` ·
      `make gosec` · `make secrets` (deploy/subzy git-ignored → no data exposure) · `make signatures-check`.

## G. Operator provisioning docs + build context (review P5)
- [x] **G1** `.dockerignore` — add `deploy/` (keep the build context clean; the DB is never baked).
- [x] **G2** Operator docs — a deploy note (README/`deploy/` section) stating: run `make subzy-db`
      before deploying and mount the DB read-only; without it, PS-025 is honestly `tool-failed`.

## F. Build & Test (the load-bearing gate)
- In-image real Docker run: `make subzy-db`, build the image, **mount** the DB, run subzy against the
  vuln-target `takeover-mock` → confirms the volume + `--vuln_list` end-to-end and **settles the flag
  risk** (fallback: `$HOME/subzy` cache mount if v1.1.0 ignores `--vuln_list`).
- **Honesty check (review P1):** with the DB **absent or empty**, PS-025 is `tool-failed`, **NOT clean**
  — verifies the D2 guard (and subzy's own behavior).
- **Part-2 early smoke (review P3, if Docker available):** `docker run subzy … --vuln_list <fetched>`
  once to confirm the schema parses, catching a source/schema mismatch before the full Build & Test.

## Risk / invariants
Low–Medium. Only real unknown = the `--vuln_list` flag/schema (settled at Build & Test Docker). probe
adapter interface · subzy parser (r9, unchanged) · 26-partition · CWE ownership · rate/gate/timeout
envelope · non-root/read-only-rootfs hardening (DB mounted read-only) unchanged. No data committed.

## 완료 노트 (Part 2)
- **P3 조기 스모크가 핵심 설계 오류를 사전 차단**: `subzy v1.1.0 run --help`에 `--vuln_list` **없음**(플래그 자체가 없음). subzy는 `$HOME/subzy/fingerprints.json`를 읽고 부재 시에만 self-fetch. → Q2-A(--vuln_list) 폐기, **Q2-B(HOME→$HOME/subzy 마운트)** 로 전환. 컨테이너 실측: read-only 마운트 + `HOME=/opt` → subzy 정상 실행(exit 0, 파싱가능 JSON, self-fetch 없음).
- 구현: subzyOutArg에 플래그 없음 · `SUBZY_DB`(env, default /opt/subzy/fingerprints.json) · `subzyRunEnv()`가 HOME 파생(runner.extraEnv) · `subzyDBGuard`(precheck, 부재/빈 파일→tool-failed, review P1) · Makefile `subzy-db`(pinned commit) · Dockerfile ENV SUBZY_DB · compose/k8s ro 볼륨 · .gitignore/.dockerignore(deploy) · deploy README operator 문서.
- 검증: TestSubzyRunEnv_HomeFromDB(HOME 파생 + --vuln_list 미포함) · TestSubzyDBGuard(부재/빈/정상) · 전체 스위트 green. F(Build&Test)에서 in-image positive + 부재→tool-failed 최종 확정.
