# Infrastructure Design Plan — increment: persistence (findings U3 + scan U2)

**Stage**: CONSTRUCTION → Infrastructure Design (증분, 2개 유닛 교차)
**Units**: `findings` (U3) · `scan` (U2)
**Created**: 2026-07-13
**Scope (사용자 선택)**: infra-plan.md §7 항목 2 — **FindingsStore → Mongo** (§1 #6) + **ResponseCache → Valkey** (§1 #5)
(seam-swap 원칙: mem 구현 **옆에** 새 구현 추가, `cmd/chiller`에서만 선택)

**아티팩트 배치 결정**: 이 증분은 두 유닛(findings·scan)에 걸쳐 있으나 하나의 일관된 인프라 결정 묶음이므로,
직전 플랫폼 증분과 같은 방식으로 **단일 위치**에 둔다:
`aidlc-docs/construction/persistence/infrastructure-design/{infrastructure-design,deployment-architecture,compliance-summary}.md`
(`construction/shared-infrastructure.md`는 갱신, 신규 생성 아님 — 배치 전략은 이미 확정됨)

**입력 아티팩트 (로드 완료)**:
- `construction/infrastructure-design/infra-plan.md` (§0 원칙, §1 표 #5·#6, §7 순서 2)
- `construction/shared-infrastructure.md` (Q9=C — Mongo 인스턴스 공유·컬렉션 분리 / Valkey 큐·캐시 **인스턴스 분리**; #5/#6 배치 **선반영 완료**)
- `construction/platform/infrastructure-design/*` (직전 증분: Mongo/Valkey 구현 패턴·계약 스위트·통합 테스트·배포 자산)
- `construction/findings/functional-design/*`, `construction/scan/functional-design/*`
- `construction/platform/code/{business-logic-summary,infra-business-logic-summary,lease-business-logic-summary}.md`
- 확장 규칙 (모두 Enabled, blocking): `security-baseline`, `resiliency-baseline`, `property-based-testing` (FULL)

---

## 0. 코드베이스 실사 결과 (질문 설계의 근거)

**infra-plan.md §1 #5/#6의 가정 중 두 가지가 사실과 다릅니다.** 특히 F-1은 이 증분의 성격 자체를 바꿉니다.

| # | 실사 항목 | 확인된 사실 | 영향 |
|---|---|---|---|
| **F-1** | **findings는 이미 Mongo에 저장되고 있다** | `platform.Scan.Report *report.Report` 에 `bson:"report"` 태그가 있고, `report.Report.Findings []domain.Finding` 이다. `--store=mongo` 로 뜨면 finalize의 `Store.SaveReport(ctx,id,rep)` 가 **모든 finding을 `scans` 문서 안에 임베드해서 이미 durable하게 쓴다.** `FindingsStore`(mem)는 그와 **별개로 중복 보관**하는 두 번째 사본이다. | infra-plan의 "#6 = 영속화 추가" 프레이밍이 **부정확**. 실제 과제는 ① 이 **이중 저장(중복)의 정리**, ② `findings` 전용 컬렉션 분리, ③ **at-rest 암호화**(현재 **평문**), ④ 16MB BSON 문서 한계. → **Q1** |
| **F-2** | **`FindingsStore` 인터페이스에 `ctx`가 없다** | `SaveFindings(scanID, []Finding) error` / `LoadFindings(scanID) ([]Finding, error)`. 취소·타임아웃 전달 불가 | E-6 규칙(ctx-first)을 **위반**. Mongo 구현에 필수 → **인터페이스 변경 불가피**. PR-11("인터페이스 불변")과 충돌 → **Q2** |
| **F-3** | **`ResponseCache` 인터페이스에 `ctx`도 `error`도 없다** | `Get(scanID, url) (*HAREntry, bool)` — 네트워크 백엔드의 실패를 표현할 수단이 **없다** | Valkey 구현이 에러를 삼키거나(조용한 오동작) 시그니처를 바꿔야 함 → **Q2**, 장애 시 동작은 **Q8** |
| **F-4** | **`WipeScan`이 아무 곳에서도 호출되지 않는다** | `grep WipeScan` → 정의(`cache.go`)와 자기 자신뿐. orchestrator finalize·stop·fail 어디에도 호출 없음 | **FR-12 / NFR-16 (스캔 종료 시 응답 본문 폐기)가 현재 미충족.** mem에서는 프로세스 종료로 가려졌지만 Valkey로 가면 **본문이 백엔드에 잔류**하는 실제 데이터 유출 → **Q3** (이번 증분에서 배선 필요) |
| **F-5** | **`domain.Finding`/`Evidence`/`HAREntry`에 직렬화 태그 없음** | `Finding{CWEs, TargetURL, SourceTool, Severity, Evidence}` — bson 태그 전무. 직전 증분은 `platform.Scan`·`WorkLedgerEntry`·`domain.WorkItem`·`Target`에만 태그를 넣었다 | Mongo가 Go 필드명 소문자화로 저장 중(`cwes`,`targeturl`,…). 필드 리네임 = 조용한 스키마 파괴. 직전 증분 정책(Q4=A)과의 **일관성 필요** → **Q4** |
| **F-6** | **`Severity`가 int iota이고 문자열 마샬링이 없다** | `type Severity int`; `SevInfo,SevLow,…` iota. 직전 증분은 `ScanState`/`ItemState`에 대해 **문자열 마샬링**을 도입했으나 `Severity`는 손대지 않았다 | `Severity`가 **이미 Mongo에 int로 저장 중** → 상수 순서가 바뀌면 기존 문서가 조용히 오염되는 **잠재 결함이 이미 존재**. 같은 증분에서 정리 대상 → **Q4** |
| F-7 | 캐시 호출부는 2곳뿐 | `fetch.fetcher.Fetch` (Get/Put) · `exec.Executor.Run`(주입 전달) · `cmd/chiller`(조립). 그 외 없음 | 시그니처 변경 파급이 작다 (Q2의 비용 낮음) |
| F-8 | findings 호출부는 2곳뿐 | `orchestrator.finalize` (`SaveFindings`) · `cmd/chiller`(조립). **`LoadFindings`는 프로덕션 코드에서 호출자가 없다** (테스트만) | Load 경로가 실사용되지 않음 → 읽기 API(`/api/scans/{id}/findings`)를 노출할지 결정 필요 → **Q1** |
| F-9 | 재사용 가능한 구현 패턴 존재 | `taskstore_mongo.go`(findOneAndUpdate CAS, majority WC, Ping 헬스체크) · `workqueue_valkey.go`(go-redis/v9, 스트림, lazy drain) · `contract_test.go`(생성자 테이블 계약 스위트) · `//go:build integration` + testcontainers · Dockerfile/compose/k8s | 새 백엔드는 **같은 골격을 그대로 따른다**. 계약 스위트 추출 리팩터가 **이미 되어 있음**(직전 F-2 해소) → 이번엔 선행 리팩터 불필요 |
| F-10 | 의존성 이미 존재 | `mongo-driver v1.17.9`, `go-redis/v9 v9.20.1`, `testcontainers v0.37.0` | **신규 의존성 추가 없음** (암호화는 표준 `crypto/aes`+`crypto/cipher`로 충분) |
| F-11 | Valkey-cache 인스턴스는 아직 없음 | `cmd/chiller`에 `--valkey-addr` 하나뿐(큐 전용). compose/k8s에도 Valkey 1대 | shared-infrastructure §2가 **큐/캐시 인스턴스 분리**를 이미 확정 → 두 번째 Valkey 인스턴스 + 플래그/시크릿/매니페스트 추가 필요 |

**F-1·F-4·F-6은 "새 기능 추가"가 아니라 "이미 존재하는 결함의 해소"입니다.** 이 증분의 실제 가치가 여기에 있습니다.

---

## 1. 실행 플랜 (체크박스)

> 규율: 각 단계 완료 즉시 해당 체크박스를 `[x]`로 갱신한다 (Plan-Level Checkbox Enforcement).

### Phase 0 — 설계 선행 조건 확정 (이 문서에서 해결)
- [x] P0-1: 입력 아티팩트 로드 (infra-plan, shared-infrastructure, 직전 증분 인프라 설계, FD, 코드 요약)
- [x] P0-2: 코드베이스 실사 — 두 seam 시그니처·호출부·직렬화·기존 Mongo 저장 실태 (§0 표 F-1..F-11)
- [x] P0-3: infra-plan 가정 검증 → F-1(이중 저장)·F-2/F-3(ctx 부재)·F-4(WipeScan 미배선)·F-6(Severity int) 발견, 질문으로 승격
- [x] P0-4: 확장 규칙 적용 범위 사전 평가 (§3 표)
- [x] P0-5: 질문 생성 (§2, Q1–Q12)
- [x] P0-6: 답변 수집 + 모순 검사 → clarification 라운드 1 (**C1=A**, **C2=B**) 해소. **모든 질문 확정**: Q1=A·Q2=A·Q3=A·Q4=A·Q5=A·Q6=A·Q7=C·Q8=A·Q9=A·Q10=A·Q11=A·Q12=A·C1=A·C2=B
- [x] P0-7: 실사 정정 — F-1은 `scans`가 아니라 **`reports` 컬렉션**(F-1′)이며, `platform.Scan.Report`는 **아무도 설정하지 않는 죽은 필드**(F-1″, 대입 지점 0건). 설계는 정정된 사실 위에서 수행

### Phase 1 — 인프라 설계 아티팩트 생성 (답변 확정 후)
- [x] P1-1: `construction/persistence/infrastructure-design/infrastructure-design.md` — 실사 정정(§0), seam 계약 정정(§2), findings→Mongo(§3: 전용 컬렉션·스키마·인덱스·직렬화·FindingKey·봉투 암호화·보존·재시도), ResponseCache→Valkey(§4: 키/값/TTL·암호화와 regex의 관계·WipeScan 배선·fail-open), 관측(§5), 테스트(§6), composition root(§7), 데이터 계약(§8), 해소되는 기존 결함(§9)
- [x] P1-2: `construction/persistence/infrastructure-design/deployment-architecture.md` — 토폴로지(Mermaid+텍스트 대안), Valkey 2-인스턴스 근거, 시크릿, 배포 자산 변경, 장애 격리 매트릭스, 공유 Mongo 트레이드오프+승격 경로, 배포/롤백/스키마 파괴 경고
- [x] P1-3: `construction/shared-infrastructure.md` **갱신** — #5/#6을 "다음"에서 **"확정(2026-07-13)"** 으로 승격, 경계 규칙(findings 정본 단일화)·캐시 암호화·WipeScan 미배선 사실·`CHILLER_EVIDENCE_KEY` 반영
- [x] P1-4: 데이터 계약 명세 — infrastructure-design.md §8 (컬렉션·인덱스·TTL·키 네임스페이스·봉투 포맷·enum 표현·스키마 버전·마이그레이션)
- [x] P1-5: 확장 규칙 컴플라이언스 표 — `compliance-summary.md` (SECURITY 13C/2NA · RESILIENCY 9C/6NA · PBT 10C/0NA, **blocking 0**)
- [x] P1-6: 콘텐츠 검증 — Mermaid 노드 ID 영숫자, 라벨은 따옴표로 감싸고 `<br/>`만 사용(특수문자 없음), 텍스트 대안 병기 (content-validation.md 준수)
- [x] P1-7: 완료 메시지 제시 + 명시적 승인 대기 ← **현재 여기**

### Phase 2 — Code Generation으로 이월 (여기서는 설계만)
- [ ] P2-*: 코드는 다음 스테이지(Code Generation)에서 별도 플랜으로 생성

---

## 2. 질문 (Q1–Q12)

> **답변 방법**: 각 질문의 `[Answer]:` 뒤에 선택지 문자(A/B/C/D)를 적어 주세요. 복수 선택이 가능한 항목은 명시했습니다.
> 선택지가 마음에 들지 않으면 자유 서술해도 됩니다. 이유를 덧붙이면 설계에 반영합니다.

---

### Q1 — [Storage] findings 이중 저장(F-1)을 어떻게 정리할 것인가 ★가장 중요

**사실**: `--store=mongo` 상태에서 finalize는 `SaveReport`로 **모든 finding을 `scans` 문서 안에 임베드**해 이미 durable하게 저장한다. 동시에 `FindingsStore`(mem)가 같은 데이터를 **한 번 더** 들고 있다. 즉 "FindingsStore→Mongo"를 순진하게 구현하면 **같은 데이터가 Mongo에 두 벌** 저장된다.
16MB BSON 문서 한계도 걸린다 — 대규모 스캔에서 findings가 커지면 `scans` 문서가 통째로 쓰기 실패한다.

- **A) findings 전용 컬렉션으로 단일화 (권장)** — `findings` 컬렉션에 **finding 당 1 문서**(`{_id: scanId+findingKey, scanId, …}`)로 저장하고, `Report.Findings`는 **Mongo에 임베드하지 않는다**(저장 시 비우고, 읽을 때 `FindingsStore`에서 재구성 = report 조립). 16MB 한계 해소 + 부분 쿼리/페이징 가능 + 암호화 대상이 한 곳으로 모임. 대신 report 읽기 경로에 조인(2 쿼리)이 생기고 `LoadFindings`가 처음으로 실사용된다.
- **B) 임베드 유지, FindingsStore는 그 위의 얇은 뷰** — `FindingsStore.Mongo`가 `scans.report.findings`를 읽고 쓰는 어댑터가 된다. 저장소 한 벌, 코드 변경 최소. 그러나 16MB 한계 그대로, 컬렉션 단위 최소 권한(SECURITY-06)·컬렉션 단위 암호화가 불가능, shared-infrastructure의 `findings` 컬렉션 분리 결정과 배치가 어긋남.
- **C) 이중 저장을 의도된 것으로 인정** — `findings` 컬렉션(정본, 암호화) + `scans.report`(스냅샷, 요약만). 읽기 빠름, 쓰기 2배, 두 사본의 정합성 관리 부담.
- **D) 잘 모르겠다 — 트레이드오프를 더 설명해 달라**

[Answer]: A

---

### Q2 — [Storage/계약] seam 인터페이스에 `ctx`(및 `error`)를 추가할 것인가 (F-2/F-3)

**사실**: `FindingsStore`에 `ctx` 없음. `ResponseCache`는 `ctx`도 `error`도 없음(`Get`이 `(*HAREntry, bool)`). 네트워크 백엔드에는 **취소·타임아웃·에러 전달이 필수**다. PR-11("Infra 교체 시 인터페이스 불변")은 **platform seam(이미 ctx-first)** 을 전제로 한 규칙이라, 이 두 seam에는 애초에 성립하지 않는다.

- **A) 두 인터페이스를 E-6 규칙에 맞게 정정 (권장)** — `SaveFindings(ctx, scanID, findings) error` / `LoadFindings(ctx, scanID) ([]Finding, error)`; `Put(ctx,…) error` / `Get(ctx, scanID, url) (*HAREntry, bool, error)` / `WipeScan(ctx, scanID) error`. 호출부는 4곳뿐(F-7/F-8)이라 파급 작음. PR-11은 "이미 ctx-first인 seam"에 적용되는 규칙임을 문서에 명시하고, 이 정정을 **일회성 계약 수정**으로 기록.
- **B) 인터페이스 유지, 구현이 내부에서 흡수** — Mongo/Valkey 구현이 `context.Background()`+고정 타임아웃을 쓰고, `Get`의 에러는 miss로 삼킨다. 코드 변경 0. 대신 **스캔 취소가 백엔드 호출을 끊지 못하고**, 캐시 백엔드 장애가 조용히 전 URL 재fetch로 번진다(관측 불가).
- **C) 기존 인터페이스 유지 + ctx 버전 인터페이스 병존** — 어댑터로 연결. 표면이 2배가 되고 어느 쪽이 정본인지 흐려짐.
- **D) 잘 모르겠다**

[Answer]: A

---

### Q3 — [Security/Storage] `WipeScan` 미배선(F-4)을 이번 증분에서 고칠 것인가

**사실**: `WipeScan`은 **호출되는 곳이 없다**. FR-12/NFR-16("스캔 종료 시 응답 본문 폐기")이 지금 **미충족**이며, mem 캐시라 프로세스 종료로 가려져 있었다. Valkey로 옮기면 **응답 본문이 백엔드에 그대로 잔류**한다(실질적 데이터 잔존 리스크, SECURITY-03).

- **A) 배선 + TTL 이중 안전망 (권장)** — orchestrator의 **모든 종료 경로**(completed / stopped / failed)에서 `WipeScan` 호출 + Valkey 키에 **TTL도 함께** 설정해 프로세스가 죽어도 만료로 반드시 사라지게 한다(수동 삭제 실패에 대한 백스톱).
- **B) TTL만** — 코드 배선 없이 키 TTL에 의존. 단순하지만 스캔 종료 후 TTL 만료까지 본문이 남는다.
- **C) 배선만** — TTL 없음. 프로세스가 죽으면 영구 잔류.
- **D) 별도 작업으로 분리** — 이번 증분 범위 밖으로 미룬다.

[Answer]: A

---

### Q4 — [Storage/데이터계약] 직렬화 태그와 enum 표현 (F-5/F-6)

**사실**: `domain.Finding`/`Evidence`/`HAREntry`에 bson 태그 없음 → Go 필드명 소문자화로 저장 중. `Severity`는 **int iota**이고 문자열 마샬링이 없어 **이미 Mongo에 int로 저장되고 있다**(상수 순서 변경 시 조용한 오염 — 잠재 결함). 직전 증분은 platform 엔티티에 대해 **명시적 bson 태그 + enum 문자열 마샬링**을 채택했다(Q4=A).

- **A) 직전 증분과 동일 정책을 확장 (권장)** — `Finding`/`Evidence`/`HAREntry`/`Cookie`/`Header`에 명시적 `bson:` 태그 추가 + `Severity`에 문자열 마샬러(`"low"`,`"high"`,…) 도입 + `schemaVersion` 필드. **기존 int 문서 마이그레이션 필요**(현재 프로덕션 데이터 없음 → 지금이 가장 싼 시점).
- **B) 태그만 추가, `Severity`는 int 유지** — 마이그레이션 회피. iota 오염 리스크 존치.
- **C) 아무것도 안 함** — 암묵적 필드명 유지.
- **D) 잘 모르겠다**

[Answer]: A

---

### Q5 — [Security] findings의 at-rest 암호화 방식 (SECURITY-01/03)

**사실**: infra-plan #6은 "**암호화된 증거**"를 요구한다. 현재 evidence snippet은 **평문**으로 Mongo에 저장 중(F-1). 응답 본문 조각이라 민감정보(토큰·세션·PII)를 포함할 수 있다.

- **A) 앱 레벨 봉투 암호화 — evidence 필드만 (권장)** — `Evidence.Snippets`를 AES-256-GCM으로 암호화해 `{v, nonce, ct}` 봉투로 저장. DEK는 env/시크릿 주입(`CHILLER_EVIDENCE_KEY`), 키 회전은 봉투의 키ID로. 백엔드 무관(Mongo Community에서도 동작), 검색 불필요한 필드라 손실 없음.
- **B) Mongo CSFLE (client-side field level encryption)** — 드라이버 기능 사용. 표준적이지만 **key vault + mongocryptd/crypt_shared 바이너리** 운영 부담이 붙고, 컨테이너 이미지가 커진다.
- **C) 백엔드 디스크 암호화에 위임** — 앱은 평문 저장, 스토리지 레벨 암호화만. 가장 단순하지만 **DB 접근 권한을 얻은 자에게는 평문**이고 SECURITY-03 요구를 앱이 보증하지 못함.
- **D) 잘 모르겠다**

[Answer]: A

---

### Q6 — [Security] ResponseCache(Valkey) 값의 암호화

**사실**: 캐시 값은 **응답 본문 전체**(bounded)다. findings evidence보다 **훨씬 민감**하다. 대신 스캔 수명 동안만 존재하고 매 접근마다 복호화 비용이 든다.

- **A) 값 전체를 AES-256-GCM으로 암호화 (권장)** — Q5와 같은 키/봉투 메커니즘 재사용. Valkey는 불투명 바이트만 본다.
- **B) TLS in-transit만, 값은 평문** — 캐시 인스턴스 접근 통제(ACL+네트워크)에 의존. 빠르지만 Valkey 메모리 덤프에 본문 노출.
- **C) 본문 제외 — 헤더/쿠키/상태만 캐시** — 민감도 최소화. 그러나 **PS-003(평문 로그인 폼)·JS 자산 탐색 등 본문이 필요한 시그니처가 캐시 재사용을 못 하게 되어** FR-02(단일 fetch 재사용)가 사실상 무력화됨.
- **D) 잘 모르겠다**

[Answer]: A를 하면 Observation에서 regex 탐색을 할 때, 어떻게 해?

---

### Q7 — [Storage] findings 보존 정책 (retention)

- **A) 무기한 보존** — 외부 소비자(ASM/보고 파이프라인)가 언제든 조회. 스토리지 무한 증가.
- **B) TTL 인덱스로 N일 후 자동 삭제** — 기본 90일 제안, 플래그로 조정 가능. 컴플라이언스 친화적.
- **C) 코드는 TTL을 설정 가능하게만 하고 기본값은 무기한** — 운영 정책으로 위임.
- **D) 잘 모르겠다**

[Answer]: C

---

### Q8 — [Resiliency] 캐시 백엔드(Valkey-cache) 장애 시 동작 (RESILIENCY)

**사실**: 캐시는 **성능 최적화**(FR-02 fetch 재사용)이지 정확성 요소가 아니다. 다만 캐시 미스는 재fetch를 유발하고, 재fetch는 **대상 서버에 부하**를 준다(rate limiter는 여전히 적용됨).

- **A) fail-open — 에러를 miss로 처리하고 재fetch (권장)** — 가용성 우선. 캐시가 죽어도 스캔은 (느리게) 완주. 에러는 **메트릭/로그로 반드시 노출**(조용히 삼키지 않음 → Q2=A가 전제).
- **B) fail-closed — 캐시 에러 시 스캔 실패** — 안전하지만 최적화 계층의 장애가 전체를 죽인다.
- **C) 하이브리드 — 에러율이 임계치를 넘으면 회로 차단 후 캐시 우회(mem 폴백)** — 복잡도 증가.
- **D) 잘 모르겠다**

[Answer]: A

---

### Q9 — [Resiliency] findings 저장(Mongo) 실패 시 동작

**사실**: 현재 `finalize`는 `SaveFindings` 실패 시 곧바로 `fail(scan)` — 스캔 전체가 failed가 된다. 스캔은 이미 다 돌았는데 저장만 실패한 상황이다.

- **A) 유한 재시도(지수 백오프) 후 실패 시 scan=failed (권장)** — 일시적 장애를 흡수. Mongo 드라이버의 재시도 가능 쓰기 + majority write concern과 결합.
- **B) 현행 유지 — 즉시 failed** — 단순.
- **C) 로그만 남기고 report는 반환(스캔은 completed)** — 결과 유실을 조용히 허용 → 반대.
- **D) 잘 모르겠다**

[Answer]: A

---

### Q10 — [Testing] 테스트 전략 (PBT 확장 = FULL, blocking)

- **A) 직전 증분과 동일 (권장)** — ① mem/Mongo/Valkey 공용 **계약 스위트**(생성자 테이블) 추출·확장 ② **PBT**: findings 저장 라운드트립 불변식(정규화·`MaxEvidenceSnippets` 경계 보존·암복호 항등), 캐시 put/get/wipe 상태 모델 ③ **`//go:build integration` + testcontainers**로 실제 Mongo/Valkey 검증(Docker 필요, CI 잡).
- **B) 계약 스위트 + 통합 테스트만 (PBT 생략)** — PBT 확장이 FULL로 켜져 있어 **blocking 위반**이 됨.
- **C) 유닛 테스트만** — 실 백엔드 미검증.
- **D) 잘 모르겠다**

[Answer]: A

---

### Q11 — [Deployment/Shared] Valkey-cache 인스턴스 추가 (F-11)

**사실**: shared-infrastructure §2는 **큐용 Valkey와 캐시용 Valkey를 별도 인스턴스**로 확정했다(정책 충돌: 큐=`noeviction`+AOF / 캐시=`allkeys-lru`+휘발성). 현재 배포 자산에는 Valkey가 **1대뿐**이다.

- **A) 확정 대로 별도 인스턴스 (권장)** — `--cache-addr`/`CHILLER_VALKEY_CACHE_ADDR` 플래그+시크릿 추가, compose/k8s에 `valkey-cache` 서비스(`maxmemory` + `allkeys-lru`) 추가. `--cache=mem|valkey` 스위치.
- **B) 단일 Valkey 인스턴스에 DB 인덱스/키 접두사로만 분리** — 리소스 절약. 그러나 **캐시 LRU eviction이 큐 항목을 evict**하거나 큐의 `noeviction`이 캐시 쓰기를 OOM으로 막는다(shared-infrastructure §2가 명시적으로 기각한 안).
- **C) 로컬/개발은 1대, 프로덕션은 2대** — compose는 1대, k8s는 2대. 환경 간 동작 차이가 생김.
- **D) 잘 모르겠다**

[Answer]: A

---

### Q12 — [Monitoring] 이번 증분의 관측 범위

**사실**: OTel은 infra-plan #5(다음 증분)다. 지금은 `/healthz`(deep) + `/api/introspect/*` + `/ui` 대시보드가 관측 창구다.

- **A) 기존 창구 확장만 (권장)** — `/healthz`에 Mongo(findings)·Valkey-cache Ping 추가, `/api/introspect`에 **캐시 hit/miss/에러 카운터**와 findings 저장 지연을 노출, 대시보드에 표시. OTel 익스포터는 다음 증분에서 이 카운터를 그대로 읽어간다.
- **B) 이번 증분에서 OTel까지 함께** — 범위 확대(다음 증분 선취).
- **C) 로그만** — 관측 없음.
- **D) 잘 모르겠다**

[Answer]: A

---

## 3. 확장 규칙 적용 범위 사전 평가 (blocking)

| 확장 | 이 단계 적용 항목 | 대응 |
|---|---|---|
| **security-baseline** | SECURITY-01(전송/저장 암호화) · SECURITY-03(민감정보 비저장/폐기) · SECURITY-06(최소 권한·네임스페이스) · SECURITY-09/12(시크릿 관리) · SECURITY-10(공급망) | Q5·Q6(암호화), Q3(폐기/wipe), Q11(키 네임스페이스·ACL), F-10(신규 의존성 없음 → 공급망 표면 불변) |
| **resiliency-baseline** | RESILIENCY-04(재시도/백오프) · RESILIENCY-05(관측) · RESILIENCY-07(장애 격리) · RESILIENCY-10(공유 자원 트레이드오프 문서화) | Q8(캐시 fail-open), Q9(저장 재시도), Q12(관측), shared-infrastructure §1 트레이드오프 |
| **property-based-testing (FULL)** | 저장/캐시 라운드트립 불변식, 경계(`MaxEvidenceSnippets`), 암복호 항등, wipe 후 잔존 없음 | Q10=A |

N/A 판정: 컴퓨트 사이징·메시징(이번 증분에 큐 변경 없음)·API 게이트웨이/네트워크 토폴로지(변경 없음) — 직전 증분에서 확정, 재결정 불필요.

---

## 4. 다음 단계

사용자가 위 `[Answer]:` 를 채우면 → P0-6(모순 검사) → Phase 1(설계 아티팩트 생성) → 승인 → Code Generation.
