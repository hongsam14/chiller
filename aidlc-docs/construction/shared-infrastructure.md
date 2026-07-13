# Shared Infrastructure — chiller

**Created**: 2026-07-11 (platform Infrastructure Design). **Decision**: Q9=C (Mongo 공유·컬렉션 분리 / Redis 분리·큐 vs 캐시).
**Updated**: 2026-07-13 (persistence Infrastructure Design) — 선반영해 두었던 **#5 ResponseCache→Valkey · #6 FindingsStore→Mongo가 실제로 확정**되었다. 아래 표의 "다음" 표기를 "확정"으로 승격하고 실측을 반영한다. 배치 전략 자체는 **변경 없음**(선반영이 그대로 맞았다).

이 문서는 여러 seam이 같은 종류의 백엔드를 쓸 때의 **인스턴스 배치 전략**을 한 곳에 고정한다. 현재 스코프(platform: TaskStore→Mongo, WorkQueue→Valkey / persistence: FindingsStore→Mongo, ResponseCache→Valkey)에서 확정한다.

---

## 1. MongoDB — 인스턴스 공유, 논리 분리 (Q9=C)

**하나의 Mongo replica set**(3 노드/3 AZ, Q6=B)을 여러 컨텍스트가 **컬렉션 단위로 분리**해 공유한다.

| 소비자 (seam) | DB / 컬렉션 | 스코프 |
|---|---|---|
| `platform.TaskStore` (platform 증분, 확정) | `chiller` / `scans`, `workledger`, `reports` | 오케스트레이션 상태·원장·보고서 **골격**(coverage/labels/revision) |
| `findings.FindingsStore` (**persistence 증분, 확정 2026-07-13**) | `chiller` / `findings` | findings **정본** — finding 당 1 문서, evidence는 AES-256-GCM 봉투 암호화, `MaxEvidenceSnippets` 경계 |

**경계 규칙 (persistence 증분에서 확정)**: `reports.report.findings`는 **비워서** 저장한다 — findings의 정본은 `findings` 컬렉션 하나뿐이다(이중 저장 금지). report 조립 시 두 컬렉션을 조인하며, 그 조인 지점은 **정확히 한 곳**(report 조립 함수)으로 국한한다. 이것이 16MB BSON 문서 한계를 해소하고 컬렉션 단위 최소 권한(SECURITY-06)을 가능하게 한다.
`platform.Scan`의 `report` 필드는 **아무도 설정하지 않던 죽은 필드**여서 제거했다.

- **격리**: 컬렉션 단위 최소 권한(SECURITY-06) — 서비스 계정은 자기 컬렉션만 readWrite.
- **트레이드오프 문서화**(RESILIENCY-10): findings 쓰기 부하가 오케스트레이션 지연에 영향 가능 → 심해지면 §3 승격 경로.
- **왜 공유**: 운영 단순(백업·복제·암호화 1벌), replica set 리소스 절약. 데이터 임계도가 유사(둘 다 majority write-concern 대상).

---

## 2. Valkey — 인스턴스 분리 (Q9=C)

큐와 캐시는 **정책이 정반대**라 **별도 인스턴스**로 둔다.

| 소비자 (seam) | 인스턴스 | 지속성 정책 | eviction |
|---|---|---|---|
| `platform.WorkQueue` (platform 증분, 확정) | **Valkey-queue** | AOF `everysec`+ (내구) | eviction 금지 (`noeviction`) — 작업 유실 방지 |
| `fetch.ResponseCache` (**persistence 증분, 확정 2026-07-13**) | **Valkey-cache** | 휘발성 (AOF 없음) | `allkeys-lru` + `maxmemory` — 메모리 압박 시 evict OK |

**persistence 증분에서 추가 확정**: 캐시 값은 **전체를 AES-256-GCM으로 암호화**해 저장한다(Valkey는 불투명 바이트만 본다). 복호화는 `ResponseCache` 구현 내부에서 끝나므로 체커는 평문 `*domain.HAREntry`를 그대로 받고 regex 탐색 코드는 무변경이다. GCM AAD에 `(scanId, url)`을 묶어 캐시 오염(다른 URL의 응답으로 위장 주입)을 차단한다.
캐시는 **유실 허용**이므로 복제 없이 단일 노드로 운영 가능하며, 장애 시 **fail-open**(miss 처리 후 재fetch — rate limiter 상한은 유지)으로 스캔은 완주한다.

- **왜 분리**: 한 인스턴스에 두면 캐시의 LRU eviction이 큐 항목을 evict하거나, 큐의 `noeviction`이 캐시 쓰기를 OOM으로 막는다. 메모리·지속성·eviction이 충돌.
- **키 네임스페이스**(SECURITY-06): 큐 `chiller:queue:{scanId}` / 캐시 `chiller:cache:{scanId}:{urlHash}`, ACL로 접두사 제한.
- **캐시 wipe**(FR-12/NFR-16): 스캔 종료 시 삭제 — 큐 인스턴스와 무관하게 독립 수명.
  **주의(persistence 증분 실사)**: 이 wipe는 **지금까지 호출되는 곳이 없었다**(`WipeScan` 미배선 → FR-12/NFR-16 미충족). persistence 증분에서 orchestrator의 세 종료 경로(completed/stopped/failed) 전부에 배선하고, 프로세스가 죽어도 사라지도록 **TTL 백스톱**(기본 6h)을 함께 건다. 삭제는 `SCAN` 패턴 순회가 아니라 스캔별 인덱스 SET(`chiller:cache:{scanId}:keys`)을 통한 정확 삭제로 한다.

---

## 3. 승격 경로 (공유 → 전용)

부하·격리 요구가 커지면 seam 인터페이스 **무변경**으로 인스턴스만 분리(접속 문자열 교체). 지표 트리거:
- Mongo: findings 쓰기가 오케스트레이션 CAS p95를 악화 → `findings`를 전용 replica set으로.
- 관측(RESILIENCY-05)에서 백엔드별 지연·경합을 분리 측정해 승격 판단.

---

## 4. 시크릿 / 접속 (SECURITY-09/12)
- 접속 문자열(Mongo URI, Valkey-queue/cache 주소·비밀번호), 인증 토큰, TLS CA, **암호화 키**는 **K8s Secret / env 주입**. 소스·이미지 하드코딩 금지.
- 필요 마운트: `CHILLER_MONGO_URI`, `CHILLER_VALKEY_ADDR`+`CHILLER_VALKEY_PASSWORD`, `CHILLER_VALKEY_CACHE_ADDR`+`CHILLER_VALKEY_CACHE_PASSWORD`, **`CHILLER_EVIDENCE_KEY`**(base64 32B — evidence + 캐시 값 암호화, 봉투의 `keyId`로 회전), `CHILLER_TOKEN`, `TLS_CA`. (config/secrets 상세는 infra-plan §5.)
- **fail-closed**: 백엔드를 켰는데(`--findings-store=mongo` / `--cache=valkey`) 접속 문자열이나 `CHILLER_EVIDENCE_KEY`가 없으면 **기동 실패**.
