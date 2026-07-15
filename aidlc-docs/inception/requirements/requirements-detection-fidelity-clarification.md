# Requirements Clarification — 종합 취약점 fixture 결정 (#0d 나머지, r9)

답변 감사합니다: **Q1=A(badssl) · Q2=A(로컬 nginx autoindex) · Q3=A(mock 지문) · Q4=A(실 골든+이미지 positive 단언) · Q5=A(3개 전부).**

Q4 답변의 메모 — *"이렇게 되면 종합적인 하나의 취약점 서비스를 만드는 것도 좋을거 같은데"* — 가 **Q1=A(testssl을 외부 badssl.com으로 실측)** 와 살짝 상충합니다. 세 도구를 하나의 로컬 취약 서비스로 묶으면 완전 hermetic(외부 의존 0·CI 재현 가능)이 되지만, testssl 대상을 badssl에서 로컬 약한-TLS 서버로 바꾸는 셈이라 Q1을 사실상 override 하게 됩니다. 이 결정이 설계(외부 의존 vs 로컬 fixture)와 산출물(fixture를 레포에 커밋할지)을 가르므로 명확화합니다.

각 질문의 `[Answer]:` 태그 뒤에 알파벳으로 답해 주세요.

---

## Clarification 1: 종합 취약점 서비스의 범위 (Q1 ↔ Q4 메모 정합)
세 probe 검증 대상을 어떻게 구성할까요?

A) **완전 통합 (권장) — 3개 전부 하나의 로컬 취약 fixture** — testssl도 로컬 약한-TLS(구 프로토콜/약한 cipher openssl) 서버로 전환해, weak-TLS + autoindex 디렉토리 + takeover 지문 mock 을 **하나의 docker-compose 취약 서비스**로 묶음. 완전 hermetic(외부 badssl 의존 제거), CI에서 재현 가능. **Q1=A를 로컬로 override.**

B) **부분 통합 — dirhunt+subzy만 로컬 fixture, testssl은 badssl 유지** — Q1=A 그대로 testssl은 외부 badssl로 실측하고, dirhunt/subzy 두 표면만 하나의 로컬 서비스로 묶음. 외부 의존 1개(badssl) 잔존.

C) **통합 안 함 — 3개 개별 ad-hoc 대상 (원안)** — badssl(testssl) + 로컬 nginx(dirhunt) + mock(subzy)를 각각 따로. 종합 서비스 미구축.

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: B. 하지만 이전 단계에서 probe한 취약점들도 모두 통합하는건 어떤지?

---

## Clarification 2: fixture 영속성 (레포 커밋 여부)
로컬 취약 fixture(Clarification 1에서 A/B 선택 시)를 어떻게 남길까요?

A) **재사용 test-fixture로 레포에 커밋 (권장)** — `test/fixtures/vuln-target/`(docker-compose + 설정)로 커밋해 CI·향후 probe 검증에서 재사용. 실 골든은 이 fixture 출력으로 고정.

B) **일회성 기동, 커밋 안 함** — 증분 중에만 띄워 실 골든을 뜬 뒤 fixture 자체는 커밋하지 않음(골든만 커밋).

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A

---

## Clarification 3: 스코프 라인 재확인 (미션 정합)
종합 취약점 fixture는 **테스트 대상(우리가 스캔당하는 쪽)** 이지 chiller 엔진 기능이 아닙니다. 이 경계를 확인해 주세요.

A) **동의 — fixture는 test/ 아래 검증용 대상일 뿐, chiller 런타임/제품 코드에 능동 공격·취약서비스 로직을 추가하지 않음 (권장)** — 미션(passive-scanner)·스코프 결정(2026-07-14) 유지. chiller는 여전히 관찰 기반 판정만.

B) **아니오 — 다른 의도가 있음** (아래 [Answer]: 태그 뒤에 설명)

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A
