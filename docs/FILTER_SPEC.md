# Packet Inspector Filter Specification

## Overview
Packet Inspector의 필터 기능을 개선하여 패킷 분석을 더 효율적으로 수행할 수 있도록 합니다.

## Features

### 1. Quick Direction Filter (방향 필터)

**UI 위치**: 필터 입력창 왼쪽에 버튼 그룹으로 배치

| 버튼 | 설명 | 동작 |
|------|------|------|
| `All` | 모든 방향 | 필터 없음 (기본값) |
| `UP ->` | Upstream → Client | direction이 "UP -> Client"인 패킷만 표시 |
| `-> UP` | Client → Upstream | direction이 "Client -> UP"인 패킷만 표시 |

**동작**:
- 버튼은 토글 형태로 동작 (하나만 선택 가능)
- 고급 필터와 조합 가능 (AND 조건)

---

### 2. Advanced Filter Syntax (고급 필터 문법)

**지원 문법**:

| 문법 | 예시 | 설명 |
|------|------|------|
| `dir:up` | `dir:up` | Upstream 방향만 |
| `dir:down` | `dir:down` | Downstream 방향만 |
| `len:N` | `len:10` | 정확히 N 바이트 |
| `len:>N` | `len:>10` | N 바이트 초과 |
| `len:<N` | `len:<10` | N 바이트 미만 |
| `len:>=N` | `len:>=10` | N 바이트 이상 |
| `len:<=N` | `len:<=10` | N 바이트 이하 |
| `len:N-M` | `len:5-20` | N~M 바이트 범위 |
| `hex:XX XX` | `hex:f7 0e` | Hex 패턴 포함 |
| `ascii:text` | `ascii:hello` | ASCII 텍스트 포함 |

**복합 필터**:
- 공백으로 구분하여 여러 조건 AND 조합
- 예: `dir:up len:>10 hex:f7` → Upstream이고 10바이트 초과이며 f7 포함

**기본 동작**:
- 문법 prefix가 없으면 기존처럼 hex와 ascii 모두에서 검색

---

### 3. Regex Support (정규식 지원)

**문법**: `/pattern/` 또는 `/pattern/flags`

| 예시 | 설명 |
|------|------|
| `/f7.{2}0e/` | f7 뒤에 2바이트 후 0e |
| `/^f7/` | f7로 시작 |
| `/0e$/` | 0e로 끝남 |
| `/hello/i` | 대소문자 무시하여 hello 검색 |

**적용 대상**: Hex 데이터 (공백 제거 후 매칭)

---

### 4. Filter Presets (필터 프리셋)

**UI 위치**: 필터 입력창 오른쪽에 드롭다운 메뉴

**기능**:
- 현재 필터를 이름과 함께 저장
- 저장된 프리셋 목록에서 선택하여 적용
- 프리셋 삭제 기능
- LocalStorage에 저장 (브라우저 유지)

**기본 프리셋** (삭제 불가):
| 이름 | 필터 |
|------|------|
| Large Packets | `len:>50` |
| Small Packets | `len:<=10` |
| Upstream Only | `dir:up` |
| Downstream Only | `dir:down` |

---

### 5. Highlight Mode (하이라이트 모드)

**UI 위치**: 필터 입력창 옆에 토글 버튼

**동작**:
- OFF (기본): 매칭되지 않는 패킷 숨김 (기존 동작)
- ON: 모든 패킷 표시, 매칭되는 패킷/바이트 하이라이트

**하이라이트 스타일**:
- 매칭 행: 배경색 강조 (노란색/주황색 계열)
- 매칭 바이트: 텍스트 하이라이트

---

## UI Layout

```
┌─────────────────────────────────────────────────────────────────────────┐
│ Packet Inspector                                                         │
├─────────────────────────────────────────────────────────────────────────┤
│ [All] [UP ->] [-> UP]  │ [Filter input...] │ [Presets ▼] │ [Highlight] │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  Time    Direction    Length    Hex                        ASCII        │
│  ...                                                                    │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Implementation Priority

1. **Phase 1**: Quick Direction Filter + Advanced Filter Syntax
2. **Phase 2**: Regex Support
3. **Phase 3**: Filter Presets
4. **Phase 4**: Highlight Mode

---

## Technical Notes

### Filter State
```javascript
{
  direction: 'all' | 'up' | 'down',
  text: string,           // raw filter text
  parsed: {
    direction?: 'up' | 'down',
    length?: { op: string, value: number, max?: number },
    hex?: string,
    ascii?: string,
    regex?: RegExp
  },
  highlightMode: boolean
}
```

### Storage Key
- Presets: `serial-tcp-proxy-filter-presets`

### Filter Parsing
1. 정규식 체크 (`/pattern/`)
2. 고급 문법 파싱 (`key:value` 형태)
3. 나머지는 일반 텍스트로 hex/ascii 검색
