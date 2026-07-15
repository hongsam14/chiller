# Infrastructure Design Plan — 통합 취약 fixture (`test/fixtures/vuln-target/`, r9)

**Unit**: scan (probe) · **Increment**: r9 · **Requirements**: `requirements-detection-fidelity.md` (approved)

이 fixture는 **스캔당하는 테스트 대상**(C3=A)입니다 — chiller 제품 인프라가 아니라 test/ 아래 검증 스캐폴딩입니다. "인프라 설계"는 여기서는 **docker-compose fixture의 토폴로지·표면 매핑·격리·통합 연결**을 뜻합니다.

## 설계 대상 (표면 → probe 매핑)
| 표면 | 제공 | probe/항목 | 기대 finding |
|---|---|---|---|
| 보안 헤더 누락(HSTS/X-Frame/CSP 부재) | nginx 기본 | humble PS-001/024 | CWE-319, CWE-1021 |
| 버전 배너(`Server: nginx/x.y.z`) | nginx `server_tokens on` | httpx PS-007/020 | CWE-693 |
| permissive CORS(`ACAO: *`) | nginx `add_header` | Corsy PS-021 | CWE-346 |
| autoindex 디렉토리 | nginx `autoindex on` | dirhunt PS-004 | CWE-548 |
| subdomain-takeover 지문 body | mock 서버 | subzy PS-025 | NoCanonicalCWE |
| (testssl weak-TLS) | **fixture 밖** — 외부 badssl (Q1=A/C4=B) | testssl PS-002 | CWE-1327 |
| observation dormant 표면(robots/.env/.git/crossdomain/backup/session-in-url/comments/insecure-cookie) | nginx 정적 | PS-008/010/011/012/022/018/009/쿠키 | (현재 미발생 — well-known 미배선) |

## Plan Steps
- [x] IS-1: 아래 IQ1-4 답변 수집 — IQ1=A · IQ2=A · IQ3=A · IQ4=A
- [x] IS-2: fixture 토폴로지 확정(서비스·네트워크·볼륨) — 2 서비스(vulnweb nginx + takeover-mock), 내부 네트워크만
- [x] IS-3: 표면별 nginx conf/정적 콘텐츠/ mock 스펙 명세 — vuln-fixture-design.md §3
- [x] IS-4: 통합 테스트 연결 방식 확정 — 하이브리드(httptest 유지 + dirhunt/subzy env-gated compose)
- [x] IS-5: security-baseline 격리 규율 명세 — 포트 미노출·더미 비밀·제품 이미지 미포함
- [x] IS-6: 산출물 생성 — `construction/scan/infrastructure-design/vuln-fixture-design.md`

---

## Infrastructure Questions

각 `[Answer]:` 태그 뒤에 알파벳으로 답해 주세요.

### IQ1 — fixture 컨테이너 구성
`test/fixtures/vuln-target/` 를 몇 개 서비스로 구성할까요?

A) **nginx 1개 + mock 1개 = 2 서비스 (권장)** — `vulnweb`(nginx: humble/httpx/Corsy/dirhunt + observation dormant 표면 전부) + `takeover-mock`(subzy용 지문 body 반환). 단순·표면 대부분을 nginx conf 하나로.

B) **단일 nginx 컨테이너** — server_name/location 분리로 subzy 지문까지 한 컨테이너에. 서비스 1개(더 단순하나 subzy DNS 처리 제약 가능).

C) **표면별 개별 컨테이너** — 각 probe 표면을 독립 컨테이너로(세분·격리 강하나 무거움).

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A

---

### IQ2 — 통합 테스트에서 fixture 연결 방식
실 골든 캡처(개발 시)와 별개로, **자동화된 통합 positive 단언**을 어떻게 연결할까요?

A) **하이브리드 (권장)** — humble/httpx/Corsy는 **기존 in-process httptest positive 유지**(CI 기본 green, compose 불요). dirhunt/subzy는 **compose fixture 기동 시에만 env-gated**(예: `PROBE_FIXTURE_NET`)로 positive 단언. golden 캡처는 fixture로 확정. → CI 기본은 가볍게, 무거운 compose는 opt-in.

B) **compose 전면** — CI `probe-integration` 잡에서 compose 기동해 6종(testssl 제외 5종 + badssl)을 fixture/badssl 대상으로 positive 단언. 완전 재현이나 CI 무거움·badssl 네트워크 의존.

C) **testcontainers-go** — 테스트 내부에서 fixture 컨테이너 기동(platform infra increment에서 이미 testcontainers 사용 중).

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A

---

### IQ3 — fixture 네트워크 노출 (security-baseline)
의도적 취약 대상의 노출 경계는?

A) **호스트 미노출 (권장)** — compose 내부 네트워크만; 호스트 포트 publish 안 함(또는 `127.0.0.1` 바인드만). 취약 대상이 외부/LAN에 노출되지 않음. chiller/테스트 하네스는 같은 compose 네트워크에서 서비스명으로 접근.

B) **로컬 포트 노출** — 개발 편의로 특정 포트를 호스트에 노출(예: 8080). 노출 리스크 있음.

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A

---

### IQ4 — subzy 지문 전략 (CNAME 매칭 리스크)
subzy는 서브도메인의 CNAME/응답 body를 지문 DB와 매칭합니다. 일부 지문은 CNAME 매칭까지 요구해 DNS 조작이 필요합니다. mock 전략은?

A) **body-only 매칭 지문 사용 (권장)** — subzy 지문 DB에서 CNAME 불요·body만으로 vulnerable 판정되는 서비스 지문을 골라 mock가 그 body를 반환. DNS 조작 불필요. **실제 어떤 지문이 body-only인지는 Code Gen에서 subzy v1.1.0 DB 확인 후 확정**(불확실 시 정직하게 골든만 + positive 이월).

B) **CNAME 매칭 지문(GitHub Pages 등)** — compose DNS/hosts로 CNAME 흉내까지 구성(더 복잡, 재현성 낮을 수 있음).

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A
