# Infrastructure Design Plan — probe 증분 (unit: scan/probe)

**Requirements**: `inception/requirements/requirements-probe.md` (Q1=A 7개 전부·Q2=B 멀티스테이지 툴 이미지·Q3=A 생태계 pin·Q4=C·Q5=A).
**Scope decision**: `inception/requirements/scope-decision-active-vs-passive.md` (능동 5개 유지, 직결 배선).
**핵심**: 코드가 참조할 **런타임 실행환경(이미지+번들 바이너리+하드닝)** 확정.

## 이 증분 인프라 설계 대상
7개 툴을 버전 고정해 런타임 이미지에 번들하고, distroless 포기에 대한 하드닝 보완(NFR-P4)을 매핑한다.
현재 런타임: `gcr.io/distroless/static-debian12:nonroot`(셸·python 없음) → Q2=B로 교체.

| 카테고리 | 적용 | 비고 |
|---|---|---|
| Deployment Environment | ✅ | 런타임 베이스 이미지 교체(핵심) |
| Compute | N/A | 단일 이미지 role-flag, k8s/compose 이미 존재(변경=리소스 튜닝만) |
| Storage | N/A | Mongo/Valkey seam 불변, 이 증분 무관 |
| Messaging | N/A | WorkQueue 불변 |
| Networking | N/A | 프록시/포트 불변(financial은 §2) |
| Monitoring | N/A | OTel = infra-plan #3, 이 증분 무관 |
| Shared Infra | 부분 | 툴 번들 멀티스테이지 규율 = 후일 Chromium(§2/§4)과 공유 |

## 실행 단계 (설계 산출물)
- [x] **S1** 런타임 베이스 이미지 확정 (IQ1) + digest 핀 방식
- [x] **S2** Go 툴(httpx·subzy) 번들: build 스테이지 `go install @ver` → 바이너리 COPY, 버전 핀 표
- [x] **S3** testssl.sh 번들: git tag 체크아웃 + openssl 방식 확정 (IQ3)
- [x] **S4** Python 툴(humble·dirhunt·changeme·Corsy) 번들 + 격리 방식 확정 (IQ2), requirements 핀+해시
- [x] **S5** 하드닝 매핑 (NFR-P4): non-root, read-only 루트 FS + tmpfs 스크래치 (IQ5), cap 드롭
- [x] **S6** 통합 테스트 실행 위치 확정 (IQ4) — 빌드 이미지 내부 vs CI 러너 설치
- [x] **S7** deploy 매니페스트(k8s/compose) 반영: 새 이미지, read-only FS+tmpfs, 리소스 조정 노트
- [x] **S8** SECURITY-09 수정 리스크 매핑을 compliance-summary로 명문화
- [x] **S9** 산출물 생성: `construction/scan/infrastructure-design/probe-infrastructure-design.md` + `deployment-architecture.md`(델타) + `compliance-summary.md`

---

## 명확화 질문

### Question IQ1 — 런타임 베이스 이미지 (Q2=B의 "툴 전용 베이스" 구체화)
python3 + bash + openssl(testssl용)을 담아야 합니다. distroless는 포기하되 공격면은 최소화하고자 합니다.

A) **`python:3.12-slim-bookworm`** (Debian slim) — 친숙, `apt`로 openssl/git 설치, bash 기본. 생태계 표준·문서 풍부. 공격면은 Chainguard보다 큼.

B) **Chainguard/Wolfi python** (`cgr.dev/chainguard/python` + wolfi-base로 bash/openssl) — **distroless 철학에 가장 근접**(최소 패키지·CVE 거의 0·SBOM 기본·nonroot 기본). SECURITY-09 원의도를 최대한 보존. 단 apt 없음(apk/wolfi 패턴)·학습곡선.

C) **`debian:12-slim` 위에 python 직접 설치** — 베이스는 얇게, 필요한 것만 apt. B보다 통제 약함, A와 유사.

X) Other (please describe after [Answer]: tag below)

[Answer]: A

### Question IQ2 — Python 툴 4종 의존성 격리 (humble·dirhunt·changeme·Corsy)
4개 툴이 서로 다른 pip 의존(버전 충돌 가능)을 가질 수 있습니다.

A) **pipx 스타일 per-tool 격리 venv** — 툴마다 독립 venv. 충돌 원천 차단, 재현성 최상. pipx(또는 venv 수동) 추가.

B) **단일 공유 site-packages** — 한 번에 `pip install` + `requirements.txt` 핀. 단순하나 4툴 의존 충돌 시 해소 부담.

X) Other (please describe after [Answer]: tag below)

[Answer]: A

### Question IQ3 — testssl.sh의 openssl (cipher 커버리지 충실도)
testssl은 시스템 openssl로도 돌지만, 구형/희귀 cipher 탐지는 openssl 빌드에 좌우됩니다(PS-002 정확도).

A) **시스템 openssl** (베이스 제공분) — 단순·경량. 최신 openssl은 레거시 cipher를 빼서 일부 약한 cipher 미탐 가능(단, "약한 cipher 존재"는 대개 최신 openssl로도 탐지 가능).

B) **testssl 권장 bundled openssl** (프로젝트가 배포하는 정적 openssl 바이너리 동봉) — 레거시/희귀 cipher까지 최대 커버리지. 이미지에 바이너리 1개 추가.

X) Other (please describe after [Answer]: tag below)

[Answer]: B

### Question IQ4 — 통합 exec 테스트 실행 위치 (Q4=C의 통합 파트)
실 바이너리 대상 골든 파서 검증(`//go:build integration`)을 어디서 돌릴지.

A) **빌드된 probe 이미지 안에서 실행** — 7개 툴이 실제로 설치된 그 이미지에서 `go test -tags integration`. "배포되는 것과 동일 환경" 보장. CI가 이미지 빌드 후 그 안에서 테스트.

B) **CI 러너에 툴 설치 후 실행** — 기존 Mongo/Valkey testcontainers 잡처럼 러너에 툴 설치. 이미지 빌드와 분리, 빠름. 단 "이미지와 동일 환경" 보장은 약함.

X) Other (please describe after [Answer]: tag below)

[Answer]: A

### Question IQ5 — read-only 루트 FS 하의 툴 스크래치 공간 (NFR-P4)
일부 툴은 임시 파일을 씁니다(dirhunt 캐시 등). read-only 루트 FS를 유지하려면 쓰기 가능 영역이 필요.

A) **tmpfs `emptyDir`(메모리) 작업공간** — `/tmp` 등을 tmpfs로. 디스크 잔여물 없음(FR-12 wipe 정합), 프로세스 종료 시 소멸. 메모리 예산 필요.

B) **디스크 `emptyDir` 작업공간** — 노드 디스크 사용. 대용량 스크래치 가능, 종료 시 정리. 메모리 부담 없음.

X) Other (please describe after [Answer]: tag below)

[Answer]: B
