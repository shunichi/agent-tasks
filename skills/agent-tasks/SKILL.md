---
name: agent-tasks
description: "エージェント開発タスクをリポジトリ外の中央ストア (~/agent-tasks-store) で管理する skill。タスクの登録・一覧・着手 (git worktree で並行)・完了・保留を行う。トリガー: 'タスクを作る/登録', 'タスク一覧', 'タスクに着手', 'タスクを完了', '/agent-tasks create|list|start|done|block' など。"
---

# agent-tasks skill

エージェント (Claude / Codex / ...) に開発させるタスクを、**各コードリポジトリの外**にある
中央ストア `~/agent-tasks-store/` で管理する。リポジトリ内に置かないのは、ブランチごとに
タスク状態がずれるのを避けるため。skill と閲覧用 CLI `agent-tasks` は repo `agent-tasks` に同梱。

## 共通ルール

### データの場所

- ストアのルートは環境変数 `AGENT_TASKS_STORE`、未設定なら `~/agent-tasks-store`。
- タスクは `<root>/<project>/<NNNN>-<slug>.md`。
- **絶対にコードリポジトリの中に書かない。** 必ず上記ストアの下に書く。

### project キーの決め方

作業中のコードリポジトリの root の basename:

```sh
basename "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
```

### タスクファイルの形式

```markdown
---
id: "0001"
project: family-app2
title: ブックマークのドラッグ並び替え
status: todo          # todo | in-progress | blocked | review | done
agent:                # 着手したエージェント (claude / codex / ...)
session:              # エージェントのセッション URL
branch: task/0001-bookmark-dnd
worktree: ../family-app2--0001
created: 2026-06-28
updated: 2026-06-28
---

# 要件
（タスク内容）

## 進捗ログ
- 2026-06-28 登録
```

- `id` は project ごとの連番 (4桁ゼロ埋め)。既存ファイルの最大 + 1。
- `slug` は内容を表す英語ケバブケース。
- `status` を更新したら必ず `updated` を当日の日付に変え、`## 進捗ログ` に 1 行追記する。
- 日付は `date +%F` で取得する (推測しない)。

## 操作の判定

ユーザーの発言から操作を判定する。引数 (`create`/`list`/`start`/`done`/`block`) があればそれに従う。

- **create**: 「タスクを作る/追加/登録」「〜というタスク」
- **list**: 「タスク一覧」「タスクの進捗」「何が残ってる」
- **start**: 「〜に着手」「タスク 0001 をやって」「start 0001」
- **done**: 「〜が完了」「done 0001」
- **block**: 「〜を保留」「block 0001」

判断できなければユーザーに確認する。

---

## create — タスク登録

1. project キーを決める (上記)。
2. `<root>/<project>/` がなければ作成する。
3. 既存 `<root>/<project>/*.md` の最大連番 + 1 で `id` を決める (なければ `0001`)。
4. `slug` を決める。ユーザー指定があれば英語ケバブケースに変換、なければ確認する。
5. `<root>/<project>/<NNNN>-<slug>.md` を上記形式で作成する。`status: todo`、`agent`/`session`/`branch`/`worktree` は空、`created`/`updated` は当日。
6. 作成したパスを報告する。**コードリポジトリには一切コミットしない。**

---

## list — 一覧

1. `agent-tasks` コマンドがあればそれを使う (`command -v agent-tasks`):
   - 全件: `agent-tasks`
   - 未完了: `agent-tasks --active`
   - 当 project のみ: `agent-tasks --project <project>`
2. コマンドが無ければ `<root>/**/*.md` の frontmatter を読み、project / id / status / title を表にして表示する。

---

## start — 着手 (git worktree で並行開発)

並行実行の肝。**別々のエージェントセッションがそれぞれ別タスクを start することで同時開発できる。**

1. 対象タスクファイルを特定する (project + id)。
2. **二重着手チェック**: `status` が `in-progress` で `session` が埋まっている場合、別セッションが作業中の可能性。ユーザーに確認し、明示的な指示がなければ中止する。
3. worktree とブランチを作る。コードリポジトリの root で:
   ```sh
   git worktree add ../<project>--<NNNN> -b task/<NNNN>-<slug>
   ```
   - 既定ブランチ (main) の最新から分岐する。必要なら事前に `git fetch` / 最新化する。
4. タスクファイルの frontmatter を更新する:
   - `status: in-progress`
   - `agent: claude` (自分のエージェント名)
   - `session:` 自分のセッション URL が分かれば記録 (Claude Code なら会話フッタの `Claude-Session` URL)
   - `branch: task/<NNNN>-<slug>` / `worktree: ../<project>--<NNNN>`
   - `updated:` 当日、`## 進捗ログ` に「着手」を追記
5. **以降の実装作業は作成した worktree (`../<project>--<NNNN>`) の中で行う。** 元のチェックアウトは汚さない。
6. プロジェクトの作法に従って実装する (`CLAUDE.md` / `AGENTS.md` を読む)。完了に近づいたら `done` へ。

---

## done — 完了

1. 対象タスクファイルを特定する。
2. worktree 内で最終確認 (型/Lint/テストなど、プロジェクトの作法に従う)。
3. 変更をコミットする (worktree 内で。コミット先はコードリポジトリ)。
4. ユーザーが PR を望めば作成する (`gh pr create`)。PR 待ちの段階なら `status: review`、マージまで完了したら `status: done`。
5. タスクファイルの frontmatter を更新: `status` を `review` または `done`、`updated` を当日、`## 進捗ログ` に対応内容を追記。
6. `done` まで来たら worktree を撤去する:
   ```sh
   git worktree remove ../<project>--<NNNN>
   ```
   未コミットがある場合は撤去せず、ユーザーに知らせる。

---

## block — 保留

1. 対象タスクファイルを特定する。
2. `status: blocked` に更新し、`updated` を当日に、`## 進捗ログ` に**保留理由** (何の判断/確認待ちか) を追記する。
3. worktree は残す (再開できるように)。判断材料が揃ったら `start` で再開、または直接実装を続ける。
