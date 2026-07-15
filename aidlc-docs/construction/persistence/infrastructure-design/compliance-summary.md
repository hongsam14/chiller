# Extension Compliance Summary — persistence 증분 Infrastructure Design

**Date**: 2026-07-13
**Enabled extensions** (all blocking, per `aidlc-state.md` §Extension Configuration):
`security-baseline` · `resiliency-baseline` · `property-based-testing` (FULL)

**Blocking findings: 0.**

각 규칙은 **이 스테이지(인프라 설계)의 산출물과 결정에 대해서만** 평가한다.
이 증분이 건드리지 않는 표면(큐·오케스트레이션·ScanAPI 인증 등)에 대한 규칙은 직전 증분에서 이미 판정되었고,
여기서는 **N/A (이 증분의 범위 밖 — 결정 변경 없음)** 로 표기한다.

---

## security-baseline

| 규칙 | 판정 | 근거 |
|---|---|---|
| SECURITY-01 Encryption at Rest and in Transit | **Compliant** | **at-rest**: evidence는 AES-256-GCM 봉투 암호화(설계 §3.6), 캐시 값은 전체 암호화(§4.1). **in-transit**: Mongo URI에 TLS 강제(기존), Valkey-cache 접속도 TLS+비밀번호. **이 증분이 해소하는 기존 결함**: evidence가 `reports` 컬렉션에 평문 저장되고 있었다(§0 F-1′) |
| SECURITY-02 Access Logging on Network Intermediaries | **N/A** | 이 증분은 네트워크 중개자(프록시·LB)를 추가하지 않는다. auth-proxy 로깅은 직전 증분 판정 유지 |
| SECURITY-03 Application-Level Logging | **Compliant** | 신규 로그 지점(캐시 에러, findings 저장 재시도/실패, wipe 실패)은 **scanId·URL·에러 종류만** 남기고 **응답 본문·evidence snippet·복호화 키를 절대 로그에 싣지 않는다**. 기존 R9(redaction) 규율 유지 |
| SECURITY-04 HTTP Security Headers | **N/A→Compliant(불변)** | 신규 엔드포인트 `/api/introspect/persistence`는 **기존 ScanAPI 미들웨어를 그대로 통과**하므로 보안 헤더·인증이 자동 적용된다(직전 증분에서 확립). 새 정책 결정 없음 |
| SECURITY-05 Input Validation on API Parameters | **Compliant** | 신규 엔드포인트는 **파라미터가 없다**(카운터 조회). 신규 플래그(`--cache-ttl`, `--findings-ttl`, 주소, 키)는 기동 시 검증 — 키 길이(32B)·duration 파싱·주소 형식 실패 시 **기동 거부** |
| SECURITY-06 Least-Privilege Access Policies | **Compliant** | Mongo: 서비스 계정은 `chiller` DB의 **자기 컬렉션만** readWrite (`findings` 분리가 이걸 처음으로 가능하게 함 — Q1=B였다면 불가능). Valkey: 키 접두사 ACL(`chiller:cache:*`)로 큐 키스페이스 접근 차단 (배포 §3) |
| SECURITY-07 Restrictive Network Configuration | **Compliant** | Valkey-cache는 **클러스터 내부 전용**(외부 노출 없음), NetworkPolicy로 chiller Pod에서만 접근. Mongo는 기존 정책 유지 |
| SECURITY-08 Application-Level Access Control | **Compliant** | 신규 introspect 엔드포인트는 기존 Bearer authz(fail-closed)를 그대로 상속 |
| SECURITY-09 Security Hardening / Misconfiguration Prevention | **Compliant** | **fail-closed 기본값**: 백엔드를 선택했는데 접속 문자열 또는 `CHILLER_EVIDENCE_KEY`가 없으면 **기동 실패**(설계 §7). 새 백엔드는 **기본 off**(`mem`) — 실수로 켜지지 않는다 |
| SECURITY-10 Software Supply Chain Security | **Compliant** | **신규 의존성 0건** (실사 F-10). 암호화는 Go 표준 `crypto/aes`+`crypto/cipher`, Mongo/Valkey 드라이버는 이미 핀 고정된 기존 의존성. 공급망 표면 **불변** → `make vuln`/`make sbom` 기존 절차 그대로 |
| SECURITY-11 Secure Design Principles | **Compliant** | ① **최소 권한**(컬렉션·키 접두사 분리) ② **심층 방어**(명시적 wipe + TTL 백스톱, §4.3) ③ **안전한 기본값**(백엔드 off, fail-closed) ④ **봉투 이식 차단**(GCM AAD에 scanId+key/url 바인딩 — 암호문을 다른 finding/URL로 옮겨 붙이는 공격 차단, §3.6/§4.1) |
| SECURITY-12 Authentication and Credential Management | **Compliant** | `CHILLER_EVIDENCE_KEY`·`CHILLER_VALKEY_CACHE_PASSWORD`는 **K8s Secret/볼트 주입만**. 소스·이미지·매니페스트 하드코딩 금지, `secret.example.yaml`은 값 없는 템플릿. **키 회전 경로 설계됨**(봉투의 `keyId` 다중 키) |
| SECURITY-13 Software and Data Integrity Verification | **Compliant** | GCM은 **AEAD** — 복호화가 곧 무결성 검증이다. 변조된 evidence/캐시 값은 **복호화 실패로 검출**되며 조용히 통과하지 못한다. 이미지 다이제스트 고정은 기존 production-readiness 항목 그대로 |
| SECURITY-14 Alerting and Monitoring | **Compliant** | 신규 카운터(`cache_errors_total`, `findings_save_failures_total`, `cache_wipe_errors_total`)를 `/api/introspect/persistence` + `/healthz`(deep, Mongo·Valkey-cache Ping)로 노출(§5). **fail-open이 조용해지지 않도록** 하는 것이 이 규칙의 핵심 요구였고, `Get`의 3-값 반환(§2.2)이 그 전제 |
| SECURITY-15 Exception Handling and Fail-Safe Defaults | **Compliant** | 기동 시 백엔드 `Ping` 실패 → **기동 거부**(fail-closed, 기존 패턴). 런타임: **정확성 계층(findings 저장)은 fail-closed**(재시도 소진 시 scan=failed), **최적화 계층(캐시)은 fail-open**(§4.4) — 계층별로 의도적으로 다르게 판정했고 그 근거를 문서화함 |

---

## resiliency-baseline

| 규칙 | 판정 | 근거 |
|---|---|---|
| RESILIENCY-01 Critical Workload Identification | **Compliant** | 이 증분의 두 백엔드를 **중요도로 명시 구분**: `findings`(Mongo) = **정확성 계층**(결과 유실 불가) / `ResponseCache`(Valkey) = **최적화 계층**(유실 허용). 이 구분이 §4.4/§5의 모든 결정을 규정 |
| RESILIENCY-02 Availability and Recovery Targets | **N/A (불변)** | RPO≈0 · RTO 수 분(Q12=B)은 직전 증분 확정. findings는 majority write concern으로 **동일 RPO 유지**. 캐시는 유실 허용이라 RPO 대상 아님 |
| RESILIENCY-03 Change Management Process | **Compliant** | 스키마 파괴적 변경(`Severity` int→string, `FrameworkLabels` []int→[]FindingKey)을 **명시 식별**하고 배포 노트에 기록(배포 §7). 프로덕션 데이터 없음 → 마이그레이션 불필요, 개발/스테이징 DB는 배포 전 drop |
| RESILIENCY-04 Automated Deployment and Rollback | **Compliant** | 새 백엔드가 **기본 off**라 **이미지 배포와 백엔드 활성화가 분리**된다 → 롤백이 곧 **플래그 되돌리기**(재배포 없이도 가능). `findings` 컬렉션은 롤백 시 보존(파괴적 롤백 없음). 활성화 순서 명시(배포 §7) |
| RESILIENCY-05 Monitoring and Alerting | **Compliant** | §5.1 카운터 + `/healthz` deep check. 경보 대상: 캐시 에러율, findings 저장 실패, wipe 실패 |
| RESILIENCY-06 Health Checks | **Compliant** | `/healthz`(deep)에 Mongo(findings)·Valkey-cache `Ping` 추가(§5.2) |
| RESILIENCY-07 Resiliency Monitoring | **Compliant** | 장애 격리 매트릭스를 명시(배포 §5): 어느 백엔드가 죽으면 무엇이 어떻게 되는지 4행 전부 기술 |
| RESILIENCY-08 Multi-Zone / Multi-Region | **N/A (불변)** | 단일 리전·멀티 AZ(Q11=C, 직전 증분). Mongo는 기존 3-AZ replica set 재사용. Valkey-cache는 **유실 허용**이라 복제 불요(단일 노드 허용) — 이 판단을 배포 §2에 근거와 함께 기록 |
| RESILIENCY-09 Auto-Scaling and Capacity | **N/A (불변)** | 워커 오토스케일 정책은 직전 증분(큐 길이 기반). 캐시 추가는 워커 스케일 정책을 바꾸지 않는다. Valkey-cache는 `maxmemory` 상한으로 용량 고정 |
| RESILIENCY-10 Dependency Isolation and Circuit Breaking | **Compliant** | **공유 Mongo 트레이드오프를 명시 문서화**(배포 §6, 이 규칙의 직접 요구): findings 쓰기가 오케스트레이션 CAS와 replica set을 공유 → 위험·완화(시간축 분리)·**승격 경로**(인터페이스 무변경, 접속 문자열 교체)를 기록. 캐시는 fail-open으로 **의존성 격리**(장애가 스캔으로 전파되지 않음) |
| RESILIENCY-11 DR Strategy Selection | **N/A (불변)** | Warm Standby → 리전 내 멀티 AZ(직전 증분 확정) |
| RESILIENCY-12 Data Backup and Replication | **Compliant** | `findings`는 기존 Mongo replica set에 속하므로 **기존 백업 정책(oplog + 일일 스냅샷, 암호화)에 자동 포함**. 캐시는 **백업 대상 아님**(휘발성·재생성 가능 — 명시) |
| RESILIENCY-13 Failover and Recovery Procedures | **Compliant** | Mongo failover는 replica set이 처리(기존). 캐시 노드 손실 = **전체 miss + 재fetch**로 자동 복구(§4.4) — 별도 복구 절차 불필요 |
| RESILIENCY-14 Chaos Engineering and DR Testing | **Compliant (설계 수준)** | 통합 테스트에서 **캐시 백엔드 다운 시나리오**(컨테이너 정지 → 스캔이 fail-open으로 완주)를 검증 대상으로 지정(§6). 정기 카오스 훈련은 Operations 항목 |
| RESILIENCY-15 Incident Response and COE | **N/A (불변)** | IR/COE 프로세스는 직전 증분·Operations 소관. 신규 경보 항목만 §5에서 추가 |

---

## property-based-testing (FULL — 10개 규칙 전부 enforced)

| 규칙 | 판정 | 근거 |
|---|---|---|
| PBT-01 Property Identification During Design | **Compliant** | **이 설계 단계에서** 속성을 식별해 문서화(설계 §6): 라운드트립·순서보존·경계보존·암복호 항등·AAD 거부·캐시 상태모델·키 유일성 |
| PBT-02 Round-Trip Properties | **Compliant** | `SaveFindings → LoadFindings` 항등(**순서까지**). `Enc → Dec` 항등. `HAREntry` 직렬화 라운드트립 |
| PBT-03 Invariant Properties | **Compliant** | ① CWE 정규화(R1)가 라운드트립 후에도 유지 ② `MaxEvidenceSnippets` 경계가 저장·로드 후에도 절대 초과되지 않음 ③ **`WipeScan` 후 해당 scanId 키 0개** ④ dedup 후 `Finding.Key()` **유일성**(§3.5의 전제 — 이게 깨지면 FrameworkLabels가 손상되므로 반드시 검증) |
| PBT-04 Idempotency Properties | **Compliant** | `SaveFindings`를 같은 입력으로 N회 호출 → 결과 동일(upsert 멱등 + 꼬리 삭제, §3.2). `WipeScan` N회 호출 안전 |
| PBT-05 Oracle and Model-Based Testing | **Compliant** | mem 구현을 **oracle**로 삼아 Mongo/Valkey 구현을 대조(직전 증분에서 확립한 패턴). 계약 스위트가 두 구현에 동일 시퀀스를 적용해 결과 일치를 단언 |
| PBT-06 Stateful Property Testing | **Compliant** | 캐시 상태 모델: 무작위 `put/get/wipe/expire` 시퀀스 → 모델 맵과 실제 구현의 관측이 일치. wipe 이후 항상 miss |
| PBT-07 Generator Quality | **Compliant** | 생성기: CWE 리스트(중복·센티넬 혼합·빈 값 포함), URL(유니코드·쿼리·긴 경로), snippet(경계 초과 케이스 포함 — 8개 상한을 넘기는 입력을 **의도적으로** 생성), 본문(빈/이진/대용량 경계) |
| PBT-08 Shrinking and Reproducibility | **Compliant** | 기존 PBT 하네스(직전 증분의 `math/rand` 시드 기반)를 재사용 — **실패 시 시드 출력**으로 재현 가능. 최소 반례 축소는 기존 헬퍼 사용 |
| PBT-09 Framework Selection | **Compliant** | 신규 프레임워크 도입 없음 — 프로젝트에 이미 확립된 자체 PBT 하네스(`math/rand` 오라클 방식, PBT-02/05/06을 이미 이 방식으로 통과) 재사용. **신규 의존성 0** 원칙(SECURITY-10)과도 정합 |
| PBT-10 Complementary Testing Strategy | **Compliant** | PBT는 **단독이 아니다**: 계약 스위트(예제 기반) + 통합 테스트(testcontainers, 실 Mongo/Valkey) + **F-4 회귀 테스트**(completed/stopped/failed 세 종료 경로 각각에서 캐시 잔존 0 단언)와 함께 구성(§6) |

---

## 판정 요약

| | Compliant | N/A | Non-compliant |
|---|---|---|---|
| security-baseline (15) | 13 | 2 | **0** |
| resiliency-baseline (15) | 9 | 6 | **0** |
| property-based-testing (10) | 10 | 0 | **0** |

**Blocking findings: 0.** N/A 판정은 모두 "이 증분이 해당 표면을 변경하지 않으며, 직전 증분/Operations에서 이미 판정됨"이라는
동일한 근거를 가진다 (규칙 자체가 프로젝트에 적용되지 않는다는 뜻이 아니다).
