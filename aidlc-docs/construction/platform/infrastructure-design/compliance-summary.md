# Extension Compliance Summary — Infrastructure Design (platform / U1)

이 스테이지 산출물은 **설계 문서**다. 코드 수준 검증은 다음 Code Generation 스테이지로 이월되며, 여기서는 설계가 각 규칙의 **근거를 제공하는지**를 판정한다. 세 확장 모두 Enabled·blocking (aidlc-state §Extension Configuration).

**결론: blocking finding 없음.** 모든 적용 규칙이 설계 수준에서 compliant이거나, 근거와 함께 N/A로 표기됨.

---

## Security Compliance (SECURITY-01 … 15)

| 규칙 | 판정 | 근거 |
|---|---|---|
| SECURITY-01 전송/저장 암호화 | **Compliant** | Mongo `tls=true`+WiredTiger encryption-at-rest, Valkey `tls=true`; 평문 접속 거부 (infra-design §2.1/§3.1) |
| SECURITY-02 네트워크 중개 접근 로깅 | **N/A (설계)** | 이번 스코프는 LB/GW/CDN 도입 없음. 운영 배포 시 K8s Ingress 접근 로그는 배포 매니페스트 소관 |
| SECURITY-03 앱 로깅 | **Compliant** | 백엔드 호출 실패 구조적 로그(correlation=ScanID/WorkItemID), 시크릿 미기재 (infra-design §5) |
| SECURITY-04 HTTP 보안 헤더 | **적용(2026-07-11)** | **introspection UI 추가로 HTML 서빙 발생** → `/ui`·`/api/introspect/*`에 CSP(`default-src 'self'`)·HSTS·X-Content-Type-Options·X-Frame-Options=DENY·Referrer-Policy 미들웨어 필수 (code-gen Step 8e) |
| SECURITY-05 입력 검증 | **적용(UI 추가분)** | introspection 엔드포인트 쿼리 파라미터 검증(code-gen Step 8c); 그 외 기존 `api`/`ingest` 소관 |
| SECURITY-06 최소 권한 | **Compliant** | Mongo 컬렉션 단위 readWrite, Valkey 키 접두사 ACL (infra-design §2.1/§3.1, shared-infra §1/§2) |
| SECURITY-07 네트워크 제한 | **Compliant** | Mongo/Valkey 내부 네트워크 전용, K8s NetworkPolicy로 chiller만 접근 (deploy-arch §2) |
| SECURITY-08 앱 접근제어 | **적용(UI 추가분)** | introspection UI·엔드포인트에 authz fail-closed 적용(code-gen Step 8c); Bearer authz 자체 무변경, Authorizer→OIDC는 infra-plan #4(별도) |
| SECURITY-09 하드닝/기본자격 금지 | **Compliant** | Mongo SCRAM·Valkey requirepass, 기본 계정 없음, digest 핀 이미지 (infra-design §2.1/§3.1/§4) |
| SECURITY-10 공급망 | **Compliant** | 신규 의존 버전 핀+`go.sum`, `govulncheck` CI, SBOM, 이미지 digest 핀 (infra-design §4, deploy-arch §4) |
| SECURITY-11 보안 설계 원칙 | **Compliant** | seam 격리 유지(PR-11), fail-closed 부팅 게이트, rate-limit는 기존 SR-4 유지 (infra-design §5/§6) |
| SECURITY-12 자격증명 관리 | **Compliant** | 접속 문자열·토큰 Secret/env 주입, 소스 하드코딩 금지 (shared-infra §4) |
| SECURITY-13 무결성 검증 | **Compliant(설계)** | 이미지/base digest 핀, CI 파이프라인 접근제어(변경관리 제안); BSON 역직렬화는 신뢰 백엔드 한정 | 
| SECURITY-14 알림/모니터링 | **부분(이월)** | 헬스·메트릭 노출 지점 설계(infra-design §5); 알람 룰·로그 보존은 Operations |
| SECURITY-15 예외/fail-safe | **Compliant** | 백엔드 불가 시 스캔 시작 거부(fail-closed), ctx 타임아웃·연결 정리, reaper 실패 시 재개 백업 경로 (infra-design §3.2/§6) |

---

## Resiliency Compliance (RESILIENCY-01 … 15)

| 규칙 | 판정 | 근거 |
|---|---|---|
| RESILIENCY-01 워크로드 분류 | **Compliant** | manager(Critical, 컨트롤 싱글턴)/proxy(High, 금융 세션 싱글턴)/worker 풀(High, 상주 오토스케일, 재실행 가능)/Mongo(Critical)/Valkey-queue(High) 분류 + 의존 맵 (deploy-arch §1/§3) |
| RESILIENCY-02 SLA/RTO/RPO | **Compliant** | RPO≈0·RTO 수 분(Q12=B); manager 단일 인스턴스 트레이드오프 정당화 (deploy-arch §3/§5) |
| RESILIENCY-03 변경관리 | **Compliant(제안)** | 경량 변경 프로세스 제안(Q3r2=B) → Operations 이월 (deploy-arch §4) |
| RESILIENCY-04 배포/롤백 | **Compliant** | rolling 배포 + 버전 고정 롤백 사용자 확정(Q11=C); CI/CD 제안(Q4r2=B), AWS 비의존 (deploy-arch §4) |
| RESILIENCY-05 모니터링 | **부분(이월)** | 메트릭(큐 깊이·CAS 경합·백엔드 지연) 노출 지점 설계; 대시보드/OTel은 infra-plan #3 |
| RESILIENCY-06 헬스체크 | **Compliant** | deep `/healthz`(Mongo ping+Valkey PING) + shallow `/livez`, 부팅 게이트 정합 (infra-design §5) |
| RESILIENCY-07 회복탄력성 모니터링 | **부분(이월)** | 단일 AZ 운영·replication lag·백업 실패 알람은 Operations; 용량 지표(큐 깊이) 설계됨 |
| RESILIENCY-08 멀티AZ/리전 | **Compliant** | Mongo 3 AZ·Valkey 멀티 AZ(정적 안정성); 단일 리전 사용자 확정(Q11=C); manager 단일-active+재스케줄은 정당화된 편차 (deploy-arch §3/§5) |
| RESILIENCY-09 오토스케일 | **Compliant** | worker=상주 풀 + HPA/KEDA가 **큐 깊이(XLEN)/컨슈머 lag로 replica 자동 증감**(Q2=B); min/max replica 한계 설정, noeviction 큐로 소실 방지 (deploy-arch §1/§2) |
| RESILIENCY-10 타임아웃/서킷/벌크헤드 | **Compliant** | 모든 백엔드 호출 ctx 데드라인, 연결 풀 상한(bulkhead), 백엔드 불가 시 degrade (infra-design §6) |
| RESILIENCY-11 DR 전략 | **Compliant** | Warm Standby=리전 내 멀티 AZ 상시(Q11=C 재해석); 페일오버=Mongo election+재개 (deploy-arch §5) |
| RESILIENCY-12 백업/복제 | **Compliant** | oplog+일 1회 스냅샷/`mongodump`, 7일+ 보관, 암호화, 분기 테스트 복원 (infra-design §2.5) |
| RESILIENCY-13 페일오버 절차 | **부분(이월)** | Mongo 자동 election + `ResumeScan` 재개 설계; 런북·통신계획은 Operations |
| RESILIENCY-14 카오스/DR 테스트 | **N/A(이월)** | 시나리오(AZ 소실·Mongo primary 킬·worker replica 킬·proxy 킬) 문서화 → Operations 실행(Q 기본 defer) |
| RESILIENCY-15 인시던트 대응 | **Compliant(제안)** | 경량 IR+COE 제안(Q8r2=B) → Operations 이월 |

---

## PBT Compliance (PBT-01 … 10, FULL 모드)

| 규칙 | 판정 | 근거 |
|---|---|---|
| PBT-01 속성 식별 | **Compliant** | 이 설계가 식별한 속성: (a) 직렬화 왕복 `Unmarshal(Marshal(x))=x` — round-trip; (b) CAS 단일 승자 — invariant; (c) `Drain` 멱등 — idempotence; (d) mem을 오라클로 한 백엔드 등가 — oracle; (e) 명령 시퀀스 상태 일치 — stateful. Code Gen 계획으로 이월 |
| PBT-02 왕복 | **Compliant(설계)** | Q4=A BSON marshaling(enum 문자열)의 왕복을 모든 영속 타입에 property test (infra-design §2.3) |
| PBT-03 불변식 | **적용(이월)** | CAS 단일 승자·majority write 후 read-back을 invariant PBT로 (구현 시) |
| PBT-04 멱등 | **적용(이월)** | `Drain` 반복·`XACK` 재적용 멱등, at-least-once 재배달 안전을 PBT로 |
| PBT-05 오라클 | **Compliant(설계)** | `mem` 구현이 Mongo/Valkey 구현의 오라클 — 계약 스위트가 양쪽 실행(Q10=A), 동일 명령열 결과 비교 |
| PBT-06 상태 기반 | **Compliant(설계)** | TaskStore/WorkQueue 모두 상태 보유 → 명령 시퀀스(enqueue/dequeue/claim/record/drain) PBT, mem 모델과 비교 |
| PBT-07 생성기 품질 | **적용(이월)** | WorkItem/Scan 도메인 생성기(제약 존중) 재사용 |
| PBT-08 shrink/재현 | **적용(이월)** | `rapid` seed 로깅, CI 통합 |
| PBT-09 프레임워크 | **Compliant** | `pgregory.net/rapid` 이미 채택(기존 P1-P7 테스트) — 유지 |
| PBT-10 상보 전략 | **적용(이월)** | 계약 스위트(예제) + PBT 병행; 중요 경로(CAS 단일 승자) 예제 테스트 유지 |

---

## 종합
- **Blocking finding: 0.** 적용 규칙 모두 설계 근거 확보; 코드 수준 항목은 Code Generation으로 명시 이월; 순수 운영 항목은 Operations로 이월(근거 표기).
- **정당화된 편차 1건**: manager 단일 인스턴스(Q3=A, 사용자 명시 선택) → 컨트롤 플레인 무중단 포기, 상태 계층 멀티 AZ + 빠른 재스케줄로 상쇄. RESILIENCY-02/08에서 문서화됨(deploy-arch §3/§5).
