# agent-dashboard

エージェント (Claude / Codex / ...) に開発させるタスクを管理するための一式。
**操作 (skill)** と **閲覧 (CLI)** を1リポジトリにまとめている。

タスクデータは各コードリポジトリの**外**、`~/agent-tasks/` に置く。リポジトリ内に置かないのは、
ブランチごとにタスク状態がずれるのを避けるため。

## 構成

```
agent-dashboard/
  skills/agent-tasks/SKILL.md   # 操作 (agent 用): /agent-tasks。登録/一覧/着手/完了/保留
  bin/agent-tasks               # 閲覧 (人間用): ~/agent-tasks を横断する CLI
```

- `skills/` 直下に置くのは、Claude 以外のエージェント (Codex / Cursor / Gemini) からも
  標準位置として見つけやすくするため。

## データの場所

```
~/agent-tasks/
  <project>/            # コードリポジトリ root の basename
    <NNN>-<slug>.md     # 1 タスク = 1 Markdown ファイル
```

`AGENT_TASKS_DIR` で場所を変更可。データ形式の詳細は `~/agent-tasks/README.md` を参照。

## インストール

このリポジトリを単一の source として、skill と CLI をそれぞれ symlink する
(`~/.local/bin` が PATH にある前提):

```sh
# skill を Claude Code に認識させる
ln -sfn "$(pwd)/skills/agent-tasks" ~/.claude/skills/agent-tasks
# CLI を PATH に通す
ln -sf  "$(pwd)/bin/agent-tasks"    ~/.local/bin/agent-tasks
```

symlink なので、このリポジトリを編集すれば即反映される。

## 使い方

### 操作 (エージェントに対して)

| 操作 | 言い方の例 |
| --- | --- |
| 登録 | 「〜というタスクを作って」「/agent-tasks create」 |
| 一覧 | 「タスク一覧」「/agent-tasks list」 |
| 着手 | 「タスク 001 に着手」「/agent-tasks start 001」(git worktree で並行開発) |
| 完了 | 「001 を完了」「/agent-tasks done 001」 |
| 保留 | 「001 を保留」「/agent-tasks block 001」 |

**並行開発**: 別々のエージェントセッションでそれぞれ別タスクを `start` すると、
タスクごとに git worktree + ブランチが切られ、衝突なく同時に開発できる。

### 閲覧 (ターミナルから)

```sh
agent-tasks                      # 全タスク一覧
agent-tasks --active             # 未完了のみ (done 以外)
agent-tasks --status in-progress # status で絞り込み
agent-tasks --project family-app2
agent-tasks show family-app2 001 # 1 タスクの全文
agent-tasks where                # データディレクトリのパス
```

## 今後

- Web ダッシュボード化 (このリポジトリ内で発展させる)
- blocked タスクの理由・経過時間の可視化など
