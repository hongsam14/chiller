# Code Generation Plan — probe 증분 (unit: scan/probe)

**Single source of truth for Code Generation.** Follow steps in order; mark [x] immediately on completion.
**Inputs**: `requirements-probe.md`, `scope-decision-active-vs-passive.md`, `probe-infrastructure-design.md`, `execution-plan-probe.md §4b`.
**Stories**: US-007(probe checker seam), US-027(fail isolation → tool-failed), 부분실패 4-state.
**Interface 불변(PR-11)**: `Adapter`/`Checker` 시그니처 불변; `argv` non-nil로만 `Wired()` 뒤집힘.

## 핵심 구분 (테스트 전략의 근거)
- **`Wired()` = argv 존재**(컴파일 타임). 이 증분 후 7개 전부 true.
- **런타임 판정 = 바이너리 실제 실행 성공**. 바이너리 없는 일반 `go test`에선 wired여도 exec 실패 → **여전히 ToolFailed**(정직).
- 따라서 "checked로 상승"은 **바이너리가 있는 통합(이미지 내부, IQ4=A)** 에서만 증명. 일반 유닛 테스트는 (a) 파서를 골든 바이트로 직접 검증, (b) exec 프레임워크를 경량 실 바이너리/주입 exec로 검증.

## 코드 위치 (절대 aidlc-docs/ 아님)
- `internal/scan/checking/probe/` — adapters.go, probe.go, exec_linux.go(신규), exec_other.go(신규), parsers.go(신규) + `*_test.go` + `testdata/`(골든)
- `Dockerfile`, `go.mod`/`go.sum`, `deploy/k8s/*`, `deploy/compose/*`, `.github/workflows/ci.yml`, `.github/dependabot.yml`
- 요약: `aidlc-docs/construction/scan/code/probe-business-logic-summary.md` (markdown only)

---

## Phase A — 서브프로세스 수명주기 (NFR-P1, 좀비 방지)

- [x] **A1** `internal/scan/checking/probe/exec_linux.go` (`//go:build linux`): `defaultExec`를 프로세스그룹 kill로 강화 — `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`, `cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }`, `cmd.WaitDelay = N초`. ctx 타임아웃/취소 시 **자식 그룹 전체** SIGKILL(testssl→openssl, python 자식 포함). `exec_other.go` (`//go:build !linux`): 기존 동작 fallback(개발기 컴파일 보장). probe.go의 `defaultExec` 정의를 이 파일들로 이동.
- [x] **A2** `probe_exec_test.go` (일반 `go test`, Q4=C 유닛 파트): 경량 실 바이너리로 `defaultExec` 검증 — ① 정상 종료→stdout 반환, ② 자식 spawn 후 타임아웃→**프로세스그룹 사멸**(자식 PID 죽음 확인), ③ 비정상 종료→error, ④ proxy env 주입 확인. Linux에서만 pgroup 단언(플랫폼 가드).

## Phase B — 툴별 argv + Parser + 골든 픽스처 (Q5=A JSON 우선)

각 스텝: `argv(Target)` + `Parser(Target, stdout)→[]Finding` + `testdata/<tool>.json` 골든 + 파서 유닛 테스트(골든→기대 Finding). Finding은 **소유 CWE만** 부여, `SourceTool`=툴명, `TargetURL`=대상, `Severity` Valid(R4), `Evidence.Snippets` ≤ `domain.MaxEvidenceSnippets`(8). 정확한 CLI 플래그/버전 스키마는 이 단계에서 설치본 기준 확정.

- [x] **B1 humble** (URL, PS-001/015/016/017/024): argv `humble -u <URL> -o json …`. Parser: 누락 헤더별 Finding — HSTS→CWE-319, cookie Secure→CWE-614, HttpOnly→CWE-1004, SameSite→CWE-1275, frame-options→CWE-1021. (1회 실행 → 최대 5 Finding)
- [x] **B2 httpx** (URL, PS-007/020): argv `httpx -json -u <URL> -silent -no-color …`. Parser: server/tech 버전 노출 → CWE-693 Finding(하나로 PS-007+020 커버, 매핑이 [PS-007,PS-020]).
- [x] **B3 Corsy** (URL, PS-021): argv `corsy -u <URL> -j …`. Parser: CORS 취약 유형 → CWE-346.
- [x] **B4 changeme** (URL, PS-019): **이번 증분 미배선(F5 결정).** `newStub` 유지 → PS-019는 정직한 tool-failed. Py2 유산(Py2 기반·능동 크리덴셜·JSON 스키마 불명)으로 불확실성 최고 → 후속 증분에서 py3 포크 확정 후 배선. **부수효과: `newStub`/`stubParse`가 계속 사용되어 F2(unused 린트) 자동 해소.**
- [x] **B5 testssl** (Host, PS-002, DirectOnly): argv `testssl.sh --jsonfile /dev/stdout --quiet --openssl=/opt/testssl/bin/openssl.Linux.x86_64 <host>:443`. **JSON을 stdout으로**(F1 — 파서 계약 `Parser(target, stdout)`에 맞춤; 진행 로그는 stderr). Parser: 약한 프로토콜/cipher/인증서 → CWE-1327. **파일 출력 방식 금지**(계약 불일치).
- [x] **B6 dirhunt** (Host, PS-004): argv `dirhunt <base-URL> --json … `. Parser: 디렉터리 리스팅 노출 → CWE-548.
- [x] **B7 subzy** (Host, PS-025): argv `subzy run --target <host> --output json --hide_fails`. Parser: 서브도메인 탈취 가능 → `domain.NoCanonicalCWE`(PS-025, attacker-axis).
- [x] **B8** `adapters.go`: **6개**(humble·httpx·Corsy·testssl·dirhunt·subzy)를 `newWired(id, tool, scope, argv, parser, cwes…)`로 교체. testssl은 `DirectOnly` 유지, 나머지 `ProxyEligible`(ConfigureProxy 자동 적용). **changeme는 `newStub` 유지**(F5). `newStub`/`stubParse`는 changeme가 사용 → 보존(정직성 seam, F2 해소).

## Phase C — 정직성 + 커버리지 테스트 갱신

- [x] **C1** `probe_test.go`: `TestUnwiredAdapter_FailsWithErrNotWired` **유지**(changeme가 미배선 → 여전히 유효, `ErrNotWired`+`newStub` 사용 유지, F2). `TestDefault_NoProbeWiredYet` → **`TestDefault_SixWired_ChangemeStub`**(6 wired·changeme만 unwired 단언). 추가: **`TestWiredAdapter_MissingBinary_ReportsError`**(wired argv + exec가 not-found → `Check`가 error 반환, **절대 빈 clean 아님**).
- [x] **C2** `test/platform_e2e_test.go`: `TestE2E_UnwiredProbes_…` → **`TestE2E_ProbesWithoutBinaries_ToolFailed_NotChecked`**로 재작성. 일반 test 환경(바이너리 부재): 11 wired probe는 exec 실패, changeme는 ErrNotWired → **전부 ToolFailed(무판정), 절대 Checked 아님**. 카운트 **불변**: tool-failed=12·delegated=4·observation 10 — **부재 환경에서 왜 여전히 12인지 주석 명기**(wired≠binary-present; 툴 없으면 정직 무판정). PS-009 Checked 유지.
- [x] **C3** 신규 `//go:build integration` e2e (이미지 내부, IQ4=A): 실 바이너리로 11개 wired 항목이 benign 대상에서 **tool-failed→Checked로 이동**(=툴 실행 성공 + 파서가 실 스키마 파싱; **양성 finding 아님** — F4). changeme(PS-019)는 tool-failed 잔존. 기대 카운트 **tool-failed 12→1(PS-019)·checked+na 10→21·delegated 4·26 파티션 유지**. 이것이 "checked 상승" 수용 증거. **양성 finding 파싱은 골든 픽스처(B, 유닛)로만 증명**(취약 대상 CI 재현 불가).

## Phase D — 이미지 + 의존성 핀 + 배포 + CI

- [x] **D1** `Dockerfile`: 멀티스테이지 — (go-build) chiller + httpx + subzy; (testssl) git tag 체크아웃 + 번들 openssl; (runtime) `python:3.12-slim-bookworm@sha256:…` + pipx per-tool venv(humble/dirhunt/changeme/Corsy) + testssl/go 바이너리 COPY + non-root + `/scratch` + `TMPDIR`. (probe-infrastructure-design §1)
- [x] **D2** 의존성 핀: `go.mod` `tool` 지시자(httpx·subzy)+`go.sum`; python pipx 핀(humble·dirhunt PyPI 버전; Corsy git SHA); testssl git 태그. **changeme는 이번 미배선(F5)이라 번들 제외** — 후속 증분에서 py3 포크 확정 후 추가.
- [x] **D3** 배포: `deploy/k8s/*` securityContext(runAsNonRoot·readOnlyRootFilesystem·drop ALL·no-new-priv) + scratch emptyDir + 리소스 상향; `deploy/compose/docker-compose.yml` read_only+scratch+cap_drop. (deployment-architecture.md)
- [x] **D4** CI `.github/workflows/ci.yml`: integration 잡을 **빌드 이미지 내부**에서 `go test -tags integration ./internal/scan/checking/probe/...`(IQ4=A); trivy/syft가 python/testssl 레이어 스캔; `.github/dependabot.yml` docker 베이스 추적(기존 확인).

## Phase E — 문서

- [x] **E1** `aidlc-docs/construction/scan/code/probe-business-logic-summary.md`: 배선된 6툴, argv/파서 요약, 프로세스그룹 kill, 정직성 불변식 유지 방식, 카운트 이동(부재=12 vs 통합=12→1), changeme 잠정 미배선 사유(F5). **F6 정직 명기: 골든 픽스처는 손으로 작성됨 → 파서는 실 툴 출력 대비 미검증이며 Docker 통합 실행 전까지 잠정**(스키마 확정은 Build&Test/CI).

---

## 로컬 수용 기준 (이 증분, Docker 불요분)
- `go build ./...`·`go vet ./...`·`gofmt -l`·`make arch-check` green.
- `go test ./... -race` green — **파서 유닛(골든)** + **exec 프레임워크(경량 실 바이너리, pgroup kill)** + 재작성 정직성/e2e(부재 환경) 통과.
- **인터페이스 불변(PR-11)** 확인.

## Docker/CI 수용 기준 (Build & Test 단계로 위임)
- 이미지 빌드 green; `go test -tags integration`(이미지 내부, 실 바이너리) → 11 wired 항목 tool-failed→Checked(**tool-failed 12→1**, PS-019만 잔존)·26 파티션·파서가 실 스키마 파싱.
- trivy/syft/gosec/govulncheck green.
- **F6**: 이 통합 실행이 골든 픽스처 스키마를 실 출력으로 처음 검증하는 지점 — 여기서 스키마 불일치 시 파서/골든 수정.

## 리스크 / 리뷰 반영 (F1-F6)
- **F1** testssl 파일출력→stdout(`--jsonfile /dev/stdout`)로 파서 계약 일치. (B5)
- **F2** 전부배선 시 `newStub` unused 린트 → changeme 미배선으로 `newStub` 사용 유지. (B4/B8/C1)
- **F4** 통합 테스트는 "실행+스키마 파싱=Checked 상승"만 증명, **양성 finding 아님**. (C3)
- **F5** changeme(PS-019) **이번 미배선**, 정직 tool-failed. 6개 배선. 후속서 py3 포크. (B4/D2)
- **F6** 골든 픽스처 손작성 → 파서는 통합 실행 전까지 잠정. (E1/CI)
- **F3(오탐)** syscall/x/sys는 depguard 비대상 — pgroup kill 문제없음.
- **툴 출력 스키마 변동** — 버전 핀 + 골든 픽스처(+통합 검증)로 고정.
- **프록시 env 존중(G1)** — 이 증분은 직결 기본. financial 라우팅 검증은 §2.
