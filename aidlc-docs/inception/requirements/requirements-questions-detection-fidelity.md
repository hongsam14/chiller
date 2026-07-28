# Requirements Questions — #0d 나머지 (testssl/dirhunt/subzy positive-finding 실측, r9)

이전 증분(r8)에서 humble/httpx/Corsy는 실 취약 대상(Juice Shop + kblife-clone.com)으로 positive-finding 스키마를 검증·고정했습니다. 남은 3개 probe는 벤치 대상(로컬 http)에 해당 취약 표면이 없어 **positive-finding 파서 스키마가 미검증**입니다:

- **testssl.sh** — PS-002 · CWE-1327 weak TLS → **약한 TLS(구 프로토콜/약한 cipher) HTTPS 서버** 필요
- **dirhunt** — PS-004 · CWE-548 directory listing → **디렉토리 리스팅이 노출된 서버** 필요
- **subzy** — PS-025 · subdomain takeover(NoCanonicalCWE) → **dangling CNAME 서브도메인**(미청구 서비스로 연결) 필요

이 증분의 목표는 각 파서가 **실제 취약 출력에서 올바른 CWE finding을 추출**하도록 스키마를 실측·고정하고, 정직성 불변식(도구 미실행/미배선 → tool-failed)과 26 파티션을 유지하는 것입니다. 각 도구가 다른 대상 표면을 요구하므로, 대상 확보 방식을 먼저 확정해야 합니다.

각 질문의 `[Answer]:` 태그 뒤에 알파벳으로 답해 주세요. 맞는 선택지가 없으면 마지막 `X) 기타`를 고르고 설명을 적어 주세요.

---

## Question 1
**testssl.sh (PS-002 weak TLS)** 의 취약 대상을 어떻게 확보할까요?

A) **badssl.com 공개 취약 서브도메인 실측** — Google이 운영하는 공개 TLS 테스트 서비스(예: `rc4.badssl.com`, `tls-v1-0.badssl.com`, `dh1024.badssl.com`)를 대상으로 실 testssl 실행 → 실 JSON 출력으로 파서/골든 확정. 즉시 가능, 제3자 스캔이지만 badssl은 이 용도로 공개된 서비스.

B) **로컬 docker 약한-TLS 서버 기동 후 실측** — 구 프로토콜/약한 cipher를 강제한 nginx/openssl 컨테이너를 띄워 실측. 제3자 트래픽 없음, 재현 완전 통제. 기동 설정 필요.

C) **실 출력 골든만 확보(대상 실행 없이)** — testssl `--jsonfile` 실 스키마 샘플로 골든을 고정하고 파서만 검증. 이미지 통합 positive 단언은 이월.

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A

---

## Question 2
**dirhunt (PS-004 directory listing)** 의 취약 대상을 어떻게 확보할까요?

A) **로컬 nginx `autoindex on` 컨테이너 실측 (권장)** — 디렉토리 리스팅을 노출한 로컬 nginx를 띄워 실 dirhunt 실행 → 실 JSON 리포트로 파서/골든 확정. 완전 통제, 제3자 스캔 없음(제3자 대상 dirhunt는 무허가 능동 스캔이라 부적절).

B) **공개 디렉토리-리스팅 노출 대상 실측** — 공개된 index-of 노출 사이트를 대상으로 실측(무허가 스캔 리스크).

C) **실 출력 골든만 확보(대상 실행 없이)** — dirhunt `--to-file` 실 스키마 샘플로 골든 고정, 파서만 검증. 이미지 통합 positive 단언은 이월.

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A

---

## Question 3
**subzy (PS-025 subdomain takeover)** 는 dangling CNAME(미청구 서비스로 연결된 서브도메인)이 필요해 가장 확보가 어렵습니다. 어떻게 처리할까요?

A) **mock 서버로 takeover 지문 응답 흉내 후 실측 (권장)** — subzy가 사용하는 fingerprint(예: GitHub Pages "There isn't a GitHub Pages site here", S3 "NoSuchBucket")를 로컬 mock HTTP 서버로 응답하게 하고, subzy를 그 대상으로 실행 → 실 `--output` JSON으로 파서/골든 확정. 제3자·실 dangling 없이 vulnerable=true 경로 실측.

B) **subzy만 이월, 파서는 실 스키마 골든으로만 검증** — subzy 실 출력 JSON 스키마로 골든을 고정해 파서 로직은 검증하되, 실 취약 대상 확보(진짜 dangling subdomain) 시점까지 이미지 통합 positive 단언은 정직하게 이월.

C) **실제 dangling subdomain 확보 후 실측** — 실 dangling CNAME을 마련(시간·리스크 큼).

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A

---

## Question 4
**골든/검증 기준** 은 이전 증분(r8)과 동일하게 갈까요?

A) **실 도구 출력 캡처 골든 + 이미지 통합 positive 단언 (확보된 대상 한정) (권장)** — 각 파서를 실 도구 출력에서 뜬 골든으로 고정하고, 대상이 확보된 도구는 in-image 통합에서 positive finding(올바른 CWE)까지 단언. 미확보 도구는 정직하게 이월 표기. r8과 동일한 정직성 규율.

B) **유닛 골든만, 이미지 통합 positive는 전부 이월** — 파서 유닛 테스트/골든만 이번 증분에, 이미지 통합 positive 단언은 별도 증분.

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A. 이렇게 되면 종합적인 하나의 취약점 서비스를 만드는 것도 좋을거 같은데.

---

## Question 5
**이번 증분 스코프** 를 어떻게 잡을까요?

A) **3개 모두 이번 증분** — 확보 가능한 대상부터 실측하고, 미확보분은 정직하게 이월 표기(부분 DONE). 한 증분에서 최대 커버리지.

B) **즉시 확보 가능한 것만(testssl + dirhunt), subzy 별도 증분** — 대상 확보가 쉬운 2개를 먼저 마감하고 subzy는 다음 증분으로 분리.

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A
