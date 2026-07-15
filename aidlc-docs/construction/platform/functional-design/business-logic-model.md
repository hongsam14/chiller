# Functional Design — Unit `platform` (U1) · Business Logic Model

**Stage**: 🟢 CONSTRUCTION → `platform` → Functional Design · **Date**: 2026-07-08

오케스트레이션 시퀀스. platform이 이미 존재하는 scan/findings를 어떻게 배선하는지의 실행 모델.

---

## 1. 부팅 (composition root, `cmd/chiller`)

```
1. 플래그 파싱(--role, --addr, config 경로)
2. config 로드: mappings/kisa.yaml, coverage/kisa.yaml, severity.yaml, allowlist, authz 토큰
3. 체커 조립: observation.Default() + probe.Default() → registry.Register(...)
4. 부팅 게이트(PR-6): coverage.New(registry.OwnershipForFindings(), mapping, spec)
      실패 → 로그 + 프로세스 종료(non-zero). 성공 → Accountant 보관.
5. seam 인스턴스화(MVP 구현): TaskStore(mem) · WorkQueue(chan) · ProgressReporter(mem)
      · safety.ApprovalGate(allowlist) · fetch.Fetcher/ResponseCache/StaticDiscoverer
      · findings 파이프라인(normalize/dedup/severity/compliance/coverage/report)
6. Orchestrator 조립(위 의존 주입). role별 기동:
      all     → HTTP 서버(ScanAPI) + 워커 풀 in-process
      manager → HTTP 서버만
      worker  → 워커 풀만
```

## 2. CreateScan + StartScan (제출 경로, US-001/024/026)

```
POST /scans (Bearer authz — PR-7b, 실패 401)
  └─ Authorizer.Authorize(req) → ok
  └─ ingest.Ingest(req) → URLSet{URLs, Options}   (검증·기본값, 실패 400 — PR-7c)
  └─ Orchestrator.CreateScan:
        scanID 생성 → Scan{State: created, Request} → TaskStore.SaveScan
  └─ Orchestrator.StartScan(scanID):
        CAS(created→running)                        (T1)
        fan-out(PR-3): 등록 체커를 Scope로 분류 → WorkItem 집합 생성
        각 WorkItem → WorkLedger{pending} 기록 + WorkQueue.Enqueue
        ProgressReporter.Init(total)
  └─ 202 Accepted {scanID}
```

- URL-scoped WorkItem: (URL × 모든 URL-scoped 체커) — Executor가 공유 fetch 1회로 처리.
- host-scoped WorkItem: (host × host-scoped 체커 1).

## 3. 워커 루프 (실행 경로, US-025/027)

```
loop:
  item ← WorkQueue.Dequeue(ctx)                     (블록; ctx 취소 시 종료)
  if !TaskStore.CompareAndSetItemState(item, pending→inflight): continue  // 다른 워커 소유
  outcome ← scan.Executor.Run(ctx, item, cache)     // PR-4: 격리, ctx 취소 전파
  TaskStore.RecordOutcome(item, done, outcome)
  ProgressReporter.OnDone(item, outcome)
  if TaskStore.ListUnfinished(item.ScanID) == ∅:
        finalize(item.ScanID)                        // §4
```

- `Executor.Run`은 URL-scoped면 Fetch+Cache 4단계(①fetch ②discover ③fetch JS ④ScanContext) 후 체커 실행; host-scoped면 gate.Authorize 후 프로브 실행(H2). 한 체커 실패는 `outcome.Failed`에 격리.
- 워커 크래시로 `inflight` 잔류 → `ResumeScan`이 재큐(원장 진실원천).

## 4. Finalize (집계 → 파이프라인 → 리포트, US-014~021)

```
finalize(scanID):
  outcomes ← TaskStore.AllOutcomes(scanID)
  # findings 파이프라인 (per-scan 조립)
  raw      ← outcomes.flatMap(Findings)
  findings ← normalize.NormalizeAll(raw) → dedup.Dedup → (severity는 normalize 내 적용)
  store.Put(scanID, findings)
  # 커버리지 회계 (PR-5 변환)
  scanOutcome ← foldOutcomes(outcomes, mappingMapper)   // RanOK / NotApplicable[c][psid] / (fail=암묵)
  coverage    ← Accountant.Account(scanOutcome)         // 26분할, Validate 보장
  # 리포트
  report ← report.Build(scanID, findings, coverage, frameworks)  // MVP: KISA 투영
  TaskStore.SaveReport(scanID, report)
  CAS(running→completed)                                 (T2)
```

- `foldOutcomes` = PR-5 규칙 구현: 체커별 applicable-ok/na/fail 신호를 `coverage.ScanOutcome`로.
- **불변식**: `report.Build`는 `coverage.Validate(spec)`가 성립하는 Coverage만 받음 — Account가 구성상 보장.

## 5. GetStatus / GetReport (조회 경로)

```
GET /scans/{id}       → Authorizer → Orchestrator.GetStatus → ProgressSnapshot(JSON)
GET /scans/{id}/report→ Authorizer → 완료 전이면 409/202(아직 running), 완료면 Report(JSON)
```

## 6. ResumeScan / StopScan

```
ResumeScan(scanID):                                   (T5, 멱등)
  assert State == running
  for item in TaskStore.ListUnfinished(scanID):       // pending ∪ inflight
      TaskStore.ResetItemState(item, →pending); WorkQueue.Enqueue(item)
  # 완료 item 재실행 없음; 캐시 히트 시 재fetch 없음(FR-02)

StopScan(scanID):                                     (T3/T4)
  CAS(running→stopped) 또는 CAS(created→stopped)
  WorkQueue.Drain(scanID)      // 미분배 item 취소
  ctxCancel(scanID)            // inflight fetch 취소(H4)
```

## 7. 시퀀스 요약 (한 스캔의 데이터 흐름)

```
P4 ─submit─▶ ScanAPI ─authz─▶ Orchestrator.CreateScan/StartScan
                                   │ FSM created→running, fan-out
                              WorkQueue ─┬─▶ worker: CAS claim → Executor.Run → outcome
                                         └─▶ (…×N workers, bounded)
                              마지막 item 종료 ─▶ finalize:
                                   normalize→dedup→severity  (findings)
                                   fold→Account              (coverage 26분할)
                                   report.Build              (KISA 투영)
                              FSM running→completed, SaveReport
P1/P2 ─GetReport─▶ ScanAPI ─authz─▶ Report(JSON)
```

## 7b. 금융권 인증 Proxy 경로 (PR-12, financial 모드)

```
부팅(--role=proxy 또는 all+financial):
  storageState 로드 → BrowserSession.New(Chromium, context+storageState) → 탭풀 T
  AuthProxy.Serve(addr)  // MITM CA(worker 신뢰) 리슨

worker(앱계층 요청, financial 모드):
  Fetcher.Transport.Proxy = proxyURL  (probe는 HTTP_PROXY env; testssl 제외 — PR-12c)
  GET target ──▶ AuthProxy
                   ├─ 탭 슬롯 획득(T개, blocking)  ─ PR-12e
                   ├─ BrowserSession.Do: 컨텍스트 쿠키/헤더 주입 → 대상 요청
                   ├─ per-host rate(SR-4) 강제
                   └─ 응답 ──▶ worker (인증된 응답)
  worker: 응답을 ScanContext로 → 체커 실행 (기존 흐름 동일)

testssl(전송계층): proxy 우회, 대상 직접 (TLS 핸드셰이크 점검)
```

- **단일 세션 불변식**: N개 worker(수평확장)가 전부 한 proxy를 가리킴 → 브라우저 컨텍스트 1개 공유. 세션은 proxy에만.
- 테스트: `BrowserSession`을 fake로 주입 → 실제 Chromium 없이 "세션 주입 + 탭풀 T 상한 + testssl 우회"를 검증.

## 8. 통합 테스트 계획 (code gen에서)
- **end-to-end(in-memory)**: httptest 대상 서버 → submit → 워커 실행 → completed → report에 26 PS-ID 전부 분류(Validate) + findings 존재. `--role=all` 경로.
- **resume**: 일부 item을 inflight로 남기고 ResumeScan → 완료 item 재실행 0, 최종 completed.
- **fail isolation**: 한 체커가 항상 에러 → 해당 PS-ID만 ToolFailed, 나머지 정상, 스캔 completed.
- **authz fail-closed**: 토큰 없음 → 401, 오케스트레이터 미호출.
- **부팅 게이트**: 고의로 매핑에서 한 항목 제거 → coverage.New 실패 → 기동 거부.
- **stop**: StopScan 후 in-flight ctx 취소 확인, 상태 stopped.
