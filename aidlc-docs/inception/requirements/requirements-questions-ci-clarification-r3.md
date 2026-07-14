# CI 파이프라인 — 명확화 질문 (Round 3, 마지막)

**작성일**: 2026-07-13
**이유**: Round 2로 모순 2건은 해소됐습니다(C1=D, C2=A). 남은 것은 **미결정 사항**입니다 — Q6은 `govulncheck` 하나의 게이트 강도만 물었는데, 새로 추가할 스캐너 3종(gitleaks·gosec·trivy)의 강도는 아직 정해지지 않았습니다. 제가 임의로 정하면 그건 요구사항이 아니라 제 추측입니다.

**여기까지 확정**:
Q1=A(GitHub Actions) · Q2=A(클라우드 러너) · Q3=A(PR + main push, **nightly 없음**) · Q4=A(통합 테스트 모든 PR) · Q5=A(툴체인 1.26.5 전면 상향) · **C1=D → govulncheck는 어디서나 non-blocking(리포트만)** · Q7=A(커버리지 게이트 없음) · Q8=A(golangci-lint blocking) · **C2=A → 이미지 빌드(푸시 없음) + Trivy 스캔** · Q11=A,B,C(gitleaks·gosec·trivy) · Q12=A(CI만) · Q13=A(PBT 현행)

---

## 먼저: 실제로 돌려본 결과 (추정 아님)

Q8=A("첫 실행 지적은 이번 증분에서 전부 수정")를 고르셨기에, 건수를 세지 않고 요구사항을 쓸 수 없어 **실제로 실행했습니다.**

**golangci-lint** — `.golangci.yml`이 **v1 스키마라 golangci-lint v2가 설정 파일 자체를 거부합니다.** 이 린트 설정은 **한 번도 실행된 적이 없습니다.** v2 스키마로 옮겨 돌린 결과:
- **총 8건** — errcheck 7건(`fh.Close`/`resp.Body.Close`/`w.Write` 반환값 미확인, 대부분 테스트 코드), staticcheck 1건(De Morgan 단순화)
- **depguard 0건** — NFR-02 아키텍처 규칙(data plane ↛ platform, domain ↛ 유닛) 위반 **없음**. 아키텍처는 깨끗합니다.
- → Q8=A는 값싼 선택입니다. 8건 전부 사소하고 이번 증분에서 수정 가능합니다.

**gosec** — 총 11건이지만 5건은 레포 안에 남아 있는 `.claude/worktrees/probe-stub-honesty/` 사본을 중복 스캔한 것입니다(git 추적 대상이 아니므로 CI에서는 나타나지 않음). **실제 소스는 6건**:
- **G112 × 2 — `ReadHeaderTimeout` 미설정** (`internal/platform/proxy/authproxy.go:46`, `cmd/chiller/main.go:239`). **이건 진짜 결함입니다** — Slowloris 공격에 열려 있습니다. CI가 진작 있었다면 잡혔을 항목입니다.
- G304 × 3 — 변수 경로로 파일 열기(`internal/findings/config/{spec,severity,mapping}.go`). 선언적 설정 파일 로더이고 경로는 운영자가 주는 값이라 **오탐에 가깝습니다.** 경로 검증을 추가하거나 `#nosec` 주석 + 사유를 달아야 합니다.
- G115 × 1 — `internal/findings/store/retry.go:86` int→uint 변환 오버플로 (심각도 HIGH / 확신도 MEDIUM). 실제로 음수가 될 수 있는 값인지 확인 필요.

**Trivy** — 아직 안 돌렸습니다. distroless 베이스 이미지의 CVE는 **우리가 코드를 고쳐서 없앨 수 없고**, 새 베이스 이미지가 나오기를 기다리거나 예외 처리해야 합니다. 이 성질이 아래 질문 3의 핵심입니다.

---

## 명확화 질문 1 — gitleaks(시크릿 스캔) 게이트

커밋에 자격증명이 들어간 것을 발견했을 때:

A) **blocking — 파이프라인 실패** (권장: 시크릿 유출은 되돌릴 수 없습니다. 히스토리에 한 번 들어가면 키를 폐기하는 것 외에 방법이 없으므로, "머지되기 전에 멈추는 것"이 유일하게 의미 있는 시점입니다. 오탐은 allowlist로 처리)

B) **non-blocking — 리포트만**

X) 기타

[Answer]: A

---

## 명확화 질문 2 — gosec(SAST) 게이트

위에서 확인한 대로 현재 실제 소스에 6건이 있습니다 (G112 2건은 진짜 결함, G304 3건은 오탐에 가까움, G115 1건은 확인 필요).

A) **blocking — 위 6건을 이번 증분에서 전부 처리(수정 또는 사유를 단 `#nosec`)한 뒤, 앞으로 신규 발견 시 실패** (권장: 지금 건수가 적어서 값이 쌉니다. 지금 안 하면 영원히 non-blocking으로 남습니다)

B) **non-blocking — 리포트만.** G112 2건(Slowloris)은 별개로 지금 수정하되, 게이트는 걸지 않음

C) **non-blocking — 리포트만. 아무것도 수정하지 않음** (기존 결함을 그대로 둠)

X) 기타

[Answer]: A

---

## 명확화 질문 3 — Trivy(이미지 스캔) 게이트

베이스 이미지 CVE는 우리 코드로 고칠 수 없다는 성질이 있습니다 (업스트림이 고쳐줘야 함).

A) **HIGH/CRITICAL만 blocking, 그 이하는 리포트** — 흔한 절충안. 단, 고칠 수 없는 CRITICAL이 뜨면 예외 등록(`.trivyignore`) 전까지 모든 PR이 막힙니다

B) **non-blocking — 리포트만** (`govulncheck`를 non-blocking으로 두기로 한 C1=D와 일관됨)

C) **HIGH/CRITICAL blocking + `.trivyignore`에 만료일 있는 예외 허용** — 가장 엄격하면서 막힘도 방지. 운영 부담이 생김

X) 기타

[Answer]: B

---

## 명확화 질문 4 — 베이스 이미지 다이제스트 고정

**SECURITY-10에 blocking 조항이 있습니다**: *"빌드 파이프라인은 고정된 도구 버전과 **검증된 베이스 이미지**를 써야 하며, 프로덕션 Dockerfile·CI 설정에 `latest` 태그를 쓰면 안 된다."* 지금 Dockerfile은 `golang:1.25` / `gcr.io/distroless/static-debian12:nonroot` — `latest`는 아니지만 **가변 태그**입니다(같은 태그가 내일 다른 이미지를 가리킬 수 있음). production-readiness의 blocking #3도 같은 항목입니다.

C2=A로 CI가 이미지를 빌드하게 됐으니, 지금 다이제스트로 고정할 수 있습니다.

A) **지금 고정한다** — Dockerfile의 두 베이스 이미지를 `@sha256:...` 다이제스트로 고정 + CI 액션도 커밋 SHA로 고정. **production-readiness blocking #3이 해소됩니다.** (권장)

B) **CI 액션만 고정하고 Dockerfile 베이스 이미지는 태그 유지** — 다이제스트 갱신 부담(베이스 이미지 업데이트마다 수동 갱신)을 피함. blocking #3은 남음

C) **둘 다 지금은 태그 유지** — 나중 이미지 증분에서 한 번에

X) 기타

[Answer]: A

---

## 답변을 마치셨으면

채팅에 **"완료"** 라고 알려주세요. 이번이 마지막 질문 라운드입니다. 답변을 받으면 `requirements.md`(CI 증분 섹션)를 작성하고 승인을 요청하겠습니다.
