# Platform FD — 브라우저 인증 Transport / Proxy 역할 (Clarification)

**Stage**: 🟢 CONSTRUCTION → `platform` → Functional Design (추가 라운드)
**Trigger**: 사용자 요구 — 금융권 타깃은 **브라우저가 주입한 인증**을 통해서만 요청을 처리하므로 OSS 툴/네이티브 요청을 **Playwright 인증 세션**을 경유시켜야 함. 세션은 하나뿐이므로 동시성은 **단일 브라우저의 탭/컨텍스트-공유 요청**으로.
**Blocking**: platform Code Generation Part 2는 이 문서 확정 후 진행. 이 결정은 **scan 유닛(Fetcher/probe)** 도 건드림 → 아래 §영향.

---

## 배경 (문제 정의)

금융권 웹앱은 세션 인증(브라우저 로그인)을 통과하지 못한 raw HTTP를 로그인으로 리다이렉트/차단합니다. chiller의 native `Fetcher`와 OSS 프로브(httpx/dirhunt/Corsy/changeme 등)는 자체 HTTP 스택으로 요청하므로 그대로면 **미인증 → 커버리지 0**. 따라서 **인증된 브라우저 세션을 경유**해야 하고, 세션이 하나뿐이므로 독립 세션 다중화가 아니라 **단일 세션 공유 동시성**이 필요합니다.

---

## 질문과 답변

### Q1 — 이번 MVP 범위: 브라우저 인증 transport를 지금 구현?
| | 옵션 |
|---|---|
| A | seam만 지금 + Playwright 구현은 Infra/Post-MVP 이연 |
| **B** ✅ | **지금 구현** — 실제 금융권 타깃 검증이 요구사항. Playwright 런타임 의존 추가. |

[Answer]: **B**

> 결과: Playwright(Chromium) 기반 인증 transport를 이번 코드젠에 구현. **playwright-go 바인딩이 신규 외부 의존**으로 추가됨. Chromium 바이너리 provisioning + `playwright install` + MITM CA 발급은 **build/Infra 단계**(OSS CLI 번들과 동일 원칙) — Go 코드(proxy/세션/탭풀/주입)는 지금 작성, 브라우저 실체는 빌드 시.

### Q2 — 인증 세션 획득 방법
| | 옵션 |
|---|---|
| **A** ✅ | **사전 제공 `storageState`(녹화 세션) 주입** — 운영자가 로그인 후 상태 파일 제공 |
| B | 자격증명 + chiller 로그인 플로우 수행 |
| C | 수동 쿠키/헤더 주입 |

[Answer]: **A**

> 결과: 스캔 요청/설정에 `storageState`(Playwright 표준 세션 상태) 경로/블롭을 제공. chiller는 로그인 플로우를 몰라도 됨(대상별 로그인 스크립트 관리 회피). 세션 만료는 운영자 갱신 책임(문서화).

### Q3 — OSS 앱계층 툴의 세션 처리 → **바이너리에 `proxy` 역할 추가**
| | 옵션 |
|---|---|
| A | 세션 export(쿠키 주입)만, 회전토큰 한계 문서화 |
| **B** ✅ | **로컬 인증 프록시** — 바이너리에 **`proxy` 역할** 추가. **모든 worker가 단일 proxy를 통해 egress**, proxy가 브라우저 컨텍스트의 **최신 세션을 주입해 포워딩**. |

[Answer]: **B — 결국 바이너리에 proxy 모드가 추가됨. 모든 worker가 단일 proxy를 통해 나감.**

> 결과 (핵심 설계):
> - **역할이 4개로**: `[all, manager, worker, proxy]`.
> - **`proxy`** 프로세스가 **단 하나의 인증 브라우저 세션**(1 Browser → 1 BrowserContext, storageState 주입)을 보유. worker의 아웃바운드를 받아 **세션 주입 후 대상으로 포워딩**하고 응답 반환.
> - **worker는 무상태·수평확장** — N개 worker가 전부 **한 proxy**를 가리킴(`HTTP(S)_PROXY`) → **단일 세션 불변식 유지하면서 worker만 확장**. 세션이 proxy에만 살기 때문.
> - **툴 통일**: chiller native Fetcher는 `http.Client.Transport.Proxy`로, OSS CLI는 `HTTP_PROXY` env/`--proxy` 플래그로 **동일 proxy** 경유 → 회전 토큰도 proxy가 최신 세션 주입해 커버.
> - **툴 분류(예외)**: **testssl(전송계층 TLS 점검)은 proxy 우회, 대상 직접** — TLS 핸드셰이크 자체가 점검 대상이라 프록시 경유가 무의미. 앱계층 요청만 proxy 경유.

### Q4 — 탭/요청 풀 크기 T (proxy 동시성 상한)
| | 옵션 |
|---|---|
| **A** ✅ | **고정 기본(T=6) + `--browser-tabs` 파라미터** |
| B | 지금 미정, NFR에서 확정 |

[Answer]: **A**

> 결과: proxy는 단일 컨텍스트 위에서 **동시 요청 슬롯 T개**(기본 6, `--browser-tabs`로 조정). 순수 HTTP 페치는 `context.request`(APIRequestContext, 컨텍스트 쿠키 공유·동시 안전)로, JS 렌더 필요분만 Page(탭)로.

---

## 확정 설계 (Step 9: 충돌 없음, 채택)

### 역할 모델 (Q3=B)
```
manager  : ScanAPI + FSM + fan-out + 최종 조립
worker   : WorkItem 소비 + 체커 실행; 앱계층 아웃바운드는 proxy 경유(HTTP_PROXY)
proxy    : 단일 인증 브라우저 세션 보유; worker 트래픽에 세션 주입 후 포워딩; 탭풀 T
all      : 위 셋을 단일 프로세스에서(개발/standalone)
```

### 인증 Transport (Q1=B, Q2=A)
- **`BrowserSession`**(인터페이스; Playwright 구현) — 1 Browser → 1 BrowserContext(storageState 주입). 테스트에서 fake 가능(브라우저 없이).
- **`AuthProxy`** — worker의 HTTP(S) 요청을 받아 `BrowserSession`의 컨텍스트로 재요청(세션 쿠키/헤더 주입) → 응답 반환. HTTPS는 번들 CA로 MITM(worker가 CA 신뢰). 탭풀 T로 동시성 제한.
- **scan `Fetcher` 변경(경미)**: `NewFetcher`에 선택적 proxy URL → `Transport.Proxy` 설정. financial 모드면 proxy, 아니면 직접.
- **probe 어댑터 변경(경미)**: financial 모드면 `HTTP_PROXY` env 주입(testssl 제외).

### 동시성 모델 (Q4=A) — 시간 단축과 상한
- 비-financial 스캔: worker 풀 W + per-host token bucket. cross-host는 W까지, intra-host는 rate-bound(의도). 호스트-인터리브 fan-out으로 HOL 회피.
- financial 스캔: 위에 더해 **모든 앱계층 요청이 proxy의 탭풀 T를 통과** → 유효 병렬성 `min(W, T, per-host rate)`. **단일 세션 스캔은 T가 지배 상한**(브라우저 단일 프로세스 자원). per-host rate는 proxy에서도 강제.

### 영향받는 유닛
- **platform**: 신규 `proxy` 역할 + `internal/platform/proxy/`(AuthProxy + BrowserSession). 승인된 산출물(FD 3종) 갱신.
- **scan**: `fetch.NewFetcher` proxy 옵션, `probe` 어댑터 proxy env — **경미**, arch-check 불변(scan은 여전히 domain만 import; proxy 주소는 설정값).
- **의존성**: `github.com/playwright-community/playwright-go` 신규. Chromium 바이너리 + CA = build/Infra.

### 정직한 스코프 경계
- MVP: AuthProxy 서버 + BrowserSession(인터페이스+Playwright 구현) + 탭풀 + 세션 주입 + 라우팅 + 테스트(BrowserSession fake로 CI 브라우저 불요). 
- Infra/build: Chromium provisioning, MITM CA 발급/배포, HTTPS 인터셉션 하드닝(스트리밍/WebSocket 엣지), 세션 자동 갱신.
