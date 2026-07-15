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

## 🔬 실 Docker 검증 (2026-07-14) — #0a 완료, 손작성 추측 대거 교정
이미지를 실제로 빌드해 6개 wired probe를 **실 바이너리로 이미지 내부에서 실행**했다(`probe-integration`). 첫 실행이 F6 리스크를 그대로 현실화 — 손작성 argv/파서 I/O 모델이 크게 틀렸고, 아래를 교정했다. **최종: 6/6 probe가 이미지 내부에서 실행·파싱 clean 통과**(URL: humble·httpx·corsy / Host: testssl·dirhunt·subzy vs example.com).

**교정 내역 (실 실행이 잡아낸 것):**
- **핀/경로**: `humble`·`Corsy`는 PyPI 미배포(git 스크립트) — `pip install humble`는 무관한 패키지였음 → git clone+wrapper. `subzy` 모듈 경로는 `LukaSikic/subzy`(PentestPad 아님). testssl 클론을 go-build 스테이지로.
- **파일 출력 계약(핵심)**: httpx만 stdout JSONL. **나머지 5개(humble/subzy/testssl/dirhunt/corsy)는 JSON을 파일에 쓰고 stdout엔 배너/진행을 섞는다.** stdout-only 파서 계약이 안 맞아 → 러너에 `outArg`(임시 디렉터리+파일 읽기) 추가, 5개를 파일 출력으로 전환. humble은 `-op <dir> -of <name>` 분리 필요.
- **런타임 의존성**: testssl은 `hexdump`(bsdextrautils)+DNS도구(`dig`/dnsutils) 필요 → 이미지에 추가.
- **타임아웃/제약**: dirhunt는 검색엔진 조회로 hang → `--exclude-sources`(전 소스)+`--max-depth 1`+`--timeout`으로 대상-국소화; testssl full 스캔 >180s → `--fast --protocols --standard`(12초). 크롤러/TLS에 per-tool 180s 타임아웃.
- **기존 파일 버그**: dirhunt는 `--to-file`가 **이미 존재하면** JSON으로 읽으려다 실패 → 러너가 파일을 미리 만들지 않고 임시 디렉터리 내 미생성 경로 전달.

**⚠️ 아직 미검증 (정직) — positive-finding 스키마 추출:**
- benign 대상(example.com)에선 이 툴들이 **빈 출력 → clean(Checked)**. 즉 검증된 것은 "**실행+파싱이 깨지지 않음 → tool-failed 탈출**"까지다.
- **각 파서의 positive-finding JSON 스키마는 여전히 손작성/미검증**이며, 실 실행으로 **humble의 실제 스키마는 섹션 기반(`"[0. Info]"`, `"[1. Missing HTTP Security Headers]"`)으로 내 `{missing,cookies}` 추측과 완전히 다름**이 드러났다 → **현 humbleParse는 실 출력에서 finding을 못 뽑는다(항상 clean)**. 나머지도 취약 대상 없이는 positive 스키마 미검증.
- **후속 필수**: 취약 대상(약한 TLS·dir 리스팅·CORS·서브도메인 dangling)으로 각 툴의 실 positive JSON을 캡처해 파서/골든을 실측 재작성. 특히 humble 파서는 섹션 기반으로 재작성 필요.
- **배포 주의**: subzy는 첫 실행 시 fingerprints를 `$HOME`에 내려받음 → read-only 루트 FS 배포에서 `HOME=/scratch`(또는 빌드시 프리베이크) 필요(#0c).

## 인터페이스 불변 (PR-11)
`Adapter`/`Checker` 시그니처 불변. 변경은 `argv` non-nil화 + 파서 부착뿐. 외부 계약 무변경.
