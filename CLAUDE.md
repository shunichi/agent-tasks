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
  tui.go                        # tui: 一覧+詳細のインタラクティブ閲覧 (Bubble Tea。mtime ポーリングで自動更新)
  version.go                     # version: ビルド埋め込みの VCS 情報 (commit+CalVer) を表示 (手動 bump なし)
  render.go                     # 色付け + CJK 幅対応のテーブル描画
  worktree.go                   # worktree-init: 作成後フック (.worktreeinclude コピー + post-create 実行) / worktree-remove: 撤去フック (post-remove 実行 + git worktree remove)
  scaffold.go                   # scaffold-worktree: スタック別 worktree 設定の雛形展開 (templates を embed)
  session.go                    # session-hook + session-link + list の SESSION 列 (working/waiting/ended)
  session_rename.go             # session-rename: 着手時に自 pane へ /rename を send-keys しセッション名をタスク名に
  statusline.go                 # statusline: Claude Code の status line に実行中タスクを 1 行表示
  completion.go                 # completion: bash/zsh の補完スクリプト生成 (静的) + completion-values (動的候補 project/id)
  blocked.go                    # list の BLOCKED 列: 保留からの経過 + 理由 (blocked_at/blocked_reason)
  datetime.go                   # 時刻系の共通ヘルパ (ISO8601 パース/日付表示 displayDate/経過整形)
  timestamps.go                 # started_at/completed_at: show の所要時間サマリ + doctor 整合チェック
  prs.go                        # prs: PR URL リスト: show の PR 一覧サマリ + doctor の URL 形式チェック
  templates/<stack>/            # firebase/rails の worktreeinclude + post-create + post-remove (バイナリに同梱)
  *_test.go                     # テスト (store/worktree/scaffold/session/blocked/datetime/timestamps/completion)
  Makefile                      # build / install / link / install-completions / test / fmt / vet
  bin/agent-tasks               # ビルド成果物 (gitignore)

~/agent-tasks-store/            ← データ (このリポジトリの外)
  <project>/<NNNN>-<slug>.md    # 1 タスク = 1 ファイル。project = コード repo root の basename
```

## ツール / コマンド

- `make build` — `go build -o bin/agent-tasks .`
- `make install` — build + symlink (CLI を `~/.local/bin`、skill を `~/.claude/skills` へ) + 補完再生成
  (`install-completions` を含む。補完は静的ファイルなので、機能追加後は `make install` で最新化する)
- `make test` / `make fmt` / `make vet`
- インストール済み symlink (この環境では設定済み):
  - `~/.claude/skills/agent-tasks` → `skills/agent-tasks`
  - `~/.local/bin/agent-tasks` → `bin/agent-tasks` (Go バイナリ。**ソース変更後は `make build` が必要**)

## CHANGELOG

利用者目線の変更 (機能追加・破壊的変更・影響のある修正) を伴う変更は、`CHANGELOG.md` に記録する
(タグ運用なし。内部リファクタの細かい話は不要)。**日付はマージ時に確定**する:

- **実装中**は `## [Unreleased]` に 1 行足す (日付なし。マージ日が未確定のため)。
- **マージ直前にブランチ上で** `## <マージする日付>` (ISO `YYYY-MM-DD`、main マージ日) セクションへ
  移してから merge する。無ければ新しい日付を `## [Unreleased]` の直下に作る (新しい順)。
  **main を直接編集しない** (移動も PR に含める)。マージは Claude Code が行うので、この移動も
  Claude Code がマージ作業の一部として行う。
- 日跨ぎの PR でも日付がマージ日と一致する (実装日ではなく、移動した時=マージ日で確定するため)。
- **並行マージの注意**: CHANGELOG は共有ファイルで衝突しやすい。各 PR が編集をブランチ内に持つことで
  衝突は GitHub のマージ不可検知で止まる (後発 PR を rebase して解消)。main 直接 push のレースはしない。
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
- **worktree 作成後フック / 撤去フック**: start/spawn は worktree 作成後に `agent-tasks worktree-init <dir>`、
  done は撤去時に `agent-tasks worktree-remove <dir>` を呼ぶ。コードリポジトリ root の `.worktreeinclude`
  (gitignored ファイルをコピー。Claude Code 互換) と `.worktree-post-create` (作成後に worktree 内で実行)、
  対の `.worktree-post-remove` (撤去直前に worktree 内で実行し、worktree 固有 DB / puma-dev 登録などを
  後始末) を参照する汎用機構。worktree-remove は cwd が対象 worktree 内なら中止する安全策付き。
  スタック固有 (firebase/rails) の設定**生成**は別タスク (store の agent-tasks/0017) で、本機構は実行のみ。

## Go の方針

- ターゲットは **Go 1.26** (`go.mod` 準拠)。`modern-go-guidelines` プラグインの `use-modern-go` に従う。
- **依存は最小限**。原則は標準ライブラリ (frontmatter パースも自前 = `store.go`) だが、自前で持つと
  保守コストや正確性で不利なもの (例: 端末表示幅 = `github.com/mattn/go-runewidth`) は**少数の確立した
  ライブラリ**を採用してよい。依存を増やすときは「自前維持より明確に得か」を確認し、小さく枯れた
  ライブラリを選ぶ。ビルドが常に通ることを優先。(旧「依存ゼロ」方針から変更。)
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
