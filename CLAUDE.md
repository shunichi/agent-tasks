# CLAUDE.md

## このリポジトリは何か

エージェント (Claude / Codex / ...) に開発させるタスクを管理する一式。
**操作 (skill)** と **閲覧 (CLI)** を1リポジトリにまとめている。
タスクデータ自体は**各コードリポジトリの外** (`~/agent-tasks-store/`) に置く
(ブランチごとに状態がずれるのを避けるため)。

ユーザー向けの使い方は `README.md`、データ形式は `~/agent-tasks-store/README.md` を参照。

## 構成

```
agent-tasks/                    ← このリポジトリ = ツール (操作 skill + 閲覧 CLI)
  skills/agent-tasks/SKILL.md   # 操作 (agent 用): /agent-tasks。登録/一覧/着手/spawn/完了/保留
  main.go                       # 閲覧 CLI: コマンド振り分け + list/show/edit/sync/doctor/where
  store.go                      # タスクの model + ストア走査 + frontmatter パース + doctor 集計
  render.go                     # 色付け + CJK 幅対応のテーブル描画
  worktree.go                   # worktree-init: 作成後フック (.worktreeinclude コピー + post-create 実行)
  scaffold.go                   # scaffold-worktree: スタック別 worktree 設定の雛形展開 (templates を embed)
  session.go                    # session-hook + session-link + list の SESSION 列 (working/waiting/ended)
  blocked.go                    # list の BLOCKED 列: 保留からの経過 + 理由 (blocked_at/blocked_reason)
  datetime.go                   # 時刻系の共通ヘルパ (ISO8601 パース/日付表示 displayDate/経過整形)
  timestamps.go                 # started_at/completed_at: show の所要時間サマリ + doctor 整合チェック
  templates/<stack>/            # firebase/rails の worktreeinclude + post-create (バイナリに同梱)
  *_test.go                     # テスト (store/worktree/scaffold/session/blocked/datetime/timestamps)
  Makefile                      # build / install / link / test / fmt / vet
  bin/agent-tasks               # ビルド成果物 (gitignore)

~/agent-tasks-store/            ← データ (このリポジトリの外)
  <project>/<NNNN>-<slug>.md    # 1 タスク = 1 ファイル。project = コード repo root の basename
```

## ツール / コマンド

- `make build` — `go build -o bin/agent-tasks .`
- `make install` — build + symlink (CLI を `~/.local/bin`、skill を `~/.claude/skills` へ)
- `make test` / `make fmt` / `make vet`
- インストール済み symlink (この環境では設定済み):
  - `~/.claude/skills/agent-tasks` → `skills/agent-tasks`
  - `~/.local/bin/agent-tasks` → `bin/agent-tasks` (Go バイナリ。**ソース変更後は `make build` が必要**)

## 設計上の決めごと (踏襲する)

- **命名は agent 非依存**。`claude-` prefix を使わない。Claude Code 標準の `/tasks` と被らないよう
  skill 名は `agent-tasks`。
- **ツール repo (`agent-tasks`) とデータ (`agent-tasks-store`) は別名**。データ側も将来 git 化したとき
  repo 名が衝突しないように `-store` を付けてある。
- データの場所は環境変数 `AGENT_TASKS_STORE` (既定 `~/agent-tasks-store`)。CLI / skill 双方が参照する。
- タスク frontmatter は `agent:` (claude/codex/...) と `session:` (URL) を分ける。
- **並行開発**: `/agent-tasks start <id>` がタスクごとに
  `git worktree add ../<project>--<NNNN> -b task/<NNNN>-<slug>` を作る。別セッションで start すれば
  衝突なく同時進行。二重着手は status=in-progress + session でガード。
- **worktree 作成後フック**: start/spawn は worktree 作成後に `agent-tasks worktree-init <dir>` を呼ぶ。
  コードリポジトリ root の `.worktreeinclude` (gitignored ファイルをコピー。Claude Code 互換) と
  `.worktree-post-create` (worktree 内で実行するセットアップスクリプト) を参照する汎用機構。
  スタック固有 (firebase/rails) の設定**生成**は別タスク (store の agent-tasks/0017) で、本機構は実行のみ。

## Go の方針

- ターゲットは **Go 1.26** (`go.mod` 準拠)。`modern-go-guidelines` プラグインの `use-modern-go` に従う。
- **依存ゼロ** (標準ライブラリのみ)。frontmatter パースも自前 (`store.go`)。ビルドが常に通ることを優先。
- 採用済みのモダンイディオム例: `slices.SortFunc` + `cmp.Or`/`cmp.Compare`、`strings.Cut`、`strings.Repeat`、
  `t.TempDir()`。`sort.Slice` や手動ループに戻さない。
- 機能追加は `store.go` (データ) / `render.go` (表示) / `main.go` (コマンド) の分担を保つ。
  サブコマンドは `main.go` の `switch` に足す。

## 現状と残タスク (2026-06-28 時点)

- ✅ skill + Go CLI 実装・テスト緑・GitHub に push 済み (`list` / `--status` / `--project` /
  `--active` / `show` / `where`)。
- ✅ `worktree-init` (作成後フック: `.worktreeinclude` コピー + `.worktree-post-create` 実行) を CLI 化し
  start/spawn に組み込み (store の agent-tasks/0014)。
- ✅ `scaffold-worktree` (スタック別 worktree 設定の雛形展開。firebase/rails テンプレを embed) を CLI 化し
  skill の `scaffold` 操作に (store の agent-tasks/0017)。テンプレ追加 = `templates/<stack>/` を足すだけ。
- 💡 未着手の発展案:
  - `~/agent-tasks-store` 自体の git 化 (マシン間同期)。
  - skill 側にある `create` / `start` / `done` / `block` の一部を CLI サブコマンド化するか検討
    (今は手順書として skill が担当)。
  - worktree 設定テンプレの拡充 (next/django/go 等)。
  - Web ダッシュボード化 (このリポジトリ内で発展)。
- ✅ blocked の理由・経過時間の可視化 (`blocked_at`/`blocked_reason` + list の BLOCKED 列。
  store の agent-tasks/0003)。
- ✅ frontmatter の時刻系 (`created`/`updated`/`blocked_at`) を ISO8601 日時に統一
  (`date --iso-8601=seconds`。一覧は日付に丸めて表示、パーサは日付のみの旧データも両対応。
  store の agent-tasks/0021)。
- ✅ 着手/完了日時 (`started_at`/`completed_at`) を記録 (start/done で。show が所要時間/経過を
  サマリ表示、doctor が日時の矛盾を検査。store の agent-tasks/0024)。
- ✅ 同一セッションで start したタスクにも session 状態を紐づけ (`session-link`。hook が session_id
  キーのマーカーも書き、start 時に cwd 逆引きで自セッションを特定して `<wt>.link.json` に記録。
  list は worktree マーカーと link の新しい方を採用。store の agent-tasks/0027)。

## コミットメッセージ

末尾に付ける:
```
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
```
