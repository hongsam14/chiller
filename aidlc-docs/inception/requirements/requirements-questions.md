# 요구사항 분석 — 확인 질문

**단계**: INCEPTION → Requirements Analysis
**기준 계획서**: `~/Documents/report/augmentation_plan.html` — KISA 컴플라이언스 26개 항목(PS-001…PS-026)을 커버할 **신규 passive-scanner 엔진** 구축.
**맥락**: 결합도 분석(`../reverse-engineering/nuclei-coupling-analysis.md` §0)에서 정리했듯, 계획서의 passive-scanner는 coolant와 **동일한 구조의 형제 오케스트레이터**입니다. coolant는 도구 하나(nuclei)를, passive-scanner는 도구 일곱(ZAP passive + 프로브 5종 + 네이티브 6종)을 조율하며, 둘 다 확보된 target URL을 받아 결과를 정규화·저장합니다. 따라서 핵심 결정은 "coolant를 고칠지"가 아니라 **"두 번째 오케스트레이터를 coolant와 어떤 관계로 지을지"** 입니다.

**답변 방법**: 각 질문의 `[Answer]:` 태그 뒤에 선택지 문자를 적어주세요. 맞는 선택지가 없으면 마지막 `X) 기타`를 고르고 태그 뒤에 자유롭게 기술하시면 됩니다. 다 되시면 알려주세요.

**순서 안내**: 1~4번은 사실 확인 질문이고, 5번이 실제 아키텍처 결정입니다. 5번의 정답은 1~4번 답에 **의존**하므로, 1~4번을 먼저 답한 뒤 5번을 답해주세요.

---

## 질문 1
신규 passive-scanner 엔진은 **어떤 언어로** 구현할 예정입니까?

가장 결정적인 질문입니다. Go라면 coolant의 엔진 중립 기반(Task FSM, asynq 래퍼, K8s Job 러너, Mongo, OTel)을 **공유 라이브러리로 추출해 재사용**하는 길이 열립니다. Go가 아니라면 코드 공유는 성립하지 않고, 논의는 "두 서비스가 컨테이너 계약으로 연결"되는 쪽으로 바뀝니다.

A) Go — coolant와 동일 (기반 코드 공유가 가능)

B) Python — (프로브 도구들이 대부분 Python/CLI라 어댑터 언어로 자연스러울 수 있음)

C) 그 외 언어

D) 아직 미정 — 이 결정을 이번 논의에서 함께 정하고 싶음

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: A. 특히 나는 Golang에서도 DI 패턴을 즐겨 쓰고, golang Interface를 통해서 class init 함수 호출을 강제하는 것을 좋아함. 또한 나는 domain pattern을 좋아함.

---

## 질문 2
이 저장소(`coolant`)의 현재 배포 상태는 무엇입니까?

`VERSION`이 `0.4.0`이고, ECR 푸시 타겟·Helm 차트·`deploy.sh`가 있습니다. coolant를 건드리는(=in-place로 진화시키는) 선택지의 비용을 이 답이 결정합니다.

A) 프로덕션 운영 중 — 파괴적 변경에는 마이그레이션과 대외 조율이 필요

B) 공용 스테이징 / 사내 환경에서 운영 중 — 부담스럽지만 내부 조율로 가능

C) 개발 단계 — 실사용 소비자 없음, 파괴적 변경 비용이 낮음

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: A

---

## 질문 3
coolant의 REST API와 MongoDB 결과 컬렉션을 사용하는 주체는 누구입니까?

nuclei의 `output.ResultEvent`가 **REST 공개 스키마**(`httpapi/model.go:34,43`)이자 **Mongo 문서 스키마**(`writer/mongo_output_writer.go`)를 겸합니다. 엔진 중립 `Finding` 타입은 passive-scanner가 어차피 필요로 하는데(도구 7종을 정규화해야 하므로), 이를 coolant에도 적용하면 **파괴적 API 변경 + 데이터 마이그레이션**이 됩니다. 이 질문이 그 비용을 산정합니다.

A) 우리가 통제하지 않는 외부 소비자가 있음 (예: Offen 콜백) — 계약 유지 또는 명시적 버저닝 필요

B) 우리가 통제하는 내부 소비자만 있음 — 파괴적 변경을 조율 가능

C) 아직 수동/임시 쿼리 외에 소비자가 없음

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: A

---

## 질문 4
두 스캐너의 **배포 형태**를 어떻게 두고 싶습니까?

계획서는 passive-scanner를 AI-Active-Scanner·Nuclei·ASM과 "**별도 단계, 동시 실행 안 함**"으로 규정합니다. 이는 배포 토폴로지 선택에 직접 영향을 줍니다.

A) **분리된 별도 배포체** — coolant(nuclei)와 passive-scanner는 각자 독립 서비스/이미지로 배포. (공유 라이브러리 또는 컨테이너 계약과 잘 맞음)

B) **단일 다중 엔진 배포체** — 하나의 오케스트레이터가 nuclei와 passive-scanner를 플러그인 엔진으로 모두 실행

C) 상관없음 / 운영 편의에 따라 결정

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: A

---

## 질문 5
위 답변을 전제로, 어떤 저장소 전략을 택하시겠습니까?

**AI 추천: C안(공유 라이브러리 추출)** — 단, 질문 1이 A(Go)이고 질문 4가 A(분리 배포)일 때. 질문 1이 B/C(비-Go)이면 **D안(컨테이너 계약)** 이 정답이 됩니다. 근거는 각 선택지에 함께 적었습니다.

A) **신규 저장소 · 패턴만 복사 (코드 공유 없음).** passive-scanner를 독립 서비스로 새로 짓되 coolant의 설계(Task FSM, asynq, K8s 러너, Mongo, OTel)를 참고해 베껴옴.
   - *비용*: `current_asynq_id` CAS/재개 정합성(`docs/reports/task-identity-analysis.md`)을 두 벌로 복제 → 이후 영원히 양쪽을 동시에 고쳐야 함. 두 스캐너가 크게 갈라질 전망일 때만 합리적.

B) **In-place · coolant를 다중 엔진 호스트로 진화.** 하나의 오케스트레이터가 nuclei와 passive-scanner를 플러그인 엔진으로 실행.
   - *비용*: 재사용은 최대지만, 독립 릴리스 가능한 두 스캐너를 하나의 배포체·하나의 blast radius로 묶어 계획서의 "별도 단계" 분리에 역행. `Finding`/API 파괴적 변경을 coolant의 실사용 소비자에게 먼저 떠안김. (질문 4가 B일 때 고려)

C) **공유 라이브러리 추출.** 결합도 분석 §3의 "엔진 중립 기반"(db, state, otel, util, asynq `TaskManager`, K8s `JobRunner`, 진행률 단위를 일반화한 Task FSM)을 별도 Go 모듈로 뽑아, coolant과 passive-scanner가 각자 import. 서로 흡수하지 않고, 각자 자기 엔진·자기 findings 스키마·자기 배포체 유지.
   - *근거*: 해당 기반은 nuclei 결합이 **0**이라 추출이 재설계가 아니라 파일 이동 + 진행률 단위 rename 수준. 두 스캐너가 "동시 실행 안 하는 별도 단계"라 단일 바이너리보다 라이브러리 공유가 맞음. 중립 `Finding` 타입은 passive-scanner가 어차피 필요(도구 7종 정규화) → 공유 모듈에 한 번만 정의하면 양쪽이 이득. **(질문 1=A, 질문 4=A 조건에서 추천)**

D) **계약만 연결 (코드 공유 없음).** 두 저장소 분리 유지. coolant의 기존 `WORKER_IMAGE_NAME` seam으로 non-Go 워커 이미지를 띄우고, 컨테이너 수준 계약(입력 JSON → findings JSON)을 정의. **(질문 1이 B/C(비-Go)이면 이 선택지가 정답)**

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: A

---

## 질문 6: 엔진 네이티브 6종 vs Nuclei 커스텀 template
계획서 §04는 네이티브 시그니처 6종(PS-008/011/012/013/022/023)을 **passive-scanner에 직접 구현**하는 대신 **Nuclei 커스텀 template으로도 구현 가능**하다고 언급합니다. 이미 CVE PoC용 Nuclei(=이 저장소)를 운영 중이므로, 이 6종을 어디에 둘지가 두 저장소가 스치는 유일한 접점입니다. 어떻게 하시겠습니까?

A) passive-scanner 엔진에 네이티브로 직접 구현 (계획서 기본안)

B) 이 저장소의 Nuclei 커스텀 template으로 구현 (기존 Nuclei 운영에 편입)

C) 이번 요구사항 분석에서 함께 비교·결정하고 싶음

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: B

---

## 질문 7: 보안 익스텐션
이 프로젝트에 보안 익스텐션 규칙을 적용할까요?

A) 예 — 모든 SECURITY 규칙을 차단성 제약(blocking constraint)으로 적용 (프로덕션 등급 애플리케이션에 권장)

B) 아니오 — 모든 SECURITY 규칙 생략 (PoC, 프로토타입, 실험적 프로젝트에 적합)

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: A

---

## 질문 8: 복원력(Resiliency) 익스텐션
이 프로젝트에 복원력 베이스라인을 적용할까요?

**이 익스텐션의 성격.** 활성화하면 **AWS Well-Architected Framework(신뢰성 기둥)** 와 복원력 리뷰 가이던스에서 도출된 **방향성 있는 설계 시점 모범 사례** 묶음이 적용됩니다. 요구사항·설계·코드를 내결함성, 고가용성, 관측 가능성, 복구 가능성 쪽으로 유도합니다.

**이 익스텐션이 아닌 것.** 활성화한다고 워크로드가 프로덕션 준비 완료가 되지 않으며, 어떤 가용성/RTO/RPO 목표도 보장하지 않습니다. 좋은 복원력 결정을 초기에 세워주는 출발점이며, 정식 AWS Well-Architected Review를 대체하지 않습니다.

A) 예 — 복원력 베이스라인을 방향성 모범 사례이자 설계 시점 가이던스로 적용

B) 아니오 — 복원력 베이스라인 생략 (빠른 반복이 우선인 PoC/프로토타입에 적합)

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: A

---

## 질문 9: 속성 기반 테스트(PBT) 익스텐션
이 프로젝트에 속성 기반 테스트(PBT) 규칙을 적용할까요?

A) 예 — 모든 PBT 규칙을 차단성 제약으로 적용 (비즈니스 로직·데이터 변환·직렬화·상태 저장 컴포넌트가 있는 프로젝트에 권장)

B) 부분 — 순수 함수와 직렬화 왕복(round-trip)에만 PBT 규칙 적용

C) 아니오 — 모든 PBT 규칙 생략 (단순 CRUD·UI 전용·얇은 통합 계층에 적합)

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: A
