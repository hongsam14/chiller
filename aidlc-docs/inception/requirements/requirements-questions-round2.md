# 요구사항 분석 — 2차 질문 (명확화 + 확장 규칙 필수 항목)

**단계**: INCEPTION → Requirements Analysis
**왜 2차인가**: 1차 답변(`requirements-questions.md`)에는 논리적 모순이 없었습니다. 다만 (1) Q5=A와 Q6=B 사이에 **설계상 미명세된 통합 지점**이 하나 있고, (2) 활성화한 **resiliency 익스텐션이 Requirements 단계에서 필수로 요구하는 질문**들이 있어 함께 묶었습니다.

**답변 방법**: 각 질문의 `[Answer]:` 태그 뒤에 선택지 문자를 적어주세요. 다 되시면 알려주세요.

---

## 질문 1 — [핵심] 6종(Q6=B)의 결과가 passive-scanner 리포트로 어떻게 흘러가는가

1차에서 **Q5=A**(passive-scanner는 coolant와 코드 공유 없는 독립 레포)와 **Q6=B**(네이티브 6종 PS-008/011/012/013/022/023을 이 저장소 coolant의 Nuclei 커스텀 template으로 구현)를 선택하셨습니다. 그러면 26개 중 6개는 coolant가 실행합니다. 계획서는 passive-scanner 엔진이 **모든 항목을 단일 PS-ID 리포트로 정규화**한다고 규정하므로, 그 6개 결과가 passive-scanner로 유입되는 경로를 정해야 합니다.

> 참고: 계획서 §01은 "Nuclei가 이 26개 컴플라이언스 항목에는 오탐이 과다해 부적합"이라 밝히면서도 §04에서 6종은 Nuclei template으로도 가능하다고 봅니다. 6종은 응답 body 앵커 기반의 단순 검사라 오탐 위험이 가장 낮은 부분집합이므로 B가 방어 가능하지만, 앵커를 타이트하게 잡는다는 전제가 필요합니다.

A) **passive-scanner → coolant REST API 호출.** passive-scanner가 coolant의 `/api/run`으로 그 6개 template 스캔을 요청하고 결과를 폴링/콜백으로 수신 (런타임 의존: passive-scanner가 coolant에 의존)

B) **passive-scanner → coolant Mongo 결과 직접 조회.** 공유 스토리지에서 결과를 읽어 정규화 (데이터 계층 의존)

C) **상위 파이프라인이 병합.** 두 시스템이 각자 리포트하고, 바깥의 오케스트레이터/파이프라인이 PS-ID 기준으로 합침 (passive-scanner는 6종을 모름, 진짜로 완전 독립)

D) **6종을 passive-scanner 네이티브로 되돌림 (Q6=A로 변경).** coolant 접점을 없애 독립성을 지킴 — 계획서 기본안. 이 경우 이 질문의 의존성 문제 자체가 사라짐

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: D

---

## 질문 2 — [RESILIENCY-02] RTO/RPO 및 재해 복구 전략

신규 passive-scanner의 복구 목표입니다. (배치성 스캔 잡이라 상시 서비스보다 목표가 낮은 편일 수 있습니다.)

A) RPO/RTO: 시간 단위 — Backup & Restore. 최저 비용. 실패 시 IaC 재배포 + 백업 복원. 비핵심 워크로드에 적합

B) RPO/RTO: 수십 분 — Pilot Light. 데이터 상시, 서비스 유휴

C) RPO/RTO: 분 단위 — Warm Standby. 데이터 상시, 축소 용량 가동

D) RPO/RTO: 준실시간 — Multi-site Active/Active. 최고 비용

E) 해당 없음 — 단일 리전 다중 AZ로 충분, 크로스 리전 DR 불필요

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: C

---

## 질문 3 — [RESILIENCY-03] 변경 관리 프로세스

프로덕션 변경을 어떻게 관리합니까? AI-DLC는 새 프로세스를 만들지 않고 답변에 맞춥니다.

A) 기존 조직 프로세스 사용 — 이름/도구를 알려주세요 (예: Jira Change, ServiceNow, 내부 CAB). **coolant와 동일하다면 그렇게 적어주세요**

B) 아직 없음 — AI-DLC가 경량 변경관리(변경 기록 + 승인 + 롤백 노트)를 제안

C) 해당 없음 — 이 워크로드는 형식적 변경관리 면제 (사유 기록)

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: B

---

## 질문 4 — [RESILIENCY-04] CI/CD 도구

coolant는 Make + Docker + ECR + skaffold + Helm을 씁니다. 신규 passive-scanner는?

A) 기존 파이프라인 사용 — **coolant와 동일(Make/Docker/ECR/skaffold/Helm)** 또는 도구명 명시

B) 없음 — AI-DLC가 IaC/런타임에 맞는 CI/CD 파이프라인을 제안

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: B. A에 기반하지만, 복잡도를 낮추고 AWS 의존성을 없애고 싶음.

---

## 질문 5 — [RESILIENCY-04] 롤백 방식

실패한 프로덕션 배포를 어떻게 롤백합니까?

A) 이전 IaC/아티팩트 버전 재배포 (버전 고정 롤백)

B) Blue/green 이전 환경으로 스왑백

C) Canary 자동 롤백 (헬스/메트릭 회귀 시)

D) DB 인지 롤백 필요 (스키마/데이터 마이그레이션 역전) — 별도 설계 플래그

E) 조직 기존 롤백 절차 사용 — 참조 제공

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: C

---

## 질문 6 — [RESILIENCY-04] 배포 전략

이 워크로드의 리스크 프로파일에 맞는 배포 방식은?

A) 직접/in-place (최저 비용, 최대 blast radius) — 비핵심 워크로드에 허용

B) 롤링 (점진적 인스턴스 교체)

C) Blue/green (무중단 컷오버, 높은 비용)

D) Canary (점진 트래픽 이동 + 자동 롤백)

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: C

---

## 질문 7 — [RESILIENCY-08] 리전 토폴로지

coolant는 단일 리전(`ap-northeast-2`)입니다. passive-scanner는?

A) 단일 리전, 다중 AZ — 존 장애는 견디되 리전 전체 장애는 아님. 낮은 비용 (RTO/RPO A/B/E와 정합)

B) 멀티 리전 active-passive — 리전 장애 시 페일오버. 높은 비용

C) 멀티 리전 active-active — 리전 장애 무중단. 최고 비용

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: X. 일단은 AWS 의존성 제거를 위해 고려하지 않음

---

## 질문 8 — [RESILIENCY-15] 장애 대응 프로세스

프로덕션 장애를 어떻게 처리합니까?

A) 기존 장애 대응 프로세스 사용 — 참조 제공 (예: PagerDuty, 내부 on-call). **coolant와 동일하다면 그렇게 적어주세요**

B) 없음 — AI-DLC가 경량 장애 대응 + COE(오류 교정) 프로세스를 제안

X) 기타 ([Answer]: 태그 뒤에 기술해주세요)

[Answer]: B
