# Build and Test Summary — 검출 정합성 확대 (r9, #0d 나머지)

**Increment**: r9 · **Branch**: `feat/probe-parser-fidelity` · **Date**: 2026-07-15
**Scope**: probe 파서 정합성(testssl/dirhunt/subzy 실측 + humble/httpx/corsy hermetic 재검증) + 통합 취약 fixture.

## Gates — 로컬 (전부 green)
| Gate | 명령 | 결과 |
|---|---|---|
| build | `go build ./...` | ✅ |
| vet | `go vet ./...` | ✅ |
| vet (integration) | `go vet -tags integration ./...` | ✅ |
| gofmt | `gofmt -l .` | ✅ clean |
| arch-check | `make arch-check` | ✅ OK (data-plane 격리·domain 커널 규율) |
| unit + race | `go test ./... -race` | ✅ 전 패키지 green (registry·test wiring 26-파티션 포함) |
| gosec | `make gosec` | ✅ 0 findings |

## Unit — 파서 정합성 (실 골든 대비)
- `internal/scan/checking/probe` 유닛 테스트 green: `TestSubzyParse`/`TestTestsslParse`/`TestDirhuntParse`/`TestHumbleParse`(+clickjacking·malformed-section)/`TestHttpxParse`/`TestCorsyParse` + `TestParsers_MalformedJSON_IsError`/`TestParsers_EmptyStdout_IsClean`.
- **PBT-D1** `TestParsers_Property_OwnedCWEOnly` (300 iter): 산출 finding은 항상 owned CWE 1개·valid severity·tool명·evidence bound. dirhunt/subzy/testssl.

## Integration — in-image end-to-end positive (실 어댑터 defaultExec→파서)
프로브 이미지 빌드 후 `//go:build integration` 스위트를 **실 바이너리**로 실행, **모두 PASS**:
- `TestIntegration_URLProbes_RealExecAndParse` (httptest 벤치): humble CWE-319, httpx CWE-693, corsy CWE-346.
- `TestIntegration_FixtureProbes_PositiveFindings` (vuln-target fixture + badssl):
  - dirhunt → **CWE-548** (nginx autoindex)
  - subzy → **takeover** (NoCanonicalCWE, takeover 지문 mock)
  - testssl.sh → **CWE-1327** (rc4.badssl.com weak TLS)

### fixture positive 재현 절차 (opt-in, IQ2=A)
```sh
# 1) fixture 기동
docker compose -f test/fixtures/vuln-target/docker-compose.yml up -d --wait
# 2) 프로브 이미지 + 통합 테스트 바이너리
docker build -t chiller:ci .
CGO_ENABLED=0 go test -c -tags integration -o /tmp/probe.test ./internal/scan/checking/probe/
# 3) subzy fingerprints DB (self-fetch URL이 죽어 수동 제공 필요)
curl -fsSL -o /tmp/fingerprints.json \
  https://raw.githubusercontent.com/EdOverflow/can-i-take-over-xyz/master/fingerprints.json
# 4) vulnnet에 조인해 positive 단언 (egress 있어 testssl→badssl 가능)
docker run --rm --init --network chiller-vuln-target_vulnnet --entrypoint /probe.test \
  -v /tmp/probe.test:/probe.test:ro \
  -v /tmp/fingerprints.json:/scratch/subzy/fingerprints.json:ro \
  -e PROBE_FIXTURE_DIRHUNT=vulnweb -e PROBE_FIXTURE_SUBZY=takeover-mock -e PROBE_BADSSL_HOST=rc4.badssl.com \
  chiller:ci -test.v -test.run 'TestIntegration_(URLProbes|FixtureProbes)'
# 5) 정리
docker compose -f test/fixtures/vuln-target/docker-compose.yml down -v
```
CI: `.github/workflows/ci.yml` `probe-fixture-positive` 잡(`workflow_dispatch` opt-in)이 위를 자동화. 기본 CI(push/PR)는 env 미설정으로 positive skip → green 유지.

## Performance / Security
- Performance: N/A(파서 정합성 증분; 처리량 곡선 변화 없음).
- Security(security-baseline): fixture는 test 전용·내부 네트워크·호스트 미노출·더미 비밀. gosec 0. secrets 스캔 대상 아님(실 비밀 없음).

## 카운트/불변 확인
- 26 파티션 유지, probe-owned 9 / observation 13 (r8 대비 불변 — 이번 증분은 ownership 미이동).
- adapter 인터페이스·`cmd/chiller`·제품 `Dockerfile`·배포 매니페스트 불변.

## 이월 (Operations #0d로)
- subzy fingerprints DB 런타임 provisioning(이미지 번들 or 사이드카) — 파서는 검증 완료, DB 없으면 정직하게 tool-failed.
- observation well-known-path provider 배선(별개 backlog) — dormant 표면 재사용 예정.
