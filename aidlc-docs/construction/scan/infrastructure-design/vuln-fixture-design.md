# Infrastructure Design — 통합 취약 fixture (`test/fixtures/vuln-target/`, r9)

**Unit**: scan (probe) · **Increment**: r9 · **Type**: Test infrastructure (스캔당하는 대상, C3=A — 제품 인프라 아님)
**Decisions**: IQ1=A(2 서비스) · IQ2=A(하이브리드 통합) · IQ3=A(호스트 미노출) · IQ4=A(body-only 지문)

## 1. 목적·경계
- **목적**: probe positive-finding 파서를 실 취약 출력으로 실측하기 위한 **재현 가능한 로컬 취약 대상**. 실 골든 캡처 소스이자 dirhunt/subzy 통합 positive 단언 대상.
- **경계 (security-baseline, blocking)**:
  - `test/fixtures/` 아래 **test 전용** — 제품 이미지/`Dockerfile`/`cmd/chiller`/배포 매니페스트에 **미포함**.
  - **호스트 포트 미노출**(IQ3=A) — compose 내부 네트워크만; 취약 대상이 외부/LAN에 노출되지 않음.
  - `.env`/secrets 표면은 **더미 값**(실 비밀 없음).
  - testssl weak-TLS는 fixture 밖 — 외부 **badssl.com** 공개 테스트 서비스(Q1=A/C4=B).

## 2. 토폴로지 (IQ1=A: 2 서비스)

```
compose network: vulnnet (internal, 호스트 미노출)
 ├─ vulnweb        (nginx:1.27-alpine, server_tokens on)
 │    ├─ /                     → 200, 보안헤더 전무(HSTS/XFO/CSP 없음), ACAO:* → humble·httpx·Corsy
 │    ├─ /listing/             → autoindex on, 파일 몇 개          → dirhunt
 │    └─ (dormant 정적)  /robots.txt /.env /.git/config
 │                       /crossdomain.xml /backup/db.sql.bak
 │                       /page-with-comments.html (민감 주석)
 │                       Set-Cookie: sid=... (Secure/HttpOnly/SameSite 없음)
 │                       /go?sid=<len32> (session-in-URL)
 └─ takeover-mock   (nginx:alpine or static) → subzy 지문 body 반환
      └─ 모든 경로 → 200/404 with body-only takeover fingerprint (IQ4=A)

접근: chiller 이미지 / go test 하네스가 vulnnet에 조인 → 서비스명(vulnweb, takeover-mock)으로 접근.
```

**텍스트 대안**: 내부 전용 compose 네트워크 `vulnnet`에 2개 서비스. `vulnweb`(nginx)가 루트에서 보안헤더 없는 200 + ACAO:* 응답(humble/httpx/Corsy 표면), `/listing/`에서 autoindex(dirhunt 표면), 그리고 dormant 관찰 표면(robots/.env/.git/crossdomain/backup/comments/insecure-cookie/session-in-url) 정적 제공. `takeover-mock`은 subzy용 body-only takeover 지문을 반환. 호스트 포트 미노출.

## 3. 표면별 명세

### 3.1 vulnweb (nginx) — HTTP probe 4종 + observation dormant
- **humble (PS-001/024 · CWE-319/1021)**: 루트 응답에 `Strict-Transport-Security` 없음(→HSTS finding), `X-Frame-Options`·`Content-Security-Policy` 둘 다 없음(→clickjacking finding). nginx 기본이 이미 이 상태 — 보안헤더를 **추가하지 않음**으로 달성.
- **httpx (PS-007/020 · CWE-693)**: `server_tokens on;` → `Server: nginx/1.27.x` 버전 배너. 파서 `hasVersion()`가 dotted 버전 검출.
- **Corsy (PS-021 · CWE-346)**: `add_header Access-Control-Allow-Origin *;` (그리고 preflight 반사). Corsy가 wildcard ACAO 오분류 검출.
- **dirhunt (PS-004 · CWE-548)**: `location /listing/ { autoindex on; }` + 더미 파일 2~3개. dirhunt 리포트 flags에 `directory-listing`/`index-of`.
- **observation dormant (finding 미발생, 문서화)**:
  - `/robots.txt` (민감 경로 disallow 예시), `/.env`(더미 `DB_PASSWORD=dummy` 등), `/.git/config`, `/crossdomain.xml`(`allow-access-from domain="*"`), `/backup/db.sql.bak`, `/page-with-comments.html`(`<!-- password: dummy -->`), `Set-Cookie: sid=abc; path=/`(Secure/HttpOnly/SameSite 없음), `/go?sid=<32-hex>`.
  - **현재 엔진은 이 well-known 경로를 요청하지 않으므로 finding이 나지 않음** — fixture에 표면만 심어두고(C5=B), well-known provider 배선 증분에서 재사용. 테스트/문서에 "dormant, no-finding by design (well-known GET 미배선)"으로 명시.

### 3.2 takeover-mock — subzy (PS-025 · NoCanonicalCWE)
- subzy `--target <host>` 가 해석·fetch 후 응답 body를 지문 DB와 매칭. **IQ4=A: body-only 매칭 지문** 사용 — CNAME 조작 불필요.
- mock는 subzy v1.1.0 DB의 body-only 지문 문자열을 반환(예: 특정 SaaS의 "not found" 배너). **구체 지문은 Code Gen에서 subzy 임베디드 fingerprints를 실측 확인해 확정**.
- **정직성 조건**: subzy가 mock로 `vulnerable=true`를 실제로 산출하지 못하면(지문 매칭 실패/네트워크), positive 통합은 **정직하게 이월**하고 골든은 실 subzy `--output` 스키마로만 고정(파서 검증까지).

## 4. 통합 테스트 연결 (IQ2=A: 하이브리드)
- **humble/httpx/Corsy**: 기존 `integration_test.go`의 in-process `httptest` positive 단언 **유지**(CI 기본 green, compose 불요). fixture는 이들의 골든 재캡처 소스로도 사용(외부 Juice Shop/kblife 의존 제거).
- **dirhunt/subzy**: compose fixture 기동 시에만 **env-gated**(예: `PROBE_FIXTURE=1` + 내부 서비스명) positive 단언 추가. 기본 CI에서는 skip(green 유지).
- **testssl**: `PROBE_TEST_HOST`/badssl 대상 시에만 실행(네트워크 실패 → tool-failed 정직, RES-D2).
- **golden 유닛 테스트**: 전 probe 실 골든 기반(hermetic) — compose 불요, 항상 실행.

## 5. Deployment/Run (fixture 기동)
- `test/fixtures/vuln-target/docker-compose.yml` — 2 서비스 + `vulnnet` 내부 네트워크, 포트 publish 없음.
- 개발/검증: `docker compose -f test/fixtures/vuln-target/docker-compose.yml up -d` → 같은 네트워크에서 chiller/probe 실행해 골든 캡처 → `docker compose down`.
- CI(opt-in): `probe-integration` 잡에 compose 기동 단계 추가 가능(IQ2=A 기본은 미기동).

## 6. Compliance (enabled extensions)
| 규칙 | 상태 | 근거 |
|---|---|---|
| security-baseline SEC-D1 (test 전용·격리) | ✅ | `test/` 아래, 내부 네트워크, 제품 이미지 미포함 |
| security-baseline SEC-D2 (무허가 제3자 스캔 금지) | ✅ | dirhunt/subzy 로컬 fixture만; testssl은 공개 badssl |
| security-baseline SEC-D3 (실 비밀 없음) | ✅ | `.env` 등 더미 값 |
| resiliency-baseline RES-D1 (타임아웃/kill 회귀 없음) | ✅ | probe argv 불변; fixture는 대상일 뿐 |
| resiliency-baseline RES-D2 (badssl 네트워크 실패 정직) | ✅ | 실패 → tool-failed, gate flaky 방지 |
| property-based-testing | N/A(이 스테이지) | 파서 PBT는 Code Gen |

## 7. Code Gen으로 넘길 실측 미확정 (F6 성격)
- subzy body-only 지문 문자열(v1.1.0 DB 확인) — 매칭 실패 시 positive 이월.
- testssl 실 `--jsonfile` 스키마(badssl 실행 대비) — 현 파서 가정(`id/severity/finding`) 검증·필요 시 교정.
- dirhunt 실 `--to-file` 스키마(autoindex 대비) — flags 필드 검증·필요 시 교정.
- nginx 버전 태그가 httpx `hasVersion` 통과하는지 확인(예: `nginx:1.27-alpine`).
