# ctx — 여러 레포에 걸친 작업을 위한 컨텍스트 매니저

> 하나의 논리적 작업은 종종 여러 레포(`front`, `back`, `cli`…)에 걸쳐 있습니다.
> `ctx`는 그 작업의 **컨텍스트를 한곳에** 모읍니다 — 목표(goal), 프로젝트별
> worktree와 spec, 진행 상황, 그리고 작업이 만들어낸 지식까지. 모두 `~/.ctx`
> 아래 평범한 파일로, 사람과 AI 에이전트 양쪽을 위해 설계됐습니다.

단일 정적 Go 바이너리. 데몬도, 데이터베이스도, 클라우드도 없습니다. **파일시스템이
진실의 원천**이고 worktree는 `git`이 소유합니다.

---

## 왜 만들었나

여러 레포를 동시에 건드리는 작업을 해봤다면 이 고통을 알 것입니다.

- **컨텍스트가 증발한다.** "내가 뭘 하고 있었지, 왜?"가 어디에도 없습니다 —
  여러 worktree와, 압축(compaction)되면 사라질 채팅 로그에 암묵적으로 흩어져
  있죠. 새 세션은 코드로부터 목표를 *역추론*하고, 미묘하게 틀립니다.
- **spec이 레포로 샌다.** 임시 작업 spec을 레포 작업 트리에 떨궜다가 `develop`에
  커밋되고, 남의 것과 충돌하고, 히스토리를 오염시킵니다.
- **plan이 흩어진다.** 각 레포엔 worktree가 있지만, *작업(task)* 그 자체를 가진
  곳은 어디에도 없습니다.

`ctx`는 **여러 레포를 가로지르는 작업을 1급(first-class) 실체로** 만들어 이를
해결합니다 — 영속적인 집(home), 북극성이 되는 목표, 그리고 "코드"(git worktree
안)와 "작업 컨텍스트"(`ctx`가 관리, 레포 안에는 절대 두지 않음)의 깔끔한 분리로.

---

## 철학

이 원칙들이 모든 설계 결정을 이끌었습니다. `ctx`가 *왜* 그렇게 동작하는지를
설명하므로 알아둘 가치가 있습니다.

1. **작업(task)이 단위다.** 프로젝트도, 브랜치도 아닌, 레포를 가로지르는 *논리적
   작업*. 모든 것이 여기에 매달립니다.
2. **파일시스템이 진실의 원천이다.** 작업당 작은 `task.yaml` 하나 + `git worktree
   list`. 인덱스도, 캐시도, DB도 없습니다 — 에이전트가 신뢰하는 도구의 최악의
   실패 모드는 캐시와 현실의 **드리프트**이기 때문입니다. 드리프트할 것 자체가
   없습니다.
3. **목표가 북극성이다.** 보존해야 할 가장 중요한 것은 목표 — `objective`와
   *검증 가능한* `done_when` 조건. 어딘가에 묻힌 산문이 아니라 구조화된 필드입니다.
   목표는 코드로부터 신뢰성 있게 역추론되지 않는 유일한 것이니까요.
4. **설정보다 구조.** spec은 worktree *바깥*, 옆에 둡니다 — `git`이 아예 볼 수
   없게. "spec이 develop에 샜다"는 문제를 규칙으로 막는 게 아니라 디렉토리 구조로
   불가능하게 만듭니다.
5. **지식은 프로젝트별로 복리(複利)로 쌓인다.** 작업은 일시적이지만 `front`나
   `back`에 대해 알게 된 것은 영속적입니다. 완료 시 발견은 죽은 작업 덤프로
   보관되는 게 아니라 *프로젝트별 토픽 페이지로 통합(consolidate)*되어, 다음
   작업이 다시 읽습니다.
6. **에이전트를 위해, 문서는 드리프트 없이.** 모든 읽기 명령은 `--json`을
   말합니다. 명령 레퍼런스는 `ctx --help`(코드에서 생성되어 절대 낡지 않음)에
   살고, 동반 스킬은 *언제/왜*를 가르칠 뿐 *어떻게*를 복제하지 않습니다.
7. **기본이 안전하다.** 파괴적 작업은 강제하지 않는 한 거부하고, 쓰기는 원자적이며,
   경로는 (심볼릭 링크로도) `~/.ctx`를 벗어날 수 없습니다.

---

## 멘탈 모델

작업은 자신의 프로젝트들을 소유하고, 각 프로젝트는 worktree와 spec을 소유합니다.
ctx가 관리하는 구조는 최대 **3단계 깊이**입니다 — 레포 자체의 트리는 `wt/` 안에
살며 ctx가 아니라 git의 영역입니다.

```
~/.ctx/                              # CTX_HOME ($CTX_HOME로 override, 기본값 ~/.ctx)
├── add-payment/                     # 작업(Task)
│   ├── task.yaml                    #   진실의 원천: goal, projects, worklist
│   ├── context.md                   #   서사: Background / Plan / Decisions / Journal
│   ├── front/                       #   작업에 속한 프로젝트
│   │   ├── spec.md                  #     ctx 관리 spec — 레포 밖, git이 볼 수 없음
│   │   └── wt/                      #     ~/Proj/front 의 git worktree
│   └── back/
│       ├── spec.md
│       └── wt/
└── _knowledge/                      # 작업을 가로질러 복리로 쌓이는 프로젝트별 지식
    ├── index.md                     #   항상 먼저 읽는 카탈로그
    ├── log.md                       #   append-only 통합 원장
    ├── _shared/                     #   2개+ 프로젝트에 걸친 지식
    │   └── payment-contract.md
    ├── front/
    │   └── pay-endpoint.md          #   토픽 페이지 (frontmatter: project[], category, sources[])
    └── back/
```

**라이프사이클:** `new`(목표 캡처) → 레포마다 `add`(worktree + spec 생성) →
진행·결정을 기록하며 작업 → 새 세션에서 `resume` → `know add`로 배운 것 통합 →
`done`(검증·정리, 지식은 남음).

---

## 설치

Go 1.22+ 와 `PATH` 상의 `git`이 필요합니다.

```sh
# CGO_ENABLED=0 은 순수 Go 정적 바이너리를 강제합니다(libc 링크 없음).
CGO_ENABLED=0 go build -o ~/bin/ctx ./cmd/ctx    # ~/bin 이 PATH에 있는지 확인
ctx --help                                       # 실행 확인
```

---

## 빠른 시작

```sh
# 1. 목표부터 캡처하며 작업 시작 (objective + 검증 가능한 done_when).
ctx new add-payment \
  --objective "add payment across front/back/cli" \
  --done-when "payment e2e passes, /pay returns 200, lint clean" \
  --background "users have asked for card payment"

# 2. 각 레포 등록. ctx가 worktree와 그 옆의 spec을 만듭니다.
ctx add front --repo ~/Proj/front
ctx add back  --repo ~/Proj/back

# 3. worktree 안에서 작업하며 작업 상태를 살아있게 유지.
cd ~/.ctx/add-payment/front/wt
ctx task add "build the payment form"
ctx task doing 1
ctx log "front form wired to /pay; chose optimistic UI"

# 4. 새 세션(또는 context 압축 이후): 모든 맥락을 재수화.
ctx resume --json        # goal + background/plan + worklist + next + journal + 실시간 git 상태
ctx goal --handoff       # native /goal 을 세션 안에서 재무장할 /goal 라인 출력

# 5. 이미 아는 것을 찾고, 배운 것을 통합.
ctx know search "결제"                          # 한국어/CJK 검색 동작 (임베딩 없음)
ctx know add --project back --topic pay-endpoint \
  --category gotcha --source-task add-payment --from notes.md

# 6. 마무리. dirty/미머지 worktree는 --force 없이는 거부; 지식은 남습니다.
ctx done
```

---

## 핵심 개념

### 목표 = objective + done_when (그리고 `/goal` 다리)

작업의 목표는 `task.yaml`에 구조적으로 저장됩니다.

```yaml
goal:
  objective: "add payment across front/back/cli"
  done_when: "payment e2e passes, /pay returns 200, lint clean"
```

`done_when`은 Claude Code의 native `/goal`에서 빌려온 *검증 가능한 완료 조건*입니다.
모호한 objective("결제 추가")는 코드만 보면 "다 된 것 같은데?"로 흐려지지만, 조건은
새 세션이 객관적으로 검증할 수 있는 계약입니다. `ctx goal --handoff`는
`/goal <done_when>` 라인을 출력해, 에이전트가 ctx의 영속 사본으로부터 세션 내의
native 목표를 재무장하게 합니다 — ctx는 **영속적 집**, `/goal`은 **세션 내 강제자**.

### 세션 재수화

`ctx resume`은 새 세션이 *가장 먼저* 실행하는 단일 진입점입니다. 목표,
`context.md`의 Background/Plan, *next* 항목이 도출된 worklist, 최근 journal 항목,
그리고 프로젝트별 **실시간** git 상태(branch, dirty, ahead/behind)를 조립합니다 —
누군가 마지막에 적은 노트가 아니라 *실제 현실*에 근거해서. 비어 있는 의도는
조용히 추측하지 않고 `MISSING`으로 드러냅니다.

### 프로젝트별 지식

`ctx done` 시 영속할 발견은 `_knowledge/<project>/` 아래(2개+ 프로젝트에 걸치면
`_shared/`) **프로젝트별 토픽 페이지로 통합**되며, 각각 출처 작업이 `sources:`
provenance로 도장 찍힙니다. 검색은 CJK 인식 토크나이저를 쓰는 `grep` + frontmatter
facet — DB도, 임베딩도 없습니다. 폴더는 *집(home)*, 토픽은 지식이 *복리로 쌓이는*
곳, 작업은 저장 축이 아니라 *provenance*입니다. `ctx know index`는 항상 먼저 읽는
카탈로그입니다.

### `done`은 fail-closed

`ctx done`은 작업의 발견이 통합되지 않았거나, 등록된 브랜치가 실제로 머지되지
않았거나(detach됐을 수 있는 worktree HEAD가 아니라 `refs/heads/<branch>`를 확인),
dirty/미머지 worktree가 있으면 마무리를 거부합니다. `--force`는 dirty/미머지
안전장치만 덮어쓰며, 통합 게이트는 **절대** 덮어쓰지 않습니다.

---

## 명령

전체 플래그는 `ctx <command> --help`. 모든 읽기 명령은 `--json`을 지원합니다.

| 명령 | 용도 |
|------|------|
| `ctx new <task>` | 작업 생성; `--objective` + `--done-when` 캡처. |
| `ctx add <project> --repo <path>` | 레포 등록: worktree + spec 생성. |
| `ctx ls` | 모든 작업을 상태·진척도와 함께 나열. |
| `ctx status` | 현재 작업 상세(또는 전체 개요). |
| `ctx current` | 현재 디렉토리가 속한 작업/프로젝트. |
| `ctx where` | `CTX_HOME`과 현재 작업 경로 출력. |
| `ctx task add\|todo\|doing\|done\|ls` | worklist 관리. |
| `ctx set-status <s> --project <p>` | 프로젝트 상태 설정. |
| `ctx log <message>` | `context.md` Journal에 날짜 항목 추가. |
| `ctx resume` | 새 세션을 위해 전체 작업 맥락 재수화. |
| `ctx goal [set\|--handoff]` | 북극성 목표 조회 / 수정 / 핸드오프. |
| `ctx spec [--project <p>]` | 프로젝트 spec 경로/내용 출력. |
| `ctx know add\|search\|index` | 프로젝트별 영속 지식. |
| `ctx done [<task>]` | 마무리: 검증, worktree 정리, 지식은 보존. |

---

## AI 어시스턴트를 위해

`ctx`는 코딩 에이전트가 쓰도록 설계됐습니다. 각자 하나의 역할을 가진 3계층:

1. **발견(Discovery)** — `docs/global-claude-md-snippet.md`의 스니펫을 전역 메모리에
   추가해, 에이전트가 따로 알려주지 않아도 `ctx`를 발견하게.
2. **레퍼런스(Reference)** — `ctx --help` / `ctx <cmd> --help`. 코드에서 생성되어
   드리프트가 없고, 필요할 때까지 컨텍스트 비용 0.
3. **행동(Behavior)** — `skill/context-manager/`의 동반 스킬이 *언제·왜*(세션 시작
   시 `resume`, 목표부터 캡처, `done` 전 통합)를 가르칩니다. 전체 명령 목록은
   복제하지 않습니다.

```sh
# 스킬 설치 (복사하거나, 이 레포를 추적하도록 symlink)
cp -R skill/context-manager ~/.claude/skills/context-manager     # Claude Code
ln -snf "$(pwd)/skill/context-manager" ~/.claude/skills/context-manager   # ...또는 symlink
cp -R skill/context-manager ~/.codex/skills/context-manager       # Codex

# 전역 발견 포인터 설치
cat docs/global-claude-md-snippet.md >> ~/.claude/CLAUDE.md       # Claude
cat docs/global-claude-md-snippet.md >> ~/.codex/AGENTS.md        # Codex
```

---

## 안전성 & 보증

- **드리프트 없음:** 캐시/인덱스 없음; 상태는 파일시스템 + `git`.
- **원자적 쓰기:** 관리되는 모든 파일은 temp + rename으로 기록.
- **경로 봉쇄:** 작업/프로젝트 이름과 저장 경로는 (심볼릭 링크로도) `~/.ctx`를
  벗어날 수 없음.
- **기본 거부:** `done`은 `--force` 없이 dirty/미머지 작업을 버리지 않음;
  `--json` 모드 실패 시 구조화된 `{error, code}`.
- **안정적 `--json` 계약:** golden 테스트로 고정되어 에이전트가 형태를 신뢰 가능.

---

## 설계 & 문서

- 설계 spec: [`docs/superpowers/specs/2026-06-18-context-manager-cli-design.md`](docs/superpowers/specs/2026-06-18-context-manager-cli-design.md)
- 구현 계획: [`docs/superpowers/plans/2026-06-18-context-manager-cli.md`](docs/superpowers/plans/2026-06-18-context-manager-cli.md)

## 로드맵 (v1에는 없음)

`ctx know lint`(orphan/stale/모순 탐지), 수백 페이지를 넘어서면 임베딩/하이브리드
검색, `ctx new` 시 관련 지식 자동 노출, 기계 판독용 `ctx schema --json`.
