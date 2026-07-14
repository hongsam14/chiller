# CI 파이프라인 — 명확화 질문 (Round 2)

**작성일**: 2026-07-13
**이유**: 1라운드 답변에서 **논리적 모순 2건**이 발견되었습니다. 제가 임의로 해석하면 요구사항이 아니라 제 추측이 구현되므로, 확인 후 진행합니다.

**확정된 답변** (모순 없음, 재질문 불필요):
Q1=A(GitHub Actions) · Q2=A(클라우드 호스팅 러너) · Q3=A(PR + main push) · Q4=A(**통합 테스트를 모든 PR에서 실행**) · Q5=A(`toolchain go1.26.5` + Dockerfile + CI 전부 상향) · Q7=A(커버리지 게이트 없음, 리포트만) · Q8=A(golangci-lint blocking, 첫 실행 지적은 이번 증분에서 전부 수정) · Q11-A(gitleaks 시크릿 스캔) · Q11-B(gosec SAST) · Q12=A(CI만, CD 제외) · Q13=A(PBT 현행 유지) · Q10=D(해당 없음)

---

## 모순 1: nightly가 없는데 nightly에서 blocking

- **Q3 = A** — 트리거는 **PR + 기본 브랜치 push** 뿐입니다. **태그도, 야간(nightly) 스케줄도 없습니다.**
- **Q6 = C** — "**기본 브랜치·nightly에서는 blocking, PR에서는 non-blocking**"

`govulncheck`를 nightly에서 blocking으로 돌리라고 하셨지만, **nightly 자체가 존재하지 않습니다.** 둘 중 하나를 조정해야 합니다.

참고로 이 결정이 실제로 의미하는 바: Q5=A로 툴체인을 1.26.5로 올리면 **지금 알려진 취약점(GO-2026-5856)은 사라집니다.** 즉 파이프라인은 켜는 시점에 깨끗합니다. 앞으로 govulncheck가 빨간불이 되는 경우는 **새 취약점이 공개될 때**인데, 그건 우리 코드 변경과 무관하게 아무 날에나 일어납니다. PR에서 non-blocking으로 두는 이유가 보통 이것입니다 — 남의 PR이 무관한 CVE 때문에 막히지 않게. 반대로 nightly가 필요한 이유도 이것입니다 — **아무도 PR을 안 올려도 새 CVE는 발견되어야 하므로.** Q3에서 nightly를 빼면 그 감시자가 사라집니다.

### 명확화 질문 1
`govulncheck` 게이트와 트리거를 어떻게 정리할까요?

A) **nightly 스케줄을 추가한다** (Q3을 C로 상향) — 트리거 = PR + main push + nightly. govulncheck는 **main·nightly에서 blocking, PR에서 non-blocking** (원래 Q6=C의 의도대로). 새 CVE가 공개되면 코드 변경이 없어도 야간에 잡힘. **(권장)**

B) **nightly 없이 간다** — 트리거 = PR + main push. govulncheck는 **main push에서 blocking, PR에서 non-blocking**. 새 CVE는 "누군가 main에 머지할 때" 비로소 발견됨 (조용한 기간에는 놓침)

C) **nightly 없이, govulncheck를 어디서나 blocking** (Q6을 A로) — 가장 엄격. 무관한 CVE가 공개되면 그날부터 모든 PR이 막힘

D) **nightly 없이, govulncheck를 어디서나 non-blocking** (Q6을 B로) — 리포트만

X) 기타

[Answer]: D

---

## 모순 2: 빌드하지 않는 이미지를 스캔할 수 없다

- **Q9 = A** — "**CI 범위 밖 — 테스트/검증만.** 이미지는 나중 증분에서"
- **Q11 = C** — "**컨테이너 이미지 스캔** (Trivy/Grype) — 베이스 이미지 CVE"

**CI가 이미지를 만들지 않으면 스캔할 이미지가 없습니다.** 세 가지 해석이 가능한데 결과물이 서로 다릅니다.

덧붙여 짚어둘 점: Q5=A에서 **Dockerfile의 빌드 스테이지를 `golang:1.26` 계열로 올리기로 하셨는데, Q9=A면 CI가 Dockerfile을 한 번도 빌드하지 않습니다.** 즉 Dockerfile이 깨져도 CI는 초록불입니다. B 이상을 고르면 이 구멍도 같이 막힙니다.

### 명확화 질문 2
이미지 빌드/스캔 범위를 어떻게 할까요?

A) **CI에서 이미지를 빌드하되 푸시하지 않고, 그 이미지를 Trivy로 스캔한다** (Q9를 B로 상향) — 레지스트리·자격증명 불필요. Dockerfile 회귀도 잡히고 베이스 이미지 CVE도 잡힘. **(권장)**

B) **이미지를 빌드하지 않고, Trivy를 파일시스템 모드(`trivy fs`)로 레포에 돌린다** — 이건 사실상 `govulncheck`와 겹칩니다(Go 의존성 스캔). **distroless 베이스 이미지의 CVE는 못 잡습니다** — Q11=C를 고르신 목적이 바로 그건데, 그 목적은 달성되지 않습니다

C) **이미지 스캔을 이번 증분에서 뺀다** (Q11에서 C 철회) — Q9=A 유지. 이미지 빌드/스캔/푸시/서명은 전부 나중 증분에서 한 묶음으로

X) 기타

[Answer]: A

---

## 답변을 마치셨으면

채팅에 **"완료"** 라고 알려주세요. 두 모순이 해소되면 `requirements.md`(CI 증분 섹션)를 작성하고 승인을 요청하겠습니다.
