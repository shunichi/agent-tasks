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

**並行開発**: 別々のエージェントセッションでそれぞれ別タスクを `start` すると、
タスクごとに git worktree + ブランチが切られ、衝突なく同時に開発できる。
`spawn` は tmux の別 pane を開き、その worktree で新しいセッションを起動して着手まで通す
(着手・session URL 記録は子セッションが担当)。tmux 外では貼り付け用コマンドを表示するだけ。

### 閲覧 (ターミナルから)

```sh
agent-tasks                      # 未完了タスク一覧 (既定。done は非表示)
agent-tasks --all                # done も含めて全件表示 (-a も可)
agent-tasks --status in-progress # status で絞り込み
agent-tasks --project family-app2
agent-tasks show family-app2 0001 # 1 タスクの全文
agent-tasks edit                 # ストアをエディタで開く (既定 code)
agent-tasks edit family-app2 0001 # 1 タスクをエディタで開く
agent-tasks where                # データディレクトリのパス
```

`edit` のエディタは `AGENT_TASKS_EDITOR` > `VISUAL` > `EDITOR` の順、未設定なら `code`。

## 今後

- Web ダッシュボード化 (このリポジトリ内で発展させる)
- blocked タスクの理由・経過時間の可視化など
