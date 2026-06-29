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
  skills/agent-tasks/SKILL.md   # 操作 (agent 用): /agent-tasks。登録/一覧/おすすめ/着手/spawn/batch/完了/保留
  main.go                       # 閲覧 CLI: コマンド振り分け + list/show/edit/sync/doctor/where
  store.go                      # タスクの model + ストア走査 + frontmatter パース + doctor 集計
  json.go                       # list/show の --json 出力 (機械可読。計算済み session_state/blocked_for を含む)
  recent.go                     # list --recent: 最近完了タスク (completed_at 降順 N 件) の選択 + 描画
  version.go                     # version: ビルド埋め込みの VCS 情報 (commit+CalVer) を表示 (手動 bump なし)
  render.go                     # 色付け + CJK 幅対応のテーブル描画
  worktree.go                   # worktree-init: 作成後フック (.worktreeinclude コピー + post-create 実行)
  scaffold.go                   # scaffold-worktree: スタック別 worktree 設定の雛形展開 (templates を embed)
  session.go                    # session-hook + session-link + list の SESSION 列 (working/waiting/ended)
  statusline.go                 # statusline: Claude Code の status line に実行中タスクを 1 行表示
  completion.go                 # completion: bash/zsh の補完スクリプト生成 (静的) + completion-values (動的候補 project/id)
  blocked.go                    # list の BLOCKED 列: 保留からの経過 + 理由 (blocked_at/blocked_reason)
  datetime.go                   # 時刻系の共通ヘルパ (ISO8601 パース/日付表示 displayDate/経過整形)
  timestamps.go                 # started_at/completed_at: show の所要時間サマリ + doctor 整合チェック
  prs.go                        # prs: PR URL リスト: show の PR 一覧サマリ + doctor の URL 形式チェック
  templates/<stack>/            # firebase/rails の worktreeinclude + post-create (バイナリに同梱)
  *_test.go                     # テスト (store/worktree/scaffold/session/blocked/datetime/timestamps/completion)
  Makefile                      # build / install / link / install-completions / test / fmt / vet
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

## CHANGELOG

利用者目線の変更 (機能追加・破壊的変更・影響のある修正) を伴う変更は、`CHANGELOG.md` の
**main マージ日の日付セクション (`## YYYY-MM-DD`)** に 1 行追記する (タグ運用なし。内部リファクタの
細かい話は不要)。`## [Unreleased]` は使わない。

- 日付は **main へマージした日** (ISO `YYYY-MM-DD`)。**マージは Claude Code が行うので、この追記も
  マージ時 (done フローの一部) に Claude Code が行う**。
- その日の日付セクションが無ければ、新しい日付を**一番上**に作る (新しい順)。同じ日に複数入れるときは
  同じセクションの Added/Changed/Fixed に足す。
- 実装中 (PR 作成時) に書いてよい。当日マージ前提なのでその日の日付でよい (日跨ぎは稀)。
- ビルドの版は commit ベース (`agent-tasks version` の commit + CalVer)。CHANGELOG=「いつ何が」、
  version=「どの commit 時点か」の補完関係。

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

## 機密情報をコミットしない

開発中に知り得た情報のうち、**このツール repo の動作に本質的でないものはコミットしない**。
特に次は、コード・コメント・ドキュメント・テストデータ・コミットメッセージのいずれにも含めない:

- **実在のプロジェクト名 / リポジトリ名 / 顧客名・会社名** (例として挙げる場合は `webapp` / `rails-app` の
  ような汎用の仮名にする)。
- **顧客情報・個人情報** (氏名・メール・電話・住所など)、認証情報 (トークン・パスワード・接続文字列)。
- **社内固有の URL / ホスト名 / パス**で、機能説明に不要なもの。

実運用での気づきを元に機能を改善する場合も、**仮名・一般化した記述に置き換えてから**書く
(具体例が要るときは `firebase` / `rails` のようなスタック名や汎用名で示す)。
実際のタスク内容や実プロジェクト固有のメモは、この repo ではなく `~/agent-tasks-store`
(コードリポジトリの外) 側に置く。
