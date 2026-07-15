# Extension Compliance Summary — probe Infrastructure Design

확장 3종 전부 Enabled(aidlc-state Extension Configuration). 이 단계(Infra Design)에 적용 가능한 규칙만 평가.

## security-baseline
| 규칙 | 판정 | 근거 |
|---|---|---|
| SECURITY-09 (최소 공격면 컨테이너 = distroless nonroot) | ⚠️ **수정(문서화된 리스크 수용)** | Q2=B로 distroless static 포기(python/bash/openssl 필요). **비-블로킹 override**: 사용자 명시 결정 + 완화책 — non-root, read-only 루트 FS, 디스크 emptyDir 스크래치(per-scan 정리), cap ALL 드롭, no-new-privileges, digest 핀. IQ1은 A(debian slim) 선택(B Chainguard는 미채택) → 공격면이 slim만큼 존재하나 하드닝으로 상쇄. trivy/syft가 신규 레이어 스캔. |
| SECURITY-10/13 (공급망 핀·무결성) | ✅ 준수 | 베이스 digest 핀; Go=go.sum(tool 지시자); Python=pipx 버전/SHA 핀; testssl=git 태그. `latest` 금지. Dependabot 추적. |
| SECURITY-12 (시크릿 out-of-band) | ✅ N/A(불변) | 이 증분은 시크릿 도입 없음. 기존 env/mount 규율 유지. |
| 그 외(인증/권한/암호화 등) | N/A | 이 증분은 이미지/번들/하드닝 범위. 런타임 인증(Bearer)·암호화는 불변. |

## resiliency-baseline
| 규칙 | 판정 | 근거 |
|---|---|---|
| 서브프로세스 수명주기(좀비 방지) | ✅ 설계 반영(구현=Code Gen) | NFR-P1 프로세스그룹 kill은 코드 단계. Infra 측: read-only FS + 휘발 스크래치로 잔여 프로세스/파일 최소화. |
| 리소스 경계 | ✅ 반영 | k8s requests/limits 상향(툴 동시 서브프로세스 감안), tmpfs 아닌 디스크 스크래치(IQ5=B)로 메모리 고갈 회피. |
| 격리/롤백 | ✅ 반영 | 이미지 롤백 경로 명시(deployment-architecture); `argv=nil` 정직 복귀. |

## property-based-testing
| 규칙 | 판정 | 근거 |
|---|---|---|
| PBT | N/A(이 단계) | 파서 속성 테스트(NFR-P6)는 Code Gen 산출물. Infra Design은 테스트 실행 **환경**(IQ4=A 이미지 내부)만 확정. |

## Blocking findings
**0** — SECURITY-09 수정은 사용자 명시 결정 + 완화책을 갖춘 문서화된 override이므로 블로킹 아님(워크플로가 사용자 override 허용). 잔여 리스크: changeme Py2(§5, Code Gen에서 해소), 이미지 크기(리소스 상향으로 대응).
