# RuleSend

**내 AI 규칙을, 모든 기기에.**

공통 규칙을 한곳에서 편집·게시하고, **어떤 기기에 어떤 버전이 실제 설치됐는지** 확인합니다. Codex, Claude Code 등 로컬 Markdown 지침을 읽는 도구를 위한 자체 호스팅 Hub와 Agent입니다.

[English](README.md) · [제품 기획](docs/PRODUCT_PLAN.ko.md) · [명령 안내](docs/CLI.md) · [운영·복구](docs/OPERATIONS.md) · [보안 모델](docs/THREAT_MODEL.md)

> **초기 Alpha입니다.** 관리자 1명, macOS/Linux, 작은 Markdown 규칙 팩을 대상으로 합니다. 파일 설치 확인과 AI 앱의 규칙 로드·행동 검증은 다릅니다. 앱 로드와 행동은 이 버전에서 항상 **미확인**입니다.

## 왜 필요한가요?

Git에 규칙을 올려도 어느 기기가 받았는지는 별도로 확인해야 합니다. 파일을 복사한 뒤에도 기존 규칙을 덮어쓰지 않았는지, 다른 기기가 오프라인인지 파악하기 어렵습니다.

RuleSend는 **게시한 규칙과 각 기기의 실제 설치 상태 사이의 차이**를 보여줍니다.

- **원본은 한곳에:** Hub에 접속할 수 있는 브라우저에서 Markdown을 편집합니다.
- **초안과 배포를 분리:** 명시적으로 게시한 버전만 기기에 전달합니다.
- **전파 상태를 확인:** 목표 버전, 설치 버전, 마지막 연결, 충돌·오류를 함께 봅니다.
- **기존 내용 보존:** RuleSend 관리 블록만 바꾸고 나머지 내용은 유지합니다.
- **로컬 수정 보호:** 관리 블록이 직접 수정되면 덮어쓰지 않고 충돌로 알립니다.
- **전파 방식 선택:** 주기 실행, 수동 요청, 일시 중지, 기기별 등록·인증 폐기를 지원합니다.

Go 실행 파일 하나에 웹 UI와 SQLite가 포함됩니다. Node 서버나 서비스 계정이 필요하지 않으며, Agent는 수신 포트를 열지 않습니다. Git과 함께 사용할 수 있고, Syncthing 포크나 범용 폴더 동기화 도구는 아닙니다.

```text
브라우저 → Hub: 규칙 편집 → v4 게시
             │
             ├→ 노트북 Agent → AGENTS.md  → v4 파일 설치 확인
             └→ 데스크톱 Agent → CLAUDE.md → 로컬 수정 충돌

                         앱 로드·AI 행동 → 미확인
```

## 내 컴퓨터에서 시작하기

**Go 1.25 이상, Git, macOS 또는 Linux**가 필요합니다. 현재 기본 설치 방법은 소스 빌드입니다. 릴리스 자동화는 포함되어 있지만, 실행 파일 배포 여부는 저장소의 [Releases](https://github.com/pilgrim13/rulesend/releases)에서 확인해야 합니다. Windows는 현재 지원 대상이 아닙니다.

### 1. 빌드하고 Hub 실행

```sh
git clone https://github.com/pilgrim13/rulesend.git
cd rulesend
go test ./...
go build -trimpath -o rulesend ./cmd/rulesend
./rulesend hub init --data "$HOME/.local/share/rulesend-hub"
./rulesend hub serve --data "$HOME/.local/share/rulesend-hub"
```

브라우저에서 **http://127.0.0.1:8787**을 엽니다. 초기화 명령은 관리자 토큰의 **파일 위치만** 출력합니다. 사용자가 해당 파일을 로컬에서 열어 로그인 화면에 직접 입력합니다. 토큰을 AI 대화·이슈·스크린샷·명령 인수에 넣지 마세요.

규칙 팩을 만들고 짧은 가상 예제 규칙을 작성한 뒤 초안을 저장하고 **Publish**로 게시합니다. 비밀번호, 회사 자료, 기기별 사적 경로는 공통 규칙에 넣지 마세요.

### 2. 기기 등록

Hub의 **Devices → Add device**에서 허용할 팩을 선택하고 일회용 등록 토큰을 만듭니다. 수신 기기에도 위의 소스 빌드를 진행하되, Agent만 실행할 기기라면 Hub 초기화·실행은 생략합니다.

수신 기기에 토큰을 보관할 비공개 폴더를 먼저 만듭니다.

```sh
mkdir -p "$HOME/.local/share/rulesend-secrets"
chmod 700 "$HOME/.local/share/rulesend-secrets"
```

사용자가 로컬 편집기로 그 폴더의 `enrollment-token` 파일에 토큰을 저장한 다음 실행합니다. 토큰 내용을 AI에게 보여주거나 명령줄에 직접 입력할 필요가 없습니다.

```sh
chmod 600 "$HOME/.local/share/rulesend-secrets/enrollment-token"
./rulesend agent enroll \
  --data "$HOME/.local/share/rulesend-agent" \
  --hub http://127.0.0.1:8787 \
  --name "My laptop" \
  --token-file "$HOME/.local/share/rulesend-secrets/enrollment-token"
```

**위 주소는 Hub와 Agent가 같은 기기에 있을 때만 사용합니다. 다른 물리 기기에서는 반드시 Hub의 HTTPS 주소로 바꾸세요.** `127.0.0.1`과 `localhost`는 언제나 명령을 실행하는 기기 자신입니다. 원격 구성은 [운영 안내](docs/OPERATIONS.md)를 따릅니다.

기기마다 별도의 등록 토큰이 필요합니다. 등록 성공 후 임시 토큰 파일은 삭제할 수 있습니다. `--token-file -`는 표준 입력도 지원합니다.

### 3. 적용할 파일을 직접 지정

처음에는 테스트 폴더를 사용합니다. 아래 `PACK_ID`를 Hub에서 복사한 실제 팩 ID로 바꿉니다.

```sh
mkdir -p "$HOME/rulesend-demo"

./rulesend agent bind \
  --data "$HOME/.local/share/rulesend-agent" \
  --pack PACK_ID --adapter codex \
  --path "$HOME/rulesend-demo/AGENTS.md" \
  --label "Test project Codex"

./rulesend agent bind \
  --data "$HOME/.local/share/rulesend-agent" \
  --pack PACK_ID --adapter claude \
  --path "$HOME/rulesend-demo/CLAUDE.md" \
  --label "Test project Claude"

./rulesend agent once --data "$HOME/.local/share/rulesend-agent"
./rulesend agent status --data "$HOME/.local/share/rulesend-agent"
```

`bind`는 적용 대상을 등록하고 요약을 출력합니다. 그때는 대상 파일을 바꾸지 않으며, 전체 내용의 전후 비교 화면도 제공하지 않습니다. `once`가 게시본을 설치하고 다시 읽어 확인합니다. 파일 하나에는 팩 하나만 연결할 수 있고, 같은 팩은 여러 파일에 적용할 수 있습니다.

전역 규칙을 명시적으로 선택하려면 `--path` 대신 `--default`를 사용합니다. 이 옵션은 Codex의 `~/.codex/AGENTS.md`, Claude Code의 `~/.claude/CLAUDE.md`를 선택합니다. 상위 폴더는 미리 있어야 하며, 앱이 다른 설정 경로를 사용한다면 `--path`로 지정합니다. 모든 프로젝트를 자동 탐색하거나 다른 도구의 관리 구간을 인수하지 않습니다.

### 4. 계속 실행

```sh
./rulesend agent run --data "$HOME/.local/share/rulesend-agent"
```

Agent는 약 15초마다 연결과 정책을 확인합니다. 팩별 적용 주기는 15초~24시간이며 수동 모드와 일시 중지도 지원합니다. **Sync now**는 다음 연결에서 처리를 요청하지만 일시 중지를 해제하지 않습니다. `once`도 일시 중지를 따릅니다.

수동 팩의 최초 설치에는 `once`를 쓰거나 대상 등록 이후 Sync now를 요청합니다. 대상 등록은 Hub에 연결해 현재 요청 번호를 저장하므로, 등록 직후에 보낸 새 요청도 처리합니다.

다른 변경 명령을 실행하기 전에는 `run`을 중지하세요. `status`는 실행 중에도 마지막 저장 상태를 읽을 수 있습니다. 로그인 후 자동 시작 설정은 [운영 안내](docs/OPERATIONS.md)를 참고하세요.

## 상태를 읽는 방법

| 표시 | 확인한 범위 |
| --- | --- |
| 게시 v4 | Hub에 v4가 변경 불가 버전으로 저장됨 |
| 파일 설치 확인 | Agent가 로컬 파일을 다시 읽어 버전·해시를 확인함 |
| 마지막 연결 | Agent가 마지막으로 Hub에 인증해 접속한 시각 |
| 오래됨·오프라인 | 과거 확인 기록만 있으며 현재 파일 상태는 모름 |
| 충돌 | 로컬 관리 블록의 수정·삭제·표식 이상으로 적용을 멈춤 |
| 앱 로드·행동 | Alpha에서는 항상 미확인 |

Claude **Code**의 로컬 규칙 파일을 대상으로 합니다. Claude 일반 채팅·Projects·Cowork는 자동 설정하지 않습니다. 기존 대화를 자동 갱신하지 않으며, Codex의 모든 규칙 탐색 설정이나 다른 위치에서 상속된 규칙까지 검증하지 않습니다.

## 수정·충돌·되돌리기

Hub에서 초안을 저장한 뒤 게시합니다. 다른 편집이 먼저 저장됐다면 오래된 기준의 저장은 거절됩니다. 과거 버전은 **초안으로 복원**한 후 검토·게시하여 새 버전으로 배포합니다.

로컬 관리 블록을 수정해도 내용이 Hub로 자동 업로드되지 않습니다. 충돌 시 로컬 변경을 별도로 보존하고, `agent status`가 표시하는 기존 버전과 비교하세요. 여러 기기에 반영할 변경은 Hub 초안에 옮겨 게시합니다. 재시도 전 로컬 블록을 확인된 기존 상태로 복구하는 과정이 필요할 수 있습니다. **Alpha에는 강제 덮어쓰기나 자동 충돌 병합 기능이 없습니다.**

Agent는 교체 전 전체 파일 백업과 복구 기록을 비공개 데이터 폴더에 남기며 백업을 자동 정리하지 않습니다. 오래된 전체 파일로 복원하면 그 이후의 사용자 변경이 사라질 수 있으므로 반드시 비교해야 합니다. Agent 상태를 삭제하면 기존 블록을 인식하지 못할 수 있습니다. [복구 절차](docs/OPERATIONS.md#recovery)를 확인하세요.

삭제 전파·연결 해제 명령도 아직 없습니다. Agent 중지, 팩 일시 중지, 기기 인증 폐기를 사용하면 이후 처리를 멈출 수 있으며, 설치된 파일은 그대로 남습니다.

## 운영과 보안

- 기본 Hub는 현재 기기에서만 접속할 수 있습니다. 원격에는 검증된 HTTPS 또는 올바른 HTTPS 역방향 프록시가 필요합니다.
- Hub 운영자는 규칙 원문을 볼 수 있습니다. Hub 운영자에 대한 종단 간 암호화는 제공하지 않습니다.
- 기기는 등록 범위의 팩만 받습니다. Hub는 기기의 새 경로를 허용하거나 셸 명령을 실행할 수 없습니다.
- 실행 데이터·토큰·백업은 저장소 밖의 비공개 폴더에 둡니다.
- 관리자 1명, Agent당 대상 100개, 팩 본문 최대 128 KiB를 지원합니다. 적용 후 **전체 파일**은 Codex 32 KiB, Claude Code·일반 Markdown 128 KiB까지입니다.
- 파일 권한 비트는 유지하지만 ACL·확장 속성·특수 파일·심볼릭 링크는 지원하지 않습니다. 별도 편집기의 모든 동시 쓰기에 대해 무손실을 보장하지 않습니다.

## AI에게 설치 맡기기

아래 내용을 Claude Code나 Codex에 전달할 수 있습니다. 비밀 입력은 사용자가 로컬에서 직접 하고, 테스트 대상부터 확인하도록 구성했습니다.

```text
https://github.com/pilgrim13/rulesend 의 RuleSend를 설치해 주세요.

1. 실제 기기, 운영체제, 현재 폴더, 저장소 소유자 pilgrim13, 빌드할 버전 또는
   커밋을 확인하세요. README.ko.md, docs/CLI.md, docs/OPERATIONS.md를 읽으세요.
2. 이 기기가 Hub, Agent, 둘 다 중 무엇을 맡는지 먼저 정리하세요.
   Hub 주소나 적용 대상이 불명확하면 꼭 필요한 정보만 물어보세요.
3. 소스와 설치 명령, 지원 Go 버전을 확인한 뒤 테스트하고 빌드하세요.
   검토하지 않은 curl-to-shell 설치 스크립트는 실행하지 마세요.
4. 실행 데이터는 저장소 밖의 사용자 전용 폴더에 보관하세요.
   관리자·기기·등록 토큰을 AI가 읽거나 출력·업로드·커밋하지 마세요.
   로그인과 토큰 파일 저장은 제가 로컬에서 직접 하도록 안내하세요.
   등록 프로그램에는 파일 경로만 전달하고 토큰 본문을 대화에 가져오지 마세요.
5. 가상 예제 규칙과 제가 지정한 테스트 AGENTS.md/CLAUDE.md부터 시작하세요.
   기존 내용과 다른 관리 블록을 보존하고, 전역 설정 폴더나 회사 자료를
   통째로 가져오지 마세요.
6. 한 버전을 게시·설치하고 실제 파일과 Hub 확인 응답을 대조하세요.
   테스트 파일의 로컬 수정 충돌을 확인하고 백업·되돌리기 방법을 안내하세요.
   Alpha에는 강제 덮어쓰기 기능이 없다는 점을 반영하세요.
7. 전역 경로 또는 자동 시작 설정 전에 정확한 대상과 변경 내용을 보여 주세요.
   다른 물리 기기의 Hub에는 HTTPS 주소를 사용하세요. 127.0.0.1/localhost를
   원격 주소로 사용하거나 TLS 인증서 검증을 우회하지 마세요.
8. Hub 설정, 기기별 파일 설치, 앱 로드, AI 행동을 각각 보고하세요.
   확인하지 않은 단계는 미확인으로 남기세요.
```

## 개발·검증

```sh
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/rulesend
```

테스트 범위에는 인증·게시·동시 편집·등록, 권한 범위·인증 폐기, 로컬 Agent 두 개, 기존 내용 보존, 충돌, 일정 정책, 파일 경로 보호와 중단 복구가 포함됩니다. **한 기기에서 Agent 두 개를 시험한 것은 물리 기기 두 대의 전파 실증과 다릅니다.** 실제 검증 환경은 [검증 기록](docs/VALIDATION.md)을 참고하세요.

[기여 안내](CONTRIBUTING.md) · [보안 신고](SECURITY.md) · [계획·수용 기준](docs/PRODUCT_PLAN.ko.md)

## 라이선스

독자 구현 코드는 [MIT 라이선스](LICENSE)로 공개합니다. 의존성 고지는 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)에 있습니다. Syncthing 소스는 포함하지 않습니다.
