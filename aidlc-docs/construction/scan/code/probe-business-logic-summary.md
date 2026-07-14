# Code Generation Summary — probe 증분 (unit: scan/probe)

**Date**: 2026-07-14 · **Plan**: `construction/plans/probe-code-generation-plan.md` (all steps [x])
**Verified (local)**: `go build ./...` · `go vet ./...`(+`-tags integration`) · `gofmt -l` clean · `go test ./... -race` pass · `make arch-check` OK
**신규 Go 의존성: 0** (표준 `os/exec`·`syscall`·`encoding/json`·`bufio`만).

## 무엇을 했나
7개 probe 중 **6개를 실배선**(argv + JSON 파서 + 골든 픽스처)하고, **서브프로세스 프로세스그룹 kill**(좀비 방지)을 넣었다. changeme는 정직하게 미배선(tool-failed) 유지.

| Phase | 산출 |
|---|---|
| **A** exec 수명주기 | `exec_linux.go`(Setpgid + `Cancel`로 `kill -pgid` + `WaitDelay`) / `exec_other.go` fallback. `defaultExec`를 probe.go에서 분리. **유닛 테스트로 손자 프로세스 사멸 실증**(`TestDefaultExec_TimeoutKillsProcessGroup`). |
| **B** 6 배선 | `parsers.go` — humble·httpx·Corsy·testssl·dirhunt·subzy의 argv + stdout→Finding 파서(JSON). `adapters.go` `newStub`→`newWired`(6). testssl `--jsonfile /dev/stdout`(F1). 골든 `testdata/*.json` + 파서 유닛 8종. |
| **C** 정직성/커버리지 | `TestUnwiredAdapter_FailsWithErrNotWired`(changeme) 유지 · `TestDefault_SixWired_ChangemeStub` · `TestWiredAdapter_MissingBinary_ReportsError` · e2e `TestE2E_ProbesWithoutBinaries_ToolFailed_NotChecked`(개명) · **통합 e2e `//go:build integration`**(이미지 내부 실 exec+파싱). |
| **D** 이미지/배포/CI | 멀티스테이지 `Dockerfile`(python:3.12-slim + pipx humble/dirhunt + Corsy clone-wrapper + go install httpx/subzy + testssl git tag+번들 openssl + non-root + `/scratch`) · `deploy/k8s/worker.yaml` scratch emptyDir+TMPDIR · `docker-compose.yml` read_only+tmpfs+cap_drop · CI `probe-integration` 잡(이미지 내부 실행). |
| **E** 문서 | 본 요약. |

## 배선/미배선
- **배선 6**: humble(PS-001/015/016/017/024) · httpx(PS-007/020) · Corsy(PS-021) · testssl(PS-002, DirectOnly) · dirhunt(PS-004) · subzy(PS-025).
- **미배선 1(F5)**: changeme(PS-019) — Python2 유산·능동 크리덴셜·JSON 스키마 불명으로 이번 제외. `newStub` 유지 → **ErrNotWired → tool-failed(정직 무판정)**. 후속 증분에서 py3 포크 확정 후 배선. (부수효과: `newStub`/`stubParse` 계속 사용 → unused 린트 회피, F2)

## 정직성 불변식 (절대 회귀 금지)
- **wired ≠ binary-present.** argv를 넣어 `Wired()`=true여도, 바이너리가 없는 일반 `go test`에선 exec가 실패 → **여전히 tool-failed**(빈 clean 절대 아님). `TestWiredAdapter_MissingBinary_ReportsError`가 방어.
- 파서가 실 출력에서 파싱 실패 → error → tool-failed(`TestParsers_MalformedJSON_IsError`). 빈 stdout(툴이 정상 실행·무발견) → clean/Checked(`TestParsers_EmptyStdout_IsClean`).

## 커버리지 카운트 이동
- **일반 `go test`(바이너리 부재)**: tool-failed **12 그대로** (11 wired가 exec 실패 + changeme ErrNotWired). e2e가 이를 명시적으로 단언·주석.
- **이미지 내부 통합(실 바이너리)**: 11 wired 항목이 benign 대상에서 실행 성공 → Checked. **tool-failed 12→1**(PS-019만 잔존), checked+na 10→21, 26 파티션 유지. (= "checked 상승"; **양성 finding 아님** — F4)

## ⚠️ 정직한 한계 (F6 — 통합 실행 전까지 잠정)
- `parsers.go`의 JSON 스키마와 `testdata/*.json` 골든은 **손으로 작성**했다. 각 툴의 **실제 출력 대비 미검증**이며, CI `probe-integration` 잡(첫 Docker 빌드)이 실 출력으로 처음 검증한다. 스키마 불일치 시 **파서/골든을 수정**(파서를 느슨하게 풀어 은폐 금지).
- **Dockerfile 툴 버전 핀·패키징**(httpx@v1.6.10·subzy@v1.1.0·testssl v3.2.1·humble==1.30·dirhunt==0.9.0·Corsy@SHA, base digest)도 **첫 이미지 빌드에서 확정**. 존재하지 않는 버전이면 조정하되 **언핀 금지**.
- financial 프록시 env 존중(G1)은 이 증분 범위 밖(직결 기본). 인증 표면 라우팅은 §2.

## 인터페이스 불변 (PR-11)
`Adapter`/`Checker` 시그니처 불변. 변경은 `argv` non-nil화 + 파서 부착뿐. 외부 계약 무변경.
