# CI 파이프라인 — 요구사항 확인 질문

**증분**: CI 파이프라인 구축 (infra-plan 후속 · production-readiness blocking #6 해소)
**작성일**: 2026-07-13
**답변 방법**: 각 질문의 `[Answer]:` 뒤에 **알파벳 하나**를 적어주세요. 보기가 맞지 않으면 마지막 `X) 기타`를 고르고 뒤에 직접 설명해 주세요.

---

## 먼저 읽어주세요 — 실사에서 나온 사실 3가지

이 질문들은 아래 사실 위에서 만들어졌습니다. 답변 전에 확인해 주세요.

1. **이 레포에는 `git remote`가 없습니다.** `git remote -v` 결과가 비어 있습니다. CI는 호스팅된 원격 저장소 위에서 도는 것이므로, **원격이 등록되기 전까지는 파이프라인 정의 파일을 작성할 수는 있어도 "실제로 초록불이 켜지는 것"을 이 자리에서 확인할 수는 없습니다.** 저는 이 한계를 완료 보고에 그대로 적을 것이고, 검증되지 않은 것을 검증됐다고 쓰지 않겠습니다.

2. **직전 증분에서 배운 것이 이번 설계의 전제입니다.** 통합 테스트를 실제로 처음 돌렸더니 결함 3건이 나왔습니다 — 그중 하나(Valkey `WorkQueue.Drain` 미동작)는 `StopScan`이 "중단"을 의미하지 않게 만드는 **실제 버그**였고, 두 증분에 걸쳐 아무도 몰랐습니다. Build&Test는 그동안 두 번 초록불이었습니다. **"컴파일만 확인하는 CI"는 CI가 아니라는 것**이 이번 증분의 출발점입니다.

3. **`make vuln`은 지금 실패합니다.** `GO-2026-5856`(crypto/tls) — Go 표준 라이브러리 취약점이고, 소스 수정 없이 **툴체인을 1.26.5 이상으로 올리면 사라집니다.** 현재 `go.mod`는 `go 1.25.0`, `Dockerfile`은 `golang:1.25`입니다. 이걸 어떻게 처리하느냐가 "CI를 켜자마자 빨간불이냐"를 가릅니다.

---

## Question 1
CI를 어느 플랫폼에서 돌릴 계획인가요? (원격 저장소가 없으므로, 앞으로 어디에 올릴지 기준으로 답해 주세요)

A) **GitHub Actions** — 코드를 GitHub(또는 GitHub Enterprise)에 올릴 예정. `.github/workflows/*.yml` 작성

B) **GitLab CI** — GitLab(사내 인스턴스 포함)에 올릴 예정. `.gitlab-ci.yml` 작성

C) **Jenkins** — 사내 Jenkins. `Jenkinsfile` 작성

D) **아직 미정** — 지금은 플랫폼 중립적으로 `Makefile` 타깃과 스크립트만 정비하고, 파이프라인 정의는 나중에

X) 기타 (아래 `[Answer]:` 뒤에 설명)

[Answer]: A

---

## Question 2
러너(빌드 머신)는 어떤 것을 쓰나요? **통합 테스트가 testcontainers로 실제 Mongo/Valkey 컨테이너를 띄우기 때문에, 러너에서 Docker를 쓸 수 있는지가 결정적입니다.**

A) **클라우드 호스팅 러너** (GitHub-hosted / GitLab SaaS) — Docker 사용 가능, 외부 인터넷 접근 가능

B) **사내 self-hosted 러너** — Docker 사용 가능하지만 외부 레지스트리(Docker Hub 등) 접근이 제한될 수 있음 (프록시/미러 필요)

C) **사내 self-hosted 러너 · Docker 사용 불가** (예: 보안 정책) — 이 경우 통합 테스트는 CI에서 돌릴 수 없고, 별도 방안이 필요합니다

X) 기타

[Answer]: A

---

## Question 3
파이프라인은 언제 돌아야 하나요?

A) **PR(머지 리퀘스트) + 기본 브랜치 push** — 표준

B) **PR + 기본 브랜치 push + 태그(릴리스)** — 태그에서 이미지 빌드/릴리스 아티팩트까지

C) **PR + 기본 브랜치 push + 태그 + 야간(nightly) 스케줄** — 느린 검사(통합·취약점)를 야간에 한 번 더

X) 기타

[Answer]: A

---

## Question 4
**통합 테스트(`make test-integration`)를 어느 트리거에 붙일까요?** 실제 컨테이너를 띄우므로 수 분~수십 분이 걸립니다 (직전 실행에서 Valkey 계약 위반 버그를 잡아낸 바로 그 테스트입니다).

A) **모든 PR에서 실행** — 가장 안전. PR 피드백이 느려짐 (수 분~수십 분)

B) **PR에서는 스킵, 기본 브랜치 push + nightly에서만 실행** — PR은 빠르게, 회귀는 머지 후에 잡음 (버그가 main에 잠깐 들어갈 수 있음)

C) **기본은 스킵, PR에 라벨(예: `run-integration`)을 붙이면 실행 + 기본 브랜치/nightly는 항상 실행** — 절충

D) **PR·main·nightly 전부 항상 실행** (= A + 스케줄)

X) 기타

[Answer]: A

---

## Question 5
**Go 툴체인 버전**을 어떻게 할까요? (`GO-2026-5856` 해소 = production-readiness blocking #2)

A) **`go.mod`에 `toolchain go1.26.5` 지시자 추가 + Dockerfile 빌드 스테이지를 `golang:1.26` 계열로 상향 + CI도 동일 버전** — 로컬·CI·이미지가 전부 한 버전으로 고정됨 (권장)

B) **CI와 Dockerfile만 1.26.5로 올리고 `go.mod`의 `go 1.25.0`은 그대로** — 언어 하위 호환은 유지되지만 로컬 개발자가 구버전으로 빌드하면 취약점이 남음

C) **이번 증분에서는 건드리지 않는다** — CI를 켜면 `make vuln` 단계가 즉시 빨간불이 됩니다 (경고로 낮추지 않는 한)

X) 기타

[Answer]: A

---

## Question 6
`make vuln`(govulncheck) 결과를 어떻게 다룰까요?

A) **취약점 발견 시 파이프라인 실패(blocking)** — SECURITY-10 취지에 맞음. 단, Q5에서 A/B를 고르지 않으면 지금 당장 빨간불

B) **리포트만 남기고 통과(non-blocking)** — 초록불은 유지되지만, 아무도 안 보게 될 위험

C) **기본 브랜치·nightly에서는 blocking, PR에서는 non-blocking** — 절충

X) 기타

[Answer]: C

---

## Question 7
**커버리지 게이트**를 둘까요? (현재: 통합 포함 63.8% / 유닛만 49.9%)

A) **게이트 없음 — 수치만 리포트로 출력** (권장: 커버리지 임계값은 종종 무의미한 테스트를 유도합니다)

B) **임계값 미달 시 실패** — 임계값은 현재 수치 근처(예: 통합 포함 60%)로 설정

C) **커버리지 측정 자체를 하지 않음**

X) 기타

[Answer]: A

---

## Question 8
**린트/정적 분석**을 CI 게이트로 넣을까요? (`.golangci.yml`은 이미 있고, depguard로 NFR-02 아키텍처 규칙을 기계적으로 강제합니다. 다만 `golangci-lint`는 이 머신에 설치돼 있지 않아 **한 번도 실행된 적이 없을 가능성**이 있습니다 — 켜면 기존 코드에서 지적이 쏟아질 수 있습니다)

A) **`golangci-lint`를 버전 고정해 CI에서 실행, 실패 시 blocking** — 처음 켤 때 나오는 지적은 이번 증분에서 전부 수정

B) **`golangci-lint`는 non-blocking(리포트만), `make arch-check`·`gofmt`·`go vet`만 blocking** — 아키텍처 규칙과 포맷은 지키되 스타일 지적은 점진 개선

C) **`golangci-lint` 없이 `gofmt`·`go vet`·`make arch-check`만** — 가장 가벼움

X) 기타

[Answer]: A

---

## Question 9
**컨테이너 이미지 빌드**를 CI 범위에 넣을까요?

A) **CI 범위 밖 — 테스트/검증만** (이미지는 나중 증분에서)

B) **빌드만 하고 푸시는 안 함** — Dockerfile이 깨지지 않았는지만 확인

C) **빌드 + 레지스트리 푸시 + 다이제스트 출력** (production-readiness blocking #3 "태그 대신 다이제스트" 해소)

D) **C + `cosign` 서명 + `syft` SBOM 첨부** (공급망 무결성 완결, SECURITY-10/13)

X) 기타

[Answer]: A

---

## Question 10
(Q9에서 C/D를 골랐다면) **레지스트리**는 어디인가요?

A) **GHCR** (ghcr.io — GitHub Actions와 가장 매끄러움)

B) **사내 Harbor / Nexus 등 프라이빗 레지스트리**

C) **AWS ECR**

D) **해당 없음** (Q9=A/B를 골랐음)

X) 기타

[Answer]: D

---

## Question 11
**추가 보안 스캔**을 넣을까요? (복수 선택 가능 — 예: `A, C`)

A) **시크릿 스캔** (gitleaks 등) — 커밋에 자격증명이 들어가는 것 차단. SECURITY-12와 직결

B) **SAST** (gosec 또는 CodeQL) — 코드 레벨 취약 패턴

C) **컨테이너 이미지 스캔** (Trivy/Grype) — 베이스 이미지 CVE

D) **추가하지 않음** — `govulncheck`(의존성)만으로 충분

X) 기타

[Answer]: A, B(gosec), C

---

## Question 12
**배포(CD)까지 이번 증분에 포함할까요?**

A) **CI만** — 빌드·테스트·검증까지. 배포는 별도 증분 (권장: AI-DLC Operations는 placeholder이고, 배포 실행은 워크플로 밖입니다)

B) **CI + 릴리스 아티팩트 생성** — 태그 push 시 이미지/SBOM/체크섬을 릴리스에 첨부, 실제 배포는 사람이 수동 트리거

C) **CI + k8s 자동 배포까지** — `deploy/k8s/*` 매니페스트를 스테이징에 자동 적용

X) 기타

[Answer]: A

---

## Question 13
**PBT(속성 기반 테스트) 확장이 FULL blocking으로 켜져 있습니다.** CI 관점에서 어떻게 다룰까요? (현재 PBT는 `go test`의 일부로 이미 돌고 있습니다)

A) **현행 유지** — PBT는 일반 `go test ./...`에 포함되어 있으므로 별도 CI 스텝 불필요

B) **nightly에서 PBT를 더 많은 반복 횟수로 재실행** (`-quick.count` 등 상향) — 얕은 케이스만 도는 것을 방지

X) 기타

[Answer]: A

---

## 답변을 마치셨으면

이 파일을 저장한 뒤 채팅에 **"완료"** 라고 알려주세요. 답변 간 모순·모호함이 있으면 후속 질문 파일을 만들고, 없으면 `requirements.md`(CI 증분 섹션)를 작성한 뒤 승인을 요청하겠습니다.
