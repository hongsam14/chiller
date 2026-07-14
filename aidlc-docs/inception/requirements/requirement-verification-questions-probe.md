# Probe CLI 배선 — 요구사항 확인 질문

이 증분의 목표: **7개 probe CLI(humble · httpx · testssl.sh · dirhunt · changeme · Corsy · subzy)를 실제로 배선**하여,
지금 `tool-failed`(무판정)로 보고되는 12개 KISA 항목을 실제 판정하도록 만든다.
(infra-plan §3, aidlc-state Backlog TOP PRIORITY)

## 배경 — 각 질문에 필요한 사실

프레임워크(`runner`: subprocess exec + timeout + proxy env + parse hook)는 **이미 완성·테스트됨**.
남은 건 툴별로 (1) `argv` 함수, (2) 실제 `Parser`(stdout→`domain.Finding`), (3) **이미지에 바이너리 번들** 세 가지뿐이다.
`argv`가 non-nil이 되는 순간 `Adapter.Wired()`가 true로 뒤집히고 해당 항목이 tool-failed → checked로 이동한다.

**핵심 제약**: 현재 런타임 이미지는 `gcr.io/distroless/static-debian12:nonroot` — 셸·libc·python이 **없다**.
7개 툴의 실행 환경 요구가 서로 다르다:

| 툴 | 종류 | 실행 요구 | distroless static에서 실행? |
|---|---|---|---|
| httpx (projectdiscovery) | 정적 Go 바이너리 | 없음 | ✅ 가능 |
| subzy | 정적 Go 바이너리 | 없음 | ✅ 가능 |
| testssl.sh | bash 스크립트 | bash + openssl/coreutils | ❌ 불가 |
| humble | Python | python3 런타임 | ❌ 불가 |
| dirhunt | Python | python3 런타임 | ❌ 불가 |
| Corsy | Python | python3 런타임 | ❌ 불가 |
| changeme | Python | python3 런타임 | ❌ 불가 |

즉 **httpx·subzy 2개만 현재 베이스에서 바로 돌고, 나머지 5개는 이미지 아키텍처 변경을 강제**한다.
이것이 이번 증분의 가장 큰 결정이다 (SECURITY-09 = distroless nonroot 준수와 정면 충돌).

---

## Question 1
이번 증분의 **배선 범위**를 어디까지로 할까요?

A) **전부 7개** — humble·httpx·testssl·dirhunt·changeme·Corsy·subzy 모두 이번에 배선. 12개 항목 전부 판정. (이미지 아키텍처 대변경 수반)

B) **Go-native 2개 먼저** (httpx·subzy) — distroless static을 유지한 채 바로 되는 것부터. httpx=PS-007/020, subzy=PS-025 → 3개 항목 확보. 나머지 5개(Python/bash)는 다음 증분으로 명시적 이월.

C) **Go-native 2개 + testssl.sh** — 위 B에 TLS(PS-002, 컴플라이언스 상 중요)를 더함. bash+openssl만 추가하면 되므로 python 5종보다 이미지 부담이 작음.

X) Other (please describe after [Answer]: tag below)

[Answer]:

---

## Question 2
Python/bash 툴을 실행하려면 **런타임 이미지 전략**을 정해야 합니다. (SECURITY-09는 현재 distroless nonroot를 요구)
※ Q1에서 B(Go 2개만)를 고르면 이 질문은 **N/A** — 현재 distroless static 그대로 유지됩니다.

A) **단일 fat 이미지로 전환** — `debian-slim`/`ubuntu` 등 python3+bash 포함 베이스로 런타임을 교체. 가장 단순하나 distroless 이점(공격면 최소화) 포기. SECURITY-09 재협상 필요.

B) **멀티 스테이지 + 툴 전용 베이스** — chiller 바이너리는 그대로, 런타임 베이스만 필요한 인터프리터를 담은 최소 이미지(예: `python:3.12-slim`에 정적 chiller COPY)로. A보다 통제되지만 여전히 non-distroless.

C) **사이드카/별도 이미지 분리** — probe 툴들을 별도 컨테이너로 두고 chiller가 호출. 격리는 최상이나 프로세스 exec 모델(`os/exec`)을 네트워크 호출로 바꿔야 해서 코드 변경 큼.

D) **distroless 유지 + Python/bash 툴은 이번에 안 함** — 결국 Q1=B와 동일 결론. 이미지 정책 불변.

X) Other (please describe after [Answer]: tag below)

[Answer]:

---

## Question 3
바이너리 **버전 고정(pinning) 방식** (SECURITY-10: 재현 가능·공급망 무결성). 툴 종류별로 방식이 다릅니다.

A) **툴별 표준 방식 혼용** — Go 툴은 `go install tool@vX.Y.Z`(빌드 스테이지), Python 툴은 `pip install tool==X.Y.Z` + `requirements.txt` 해시 고정, testssl.sh는 특정 git tag/commit 체크아웃. 각 생태계의 관용을 따름.

B) **릴리스 바이너리 + sha256 검증 일괄** — 가능한 건 배포된 릴리스 아티팩트를 받아 체크섬 검증 후 배치. 생태계 무관하게 동일 패턴.

X) Other (please describe after [Answer]: tag below)

[Answer]:

---

## Question 4
`defaultExec`(실제 `os/exec` 경로)는 **아직 한 번도 실행된 적이 없습니다**(테스트 커버리지 0). infra-plan은 "최소 1회 실제 바이너리로 실행 검증"을 요구합니다. 어떻게 증명할까요?

A) **`//go:build integration` 통합 테스트** — 기존 Mongo/Valkey 통합 테스트와 같은 패턴. 실제 핀된 바이너리(또는 컨테이너) 대상으로 `defaultExec`를 돌려 파싱까지 검증. CI integration 잡에서 실행.

B) **경량 실 바이너리 테스트** — `echo`/`true` 같은 항상 존재하는 최소 바이너리로 `defaultExec`의 exec·timeout·env 주입 경로만 유닛 레벨에서 밟고, 실제 툴 파싱은 통합 테스트로 분리.

C) **A + B 둘 다** — 유닛에서 exec 경로 커버 + 통합에서 각 실툴 파서 골든 출력 검증.

X) Other (please describe after [Answer]: tag below)

[Answer]:

---

## Question 5
**Parser 충실도** — 각 툴 stdout을 `domain.Finding`으로 변환하는 수준.

A) **JSON 출력 모드 우선** — 지원 툴은 JSON 플래그 사용(httpx `-json`, testssl `--jsonfile`, subzy 등). 파싱 안정성 최상. 골든 출력 픽스처로 회귀 테스트.

B) **각 툴이 판정한 것만 최소 매핑** — 툴이 "취약"이라 한 항목만 소유 CWE로 Finding 생성, 세부 evidence는 최소화. 빠르지만 리포트 정보량 적음.

X) Other (please describe after [Answer]: tag below)

[Answer]:

---

## Question 6
배선되지 **않은** 툴의 보고 방식(현재 `ErrNotWired` → `StatusToolFailed`, 무판정). Q1에서 일부만 배선할 경우 나머지 처리.

A) **현행 유지** — 미배선 툴은 계속 `tool-failed`(정직한 무판정)로 보고. 회귀 방지 테스트(`TestE2E_UnwiredProbes_…`)의 기대 카운트를 이번에 배선한 만큼만 갱신. (2026-07-13 정직성 수정을 절대 되돌리지 않음)

X) Other (please describe after [Answer]: tag below)

[Answer]:
