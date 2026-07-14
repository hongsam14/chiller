# Shared Infrastructure — chiller

**Created**: 2026-07-11 (platform Infrastructure Design). **Decision**: Q9=C (Mongo 공유·컬렉션 분리 / Redis 분리·큐 vs 캐시).

이 문서는 여러 seam이 같은 종류의 백엔드를 쓸 때의 **인스턴스 배치 전략**을 한 곳에 고정한다. 현재 스코프(platform: TaskStore→Mongo, WorkQueue→Valkey)에서 확정하되, infra-plan의 다음 항목(#5 ResponseCache→Valkey, #6 FindingsStore→Mongo)이 이 배치를 **미리 따르도록** 선반영한다.

---

## 1. MongoDB — 인스턴스 공유, 논리 분리 (Q9=C)

**하나의 Mongo replica set**(3 노드/3 AZ, Q6=B)을 여러 컨텍스트가 **컬렉션 단위로 분리**해 공유한다.

| 소비자 (seam) | DB / 컬렉션 | 스코프 |
|---|---|---|
| `platform.TaskStore` (이번 스코프) | `chiller` / `scans`, `workledger`, `reports` | 오케스트레이션 상태·원장·보고서 |
| `findings.FindingsStore` (infra-plan #6, 다음) | `chiller` / `findings` | 증거·발견 (저장 시 암호화, `MaxEvidenceSnippets` 경계) |

- **격리**: 컬렉션 단위 최소 권한(SECURITY-06) — 서비스 계정은 자기 컬렉션만 readWrite.
- **트레이드오프 문서화**(RESILIENCY-10): findings 쓰기 부하가 오케스트레이션 지연에 영향 가능 → 심해지면 §3 승격 경로.
- **왜 공유**: 운영 단순(백업·복제·암호화 1벌), replica set 리소스 절약. 데이터 임계도가 유사(둘 다 majority write-concern 대상).

---

## 2. Valkey — 인스턴스 분리 (Q9=C)

큐와 캐시는 **정책이 정반대**라 **별도 인스턴스**로 둔다.

| 소비자 (seam) | 인스턴스 | 지속성 정책 | eviction |
|---|---|---|---|
| `platform.WorkQueue` (이번 스코프) | **Valkey-queue** | AOF `everysec`+ (내구) | eviction 금지 (`noeviction`) — 작업 유실 방지 |
| `fetch.ResponseCache` (infra-plan #5, 다음) | **Valkey-cache** | RDB만/휘발성 | `allkeys-lru` — 메모리 압박 시 evict OK |

- **왜 분리**: 한 인스턴스에 두면 캐시의 LRU eviction이 큐 항목을 evict하거나, 큐의 `noeviction`이 캐시 쓰기를 OOM으로 막는다. 메모리·지속성·eviction이 충돌.
- **키 네임스페이스**(SECURITY-06): 큐 `chiller:queue:{scanId}` / 캐시 `chiller:cache:{scanId}:{urlHash}`, ACL로 접두사 제한.
- **캐시 wipe**(FR-12/NFR-16): 스캔 종료 시 `chiller:cache:{scanId}:*` 삭제 — 큐 인스턴스와 무관하게 독립 수명.

---

## 3. 승격 경로 (공유 → 전용)

부하·격리 요구가 커지면 seam 인터페이스 **무변경**으로 인스턴스만 분리(접속 문자열 교체). 지표 트리거:
- Mongo: findings 쓰기가 오케스트레이션 CAS p95를 악화 → `findings`를 전용 replica set으로.
- 관측(RESILIENCY-05)에서 백엔드별 지연·경합을 분리 측정해 승격 판단.

---

## 4. 시크릿 / 접속 (SECURITY-09/12)
- 접속 문자열(Mongo URI, Valkey-queue/cache 주소·비밀번호), 인증 토큰, TLS CA는 **K8s Secret / env 주입**. 소스·이미지 하드코딩 금지.
- 필요 마운트: `MONGO_URI`, `VALKEY_QUEUE_ADDR`+`VALKEY_QUEUE_PASSWORD`, `VALKEY_CACHE_ADDR`+`VALKEY_CACHE_PASSWORD`, `CHILLER_TOKEN`, `TLS_CA`. (config/secrets 상세는 infra-plan §5.)
