# Clarification — persistence 증분 Infrastructure Design (라운드 1)

**Timestamp**: 2026-07-13
**대상 플랜**: `construction/plans/persistence-infrastructure-design-plan.md`
**확정된 답변**: Q1=A · Q2=A · Q3=A · Q4=A · Q5=A · **Q6=되물음(→C1)** · Q7=C · Q8=A · Q9=A · Q10=A · Q11=A · Q12=A

두 건만 확인하면 설계 아티팩트 생성에 들어갑니다.

---

## C1 — Q6 되물음에 대한 답: "캐시 값을 암호화하면 Observation의 regex 탐색은 어떻게 하나?"

### 결론: **영향 없습니다.** 암호화는 **Valkey에 저장될 때만** 적용되고, 체커는 **평문 `*domain.HAREntry`를 그대로** 받습니다.

암호화 경계가 어디인지가 핵심입니다. 봉투 암·복호는 **`ResponseCache` 구현 안**에서 끝납니다:

```
fetcher.Fetch()
  └─ cache.Put(ctx, scanID, url, entry)     ← 여기서 entry를 직렬화 + AES-GCM 암호화 → Valkey에 불투명 바이트 저장
                                                (Valkey는 평문을 본 적이 없음)

observation 체커
  └─ cache.Get(ctx, scanID, url)            ← Valkey에서 바이트 읽기 → 복호화 → 역직렬화
      → *domain.HAREntry (평문, 프로세스 메모리)
          └─ regexp.Match(entry.Body)       ← 평문 []byte에 대한 통상적인 regex. 코드 무변경.
```

즉 체커 코드(`internal/scan/observation/*`의 10개 시그니처)는 **한 줄도 바뀌지 않습니다.** 지금 `InMemory` 캐시가
평문 `*HAREntry`를 돌려주는 것과 **완전히 동일한 것을 돌려받습니다.**

이게 가능한 이유는 우리가 **Valkey 안에서 검색할 필요가 없기** 때문입니다. 캐시 조회는
`chiller:cache:{scanId}:{urlHash}` **정확 키 룩업** 하나뿐이고, 키는 암호화하지 않습니다(값만 암호화).
Valkey 쪽에서 내용 기반 검색(`SCAN`+패턴 매칭 같은 것)을 하는 코드는 없습니다.

**실제 비용은 두 가지이고, 둘 다 수용 가능합니다:**

| 비용 | 실체 |
|---|---|
| CPU | `Get` 마다 AES-256-GCM 복호화 1회. 바운드된 본문(NFR-16, 수백 KB 상한)에 대해 AES-NI 하드웨어 가속이 걸리므로 **µs 단위**. 네트워크 왕복(Valkey RTT)이 압도적으로 크다. |
| 평문의 프로세스 메모리 노출 | 복호화된 본문이 워커 프로세스 힙에 존재. **그러나 이건 이미 지금도 그렇다** — fetcher가 응답을 읽는 순간부터 평문이 메모리에 있다. 암호화가 막는 것은 *Valkey 인스턴스*(메모리 덤프·RDB 파일·백업·운영자 접근)에서의 노출이지, 워커 프로세스 내부가 아니다. |

**요약**: 암호화는 "Valkey가 본문을 못 보게" 하는 것이고, 체커는 여전히 평문을 봅니다. 두 목표는 충돌하지 않습니다.
Q6의 **선택지 C(본문 제외)** 만이 regex 탐색을 실제로 망가뜨립니다 — 본문이 없으면 PS-003(평문 로그인 폼)·JS 자산 탐색이
캐시를 못 쓰고 FR-02(단일 fetch 재사용)가 무력화됩니다. **A는 그런 손실이 없습니다.**

**Q6을 어떻게 확정할까요?**

- **A) 값 전체 AES-256-GCM 암호화** — 위 설명대로. 체커 코드 무변경, Valkey는 불투명 바이트만 봄. (권장)
- **B) TLS in-transit만, 값은 평문** — 복호화 CPU 0. 대신 Valkey 메모리/RDB/백업에 응답 본문이 평문 잔류.
- **C) 본문 제외(헤더/쿠키/상태만)** — regex 탐색이 캐시를 못 씀. FR-02 무력화. **비권장.**

[Answer]: A

---

## C2 — Q1=A의 파생 문제: `Report.FrameworkLabels`가 `Findings` **슬라이스 인덱스**를 참조한다

### 실사에서 발견한 사실

`internal/findings/report/report.go`:

```go
type Report struct {
    ScanID          domain.ScanID
    Findings        []domain.Finding
    Coverage        domain.Coverage
    MappingRevision string
    FrameworkLabels map[domain.Framework]map[string][]int  // ← framework → item → **Findings 슬라이스의 인덱스**
}
```

`FrameworkLabels`의 값은 **`Findings` 슬라이스에 대한 정수 인덱스**입니다. Q1=A는 `Report.Findings`를 Mongo에
임베드하지 않고 `findings` 컬렉션에서 **재구성(rehydrate)** 하겠다는 결정이므로, **재구성 시 순서가 원본과
정확히 같지 않으면 라벨이 엉뚱한 finding을 가리킵니다.** (조용한 데이터 오염 — 보고서가 PS-015 항목에
관계없는 finding을 붙여 보여줌.)

Mongo는 **문서 순서를 보장하지 않으므로**(정렬 없는 `find`는 임의 순서) 이건 반드시 처리해야 합니다.

**어떻게 할까요?**

- **A) finding 문서에 `idx` 필드를 저장하고, 로드 시 `sort by idx` (권장)** — `SaveFindings`가 슬라이스 위치를
  `idx: 0,1,2,…`로 함께 쓰고, `LoadFindings`가 `.Sort(bson.D{{"idx",1}})`로 읽어 **원본 순서를 정확히 복원**한다.
  `FrameworkLabels` 인덱스가 그대로 유효. 변경 최소, 기존 `Report` 구조 무변경.
  (계약: "`LoadFindings`는 `SaveFindings`에 준 슬라이스와 순서까지 동일한 슬라이스를 돌려준다" — mem 구현도 이미 만족.
  이 순서-보존 계약을 **계약 스위트 + PBT 불변식**으로 못 박는다.)
- **B) `FrameworkLabels`를 인덱스가 아니라 **finding 키**(CWE+URL+tool 해시)로 바꾼다** — 순서 의존성 자체를 제거.
  구조적으로 더 견고하지만 `report.Builder`·리포트 소비자·테스트가 바뀌는 **도메인 계약 변경**이라 이 증분의
  범위를 넘는다.
- **C) `Report.Findings`를 Mongo에도 계속 임베드한다** — Q1을 사실상 C(이중 저장)로 되돌리는 것. 16MB 한계 존치.

[Answer]:B

---

## 다음 단계

C1·C2 답변 → P0-6 완료 → Phase 1(설계 아티팩트 생성) → 승인 → Code Generation.
