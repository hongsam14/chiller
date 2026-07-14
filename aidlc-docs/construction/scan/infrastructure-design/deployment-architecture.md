# Deployment Architecture — probe 증분 델타

기존 배포(단일 이미지 role-flag, k8s/compose)는 불변. 이 증분이 바꾸는 **델타만** 기술.

## 이미지
- 런타임 베이스: `gcr.io/distroless/static-debian12:nonroot` → **`python:3.12-slim-bookworm@sha256:…`** (digest 핀).
- 빌드: 멀티스테이지 (go-build + testssl-checkout → runtime). 7개 툴 번들(probe-infrastructure-design.md §1-2).
- 태그: digest 핀 유지, `latest` 금지(SECURITY-10/13).

## Kubernetes 매니페스트 델타 (`deploy/k8s/*`)
```yaml
# securityContext (Pod/Container)
runAsNonRoot: true
runAsUser: <app-uid>
readOnlyRootFilesystem: true          # NFR-P4
allowPrivilegeEscalation: false
capabilities: { drop: ["ALL"] }
# 스크래치 (IQ5=B — 디스크 emptyDir)
volumes:
  - name: scratch
    emptyDir: {}                       # 노드 디스크, per-pod 휘발, 종료 시 소멸
volumeMounts:
  - name: scratch
    mountPath: /scratch
env:
  - name: TMPDIR
    value: /scratch
# 리소스 (이미지 비대 → 상향; 후일 Chromium까지 감안)
resources:
  requests: { cpu: "...", memory: "..." }   # 툴 서브프로세스 동시 실행 감안 상향
  limits:   { cpu: "...", memory: "..." }
```
- HPA/큐 스케일링(기존 KEDA/CPU placeholder) 불변.
- 프록시/포트/서비스 불변(financial HTTPS는 §2).

## Compose 델타 (`deploy/compose/docker-compose.yml`)
- chiller 서비스 이미지 = 새 probe 이미지.
- `read_only: true` + `tmpfs`/volume 스크래치(`/scratch`), `TMPDIR=/scratch`.
- `cap_drop: [ALL]`, `security_opt: [no-new-privileges:true]`.
- Mongo/Valkey 서비스 불변.

## CI 델타 (`.github/workflows/ci.yml`)
- integration 잡: 이미지 빌드 후 **그 이미지 안에서** `go test -tags integration ./internal/scan/checking/probe/...`(IQ4=A). 기존 Mongo/Valkey testcontainers 통합과 병존.
- supply-chain 잡: trivy/syft가 python·testssl 레이어까지 스캔(레이어 증가 반영).
- dependabot: docker 생태계가 새 베이스 digest 추적(기존).

## 롤백
- 이미지: 이전 distroless 태그로 즉시 복귀 가능(코드의 `argv=nil` 복귀와 독립).
- probe 배선 자체: `argv` nil 복귀 → tool-failed 정직 상태.
