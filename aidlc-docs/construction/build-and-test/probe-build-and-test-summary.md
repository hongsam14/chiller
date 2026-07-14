# Build and Test Summary — probe 증분 (r7)

**Date**: 2026-07-14 · **Scope**: 6 probe CLI 배선 + 프로세스그룹 kill + 멀티스테이지 이미지.
이 요약은 **로컬에서 실제 실행·검증한 것**과 **Docker/네트워크가 필요해 CI로 위임한 것**을 정직하게 분리한다.

## ✅ 로컬에서 실행·통과 (이 증분 산출물)
| 게이트 | 명령 | 결과 |
|---|---|---|
| build | `go build ./...` | **OK** |
| vet | `go vet ./...` (+`-tags integration`) | **OK** |
| gofmt | `gofmt -l internal cmd` | **clean** |
| arch-check | `make arch-check` | **OK** (NFR-02 유지) |
| unit + race | `go test ./... -race` | **PASS** (전 패키지) |
| gosec | 전 모듈 | **0 issues** (신규 `exec.CommandContext`/`syscall.Kill` 포함) |
| govulncheck | `make vuln` | **0** 영향 취약점 |
| probe 커버리지 | `go test ./internal/scan/checking/probe -cover` | **94.0%** |
| 모듈 커버리지 | `go test ./... -coverprofile` | 52.0% (통합-태그 테스트 미집계) |

### 이 증분이 실제로 증명한 것 (로컬)
- **NFR-P1 프로세스그룹 kill**: `TestDefaultExec_TimeoutKillsProcessGroup` — sh가 낳은 **손자 `sleep`이 타임아웃과 함께 사멸**함을 실측(직속 자식만 죽이면 실패하는 테스트). 좀비/고아 없음.
- **6 파서 정확성**: `parsers_test.go` — 골든 JSON → 기대 CWE Finding; 소유 CWE·Valid severity·evidence 경계.
- **정직성 불변식**: 손상 JSON→error(무판정), 빈 stdout→clean; **wired+바이너리부재→error(tool-failed, 빈 clean 아님)**(`TestWiredAdapter_MissingBinary_ReportsError`).
- **커버리지 회계**: `TestE2E_ProbesWithoutBinaries_…` — 바이너리 부재 환경에서 12 tool-failed·4 delegated·10 observation, 26 파티션.

## ⏳ CI/Docker로 위임 (여기서 실행 불가 — Docker·네트워크 없음)
| 게이트 | 어디서 | 무엇을 확정 |
|---|---|---|
| 이미지 빌드 | CI `supply-chain` + `probe-integration` | 멀티스테이지 Dockerfile 빌드 성공; 6 툴 바이너리 실제 설치 |
| **probe 통합** | CI `probe-integration`(이미지 내부) | 실 `defaultExec`가 실 바이너리 실행 + 파서가 **실 출력** 파싱; **tool-failed 12→1**(PS-019만); **골든 스키마 실측 검증(F6)** |
| Mongo/Valkey 통합 | CI `integration` | 기존 durable 백엔드 계약(불변) |
| trivy/syft | CI `supply-chain` | python/testssl 신규 레이어 취약점·SBOM |

## ⚠️ 미해결/확정 대기 (F6 — 첫 Docker 빌드에서 판명)
- **파서 JSON 스키마 6종 + 골든**: 손작성. 실 툴 출력과 어긋나면 `probe-integration`이 실패 → 파서/골든 수정(느슨화 금지).
- **Dockerfile 툴 핀**: httpx@v1.6.10·subzy@v1.1.0·testssl v3.2.1·humble==1.30·dirhunt==0.9.0·Corsy@SHA·python-slim digest — 존재 확인·조정 필요(언핀 금지). Corsy는 PyPI 미배포라 clone-wrapper 방식.
- **changeme(PS-019)**: 미배선(F5) — 정직 tool-failed. 후속 증분.

## Overall
- **Build (local)**: Success · **Unit/race/gosec/vuln (local)**: Pass · **Docker/통합**: 위임(CI)
- **Ready for Operations**: Yes (로컬 게이트 green; Docker 게이트는 CI에서 강제). Operations에서 production-readiness #0(probe 배선) 갱신.
