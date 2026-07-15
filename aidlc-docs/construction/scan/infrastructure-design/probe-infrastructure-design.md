# Infrastructure Design — probe 증분 (unit: scan/probe)

**Decisions**: IQ1=A(python:3.12-slim-bookworm)·IQ2=A(pipx per-tool venv)·IQ3=B(testssl 번들 openssl)·IQ4=A(빌드 이미지 내부 통합 테스트)·IQ5=B(디스크 emptyDir 스크래치).
**Requirements**: `requirements-probe.md` · **Scope**: `scope-decision-active-vs-passive.md`.
**불변**: `Adapter`/`Checker` 인터페이스(PR-11) 불변; `argv` non-nil로만 `Wired()` 뒤집힘.

## 1. 런타임 이미지 아키텍처 (distroless static → 멀티스테이지 툴 이미지)

멀티스테이지. **build 스테이지들**에서 각 툴을 버전 고정 산출 → **런타임 스테이지**(`python:3.12-slim-bookworm`)로 COPY.

```
[go-build]   golang:1.26.5@sha256:…  → chiller 바이너리 + go-tool 바이너리(httpx, subzy)
[testssl]    (git checkout, 태그 핀)  → /opt/testssl (스크립트 + bin/openssl.* 정적)
[runtime]    python:3.12-slim-bookworm@sha256:…
             ├─ COPY chiller, httpx, subzy  (정적 Go 바이너리)
             ├─ COPY /opt/testssl           (스크립트 + 번들 openssl, IQ3=B)
             ├─ pipx install (per-tool venv, IQ2=A): humble, dirhunt, changeme, Corsy
             ├─ apt: git 불요(런타임), ca-certificates, (openssl은 testssl 번들 사용)
             ├─ nonroot 사용자 + read-only 루트 FS + 디스크 emptyDir 스크래치(IQ5=B)
             └─ ENTRYPOINT /app/chiller
```

- **베이스 digest 핀**(SECURITY-10/13): `python:3.12-slim-bookworm@sha256:…`. Dependabot이 이동 시 PR(기존 `.github/dependabot.yml` docker 생태계).

## 2. 툴별 번들 + 버전 고정 (Q3=A 생태계 관용)

| 툴 | 종류 | 소스 | 고정 방식 | 실행 경로 |
|---|---|---|---|---|
| **httpx** | Go 정적 | `github.com/projectdiscovery/httpx/cmd/httpx` | **go.mod `tool` 지시자 + go.sum**(Go 1.26 관용) → build 스테이지 `go build -o /out/httpx` | `/usr/local/bin/httpx` |
| **subzy** | Go 정적 | subzy 저장소 `@vX.Y.Z` | 동일(tool 지시자, go.sum 핀) | `/usr/local/bin/subzy` |
| **testssl.sh** | bash+openssl | `github.com/drwetter/testssl.sh` | **git checkout `vX.Y.Z` 태그**(--depth 1) → `/opt/testssl` 통째. **번들 `bin/openssl.Linux.x86_64`**(IQ3=B) | `/opt/testssl/testssl.sh --openssl=/opt/testssl/bin/openssl.Linux.x86_64` |
| **humble** | Python(PyPI) | `humble==X.Y.Z` | **pipx per-tool venv**(IQ2=A) + 버전 핀 | `pipx` shim `/opt/pipx/bin/humble` |
| **dirhunt** | Python(PyPI) | `dirhunt==X.Y.Z` | pipx per-tool venv + 핀 | `/opt/pipx/bin/dirhunt` |
| **Corsy** | Python(git) | `github s0md3v/Corsy@<SHA>` | pipx install `git+…@<SHA>`(PyPI 미배포) | `/opt/pipx/bin/corsy` |
| **changeme** | Python(git) | `github ztgrace/changeme@<SHA>` | pipx install git@SHA. ⚠️ **Py2 유산 리스크**(아래 §5) | `/opt/pipx/bin/changeme` |

정확한 버전/SHA는 Code Generation에서 락파일(go.sum / pipx pin / git tag)에 고정. 여기서는 **고정 메커니즘**을 확정.

## 3. 하드닝 (NFR-P4 — SECURITY-09 수정 보완)

| 항목 | 결정 |
|---|---|
| 사용자 | **non-root**(전용 uid/gid, `USER app`). 툴은 nonroot로 실행. |
| 루트 FS | **read-only**(`readOnlyRootFilesystem: true`). |
| 스크래치 | **디스크 `emptyDir`**(IQ5=B) 마운트 `/scratch`(+ `TMPDIR=/scratch`). pipx venv는 이미지에 굽고 읽기전용; 툴 런타임 임시파일만 여기로. **per-scan 정리**(툴 종료 후 산출물 삭제; 캡처 바디를 런 밖으로 남기지 않음 — FR-12 정합, evidence wipe는 ResponseCache.WipeScan이 별도 담당). |
| capabilities | **전부 드롭**(`drop: [ALL]`), privilege escalation 금지(`allowPrivilegeEscalation: false`). |
| 이미지 | digest 핀, `latest` 금지(SECURITY-10/13). |
| SBOM/스캔 | 기존 CI `syft`/`trivy` 확장(python/testssl 레이어 포함). |

## 4. 통합 테스트 실행 환경 (IQ4=A)

- `//go:build integration` 파서 골든 테스트를 **빌드된 probe 이미지 안에서** 실행. CI: 이미지 빌드 → 그 이미지에서 `go test -tags integration ./internal/scan/checking/probe/...`.
- "배포되는 것과 동일 환경"에서 `defaultExec`(실 `os/exec`)가 각 실 바이너리를 최소 1회 실행함을 보장(Q4=C 통합 파트). 유닛 파트(경량 실 바이너리로 exec·timeout·프로세스그룹 kill 경로)는 일반 `go test`에서.
- 골든 픽스처: 각 툴 JSON 출력 샘플을 커밋, 파서 회귀 고정(Q5=A).

## 5. 리스크 / 주의

- ⚠️ **changeme Python2 유산**: 상류가 Py2 기반일 수 있음. Code Gen에서 (a) py3 유지 포크 채택 or (b) py2 인터프리터 별도 번들 중 택1. py2 번들은 공격면↑ → **포크 우선 조사**. 최악의 경우 PS-019를 잠정 tool-failed로 두고 별도 처리(정직성 불변식).
- **Corsy/changeme PyPI 미배포** → git SHA 핀(pipx `git+URL@SHA`). 태그 없으면 SHA가 유일한 재현 앵커.
- **이미지 크기 증가**: python + 4 pipx venv + testssl → 수백 MB. 후일 Chromium(§2/§4)까지 더해지므로 k8s 리소스 request/limit 상향 필요(§deployment-architecture 델타).
- **프록시 env 존중(G1)**: financial 모드에서 각 툴이 `HTTP(S)_PROXY`를 따르는지 Code Gen에서 검증(무시 툴은 `argv` 명시 플래그 강제). 단, 이 증분은 직결 배선이 기본(scope-decision).

## 6. 산출물
- 본 문서 = `infrastructure-design.md` 역할.
- `deployment-architecture.md` — k8s/compose 델타(이미지 교체·read-only+scratch·리소스).
- `compliance-summary.md` — 확장(security/resiliency/PBT) 준수 + SECURITY-09 수정 리스크 매핑.
