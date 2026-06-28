# agent-tasks

エージェント (Claude / Codex / ...) に開発させるタスクを管理するための一式。
**操作 (skill)** と **閲覧 (CLI)** を1リポジトリにまとめている。

タスクデータは各コードリポジトリの**外**、`~/agent-tasks-store/` に置く
(repo `agent-tasks` = ツール、`agent-tasks-store` = タスクの中身、という役割分担)。
リポジトリ内に置かないのは、ブランチごとにタスク状態がずれるのを避けるため。

## 構成

```
agent-tasks/
  skills/agent-tasks/SKILL.md   # 操作 (agent 用): /agent-tasks。登録/一覧/着手/spawn/完了/保留
  main.go store.go render.go    # 閲覧 (人間用) CLI 本体 (Go, 依存ゼロ)
  store_test.go                 # テスト
  Makefile                      # build / install / test
  bin/agent-tasks               # ビルド成果物 (gitignore)
```

- 閲覧 CLI は Go 製。機能追加していく前提で、frontmatter パース (`store.go`) /
  表示 (`render.go`) / コマンド振り分け (`main.go`) に分けている。
- `skills/` 直下に置くのは、Claude 以外のエージェント (Codex / Cursor / Gemini) からも
  標準位置として見つけやすくするため。

## データの場所

```
~/agent-tasks-store/
  <project>/            # コードリポジトリ root の basename
    <NNNN>-<slug>.md    # 1 タスク = 1 Markdown ファイル
```

`AGENT_TASKS_STORE` で場所を変更可。データ形式の詳細は `~/agent-tasks-store/README.md` を参照。

## インストール

`make install` で CLI をビルドし、CLI (`~/.local/bin`) と skill (`~/.claude/skills`)
を symlink する (`~/.local/bin` が PATH にある前提):

```sh
make install
```

中身は次と等価:

```sh
go build -o bin/agent-tasks .                            # CLI をビルド
ln -sf  "$(pwd)/bin/agent-tasks"    ~/.local/bin/agent-tasks
ln -sfn "$(pwd)/skills/agent-tasks" ~/.claude/skills/agent-tasks
```

- skill は symlink なので編集すれば即反映。
- CLI は Go バイナリなので、ソースを変えたら `make build` で再ビルドする
  (symlink 先のバイナリが更新される)。

## 使い方

### 操作 (エージェントに対して)

| 操作 | 言い方の例 |
| --- | --- |
| 登録 | 「〜というタスクを作って」「/agent-tasks create」 |
| 一覧 | 「タスク一覧」「/agent-tasks list」 |
| 着手 | 「タスク 0001 に着手」「/agent-tasks start 0001」(git worktree で並行開発) |
| 別 pane で着手 | 「別 pane で 0001 をやって」「/agent-tasks spawn 0001」(tmux の別 pane に新セッション) |
| 完了 | 「0001 を完了」「/agent-tasks done 0001」 |
| 保留 | 「0001 を保留」「/agent-tasks block 0001」 |
| worktree 設定の展開 | 「worktree 設定を入れて」「/agent-tasks scaffold」(firebase/rails を検出して雛形生成) |

**並行開発**: 別々のエージェントセッションでそれぞれ別タスクを `start` すると、
タスクごとに git worktree + ブランチが切られ、衝突なく同時に開発できる。
`spawn` は tmux の別 pane を開き、その worktree で新しいセッションを起動して着手まで通す
(着手・session URL 記録は子セッションが担当)。tmux 外では貼り付け用コマンドを表示するだけ。

**worktree 作成後フック**: start/spawn は worktree を作ったあと `agent-tasks worktree-init` を呼び、
環境の干渉 (worktree に `.env` も `node_modules` も無い問題) を解消する。コードリポジトリ root に
置いた 2 ファイルを参照する (両方任意・無ければ no-op):

- `.worktreeinclude` — コピーする gitignored ファイル (`.gitignore` 構文。Claude Code の `--worktree` と
  同名・互換)。tracked ファイルは複製しない。
- `.worktree-post-create` — worktree 内で実行するスクリプト (依存インストール / ポート分離 / DB 準備など)。
  `AGENT_TASKS_WORKTREE` / `AGENT_TASKS_MAIN` / `AGENT_TASKS_PROJECT` が渡る。

コピーは既存を上書きせず、post-create は worktree ごとに一度だけ実行する (冪等。`--force` で再実行)。

この 2 ファイルの雛形は **`agent-tasks scaffold-worktree [stack]`** でスタック別に生成できる
(firebase / rails 同梱。stack 省略で自動検出、`--list` で一覧)。テンプレはバイナリに同梱しており、
スタックを増やすには `templates/<stack>/{worktreeinclude,post-create}` を足すだけ。例えば firebase なら
emulator ポートを worktree ごとに一意化する post-create が入る。

### 閲覧 (ターミナルから)

```sh
agent-tasks                      # 現在 project の未完了タスク一覧 (既定。done は非表示)
agent-tasks --all-projects       # 全 project を横断して一覧
agent-tasks --all                # done も含めて表示 (-a も可)
agent-tasks --status in-progress # status で絞り込み (既定どおり現在 project に絞られる)
agent-tasks --project webapp     # 別 project を指定
agent-tasks show webapp 0001     # 1 タスクの全文
agent-tasks show 0001            # project 省略時は現在 project のタスク
agent-tasks edit                 # ストアをエディタで開く (既定 code)
agent-tasks edit webapp 0001     # 1 タスクをエディタで開く
agent-tasks edit 0001            # project 省略時は現在 project のタスク
agent-tasks show webapp 1        # ID は短縮形でも可 (1 -> 0001)
agent-tasks sync                 # ストアを add/commit/push して同期
agent-tasks sync --no-push       # commit まで (push しない)
agent-tasks worktree-init ../foo--0001 # worktree 作成後フック (start/spawn が自動で呼ぶ)
agent-tasks scaffold-worktree    # worktree 設定の雛形を生成 (stack 自動検出。--list/--dir/--force)
agent-tasks where                # データディレクトリのパス
```

既定では**現在のコードリポジトリ (project) のタスクだけ**を表示する。横断したいときは
`--all-projects`、別 project を見たいときは `--project <name>` を使う。現在 project は cwd の
git リポジトリから判定し (リンク worktree 内でもメイン repo 名に解決する)、git 外なら自動で横断にフォールバックする。

`show` / `edit` は `<project>` を省略すると現在 project のタスクとして解決する (`show 0001`)。
git 外などで現在 project を判定できないときは `<project> <id>` の明示指定を促す。
`<id>` は数値なら4桁ゼロ埋めに正規化して照合する。`1` でも `0001` でも同じタスクを指せる。

`sync` は `~/agent-tasks-store` (git repo) を `add -A` → コミット → `pull --rebase` → `push` する。
コミットメッセージは変更されたタスクから自動生成する (例: `tasks: agent-tasks/0005 (in-progress)`)。
リモート未設定なら push をスキップ、upstream 未設定なら初回 `push -u` で追跡を設定する。

`edit` のエディタは `AGENT_TASKS_EDITOR` > `VISUAL` > `EDITOR` の順、未設定なら `code`。

### 色出力

既定 (`--color=auto`) は stdout が端末のときだけ色を付ける。`watch` などパイプ経由では
端末でなくなるため色が消える。色を制御したいときは:

```sh
agent-tasks --color=always …   # 端末でなくても色を出す (パイプ向け)
agent-tasks --color=never …    # 常に無色
watch --color agent-tasks --color=always   # watch で色付き監視
```

環境変数も尊重する: `NO_COLOR` (非空) で無効化、`FORCE_COLOR` (非空) で強制。
優先順位は `--color` フラグ > `NO_COLOR` > `FORCE_COLOR` > 端末判定。

## 今後

- Web ダッシュボード化 (このリポジトリ内で発展させる)
- blocked タスクの理由・経過時間の可視化など
