# Changelog

このプロジェクトの主な変更を記録する。形式は [Keep a Changelog](https://keepachangelog.com/ja/1.1.0/) に倣う。

タグによるバージョニングは行わず、**main にマージした日付の見出し (`## YYYY-MM-DD`)** の下に変更を
記録していく運用。日付は **main へマージした日** (ISO)。機能追加・破壊的変更・利用者に影響する修正が
対象 (内部リファクタの細かい話は不要)。

記入フロー (日跨ぎでも日付が正しくなるよう、**日付はマージ時に確定する**):

1. **実装中**は `## [Unreleased]` に1行追記する (まだ日付を付けない。マージ日が未確定のため)。
2. **マージ直前にブランチ上で**、その変更を `## <マージする日付>` セクションへ移してから merge する
   (無ければ新しい日付を `## [Unreleased]` の直下に作る。新しい順)。**main を直接編集しない**
   — この移動も PR に含めることで、変更は必ず PR 経由で main に入る。マージは Claude Code が
   行うので、この移動も Claude Code がマージ作業の一部として行う。
3. **複数セッションが並行でマージするとき**: CHANGELOG は全員が触る共有ファイルなので衝突しやすい。
   各 PR が自分の CHANGELOG 編集を**ブランチ内に持つ**ことで、衝突は GitHub の「マージ不可」検知で
   止まる (後発 PR を rebase して解消)。main への直接 push レースは起こさない。衝突は通常の
   PR コンフリクト解消 (rebase) で対応する。

ビルドの版はこの CHANGELOG の節ではなく、ビルド元 commit で識別する (`agent-tasks version` が
commit + CalVer を表示)。CHANGELOG は「いつ何が変わったか」、version は「どの commit 時点か」という補完関係。

## [Unreleased]

(マージ待ちの変更をここに置く。マージ時に下の日付セクションへ移す。)

## 2026-06-29

### Added

- `list --recent [N]`: 最近完了したタスク (done かつ `completed_at` あり) を完了日時降順で上位 N 件
  (既定 10) 表示する。`COMPLETED` 列付き。`--all-projects` / `--json` と併用可。
- `version` サブコマンド (`--version` / `-V`): ビルド元の commit SHA + commit 日時 + CalVer を表示する。
  `go build` が埋め込む VCS 情報を実行時に読むので手動 bump 不要 (タグ運用なしの継続的 main 向け)。
- GitHub Actions による CI (`.github/workflows/ci.yml`): `push` (main) と `pull_request` で
  gofmt (未整形チェック) / `go vet` / `go build` / `go test` を実行する。Go バージョンは
  go.mod 連動。README に CI バッジを追加。
- `list --json` / `show --json`: タスクを機械可読な JSON (一覧は配列、show はオブジェクト) で出力する。
  既存フィルタと併用でき、計算済みフィールド (`session_state` / `blocked_for`) を含む。skill/スクリプト向け。
- シェル補完の動的補完: `--project` の値 (ストアの project 一覧) と `show`/`edit`/`session-link` の
  位置引数 `[<project>] <id>` をタブ補完できる (第1引数=project名+現在projectのid、第2引数=その
  projectのid)。zsh では id 候補にタスクのタイトルを併記する。内部コマンド `completion-values` が
  候補を列挙する。
- タスクに PR URL を記録する `prs:` フィールド (YAML リスト。1 タスクに複数 PR 可)。`show` が末尾に
  PR 一覧を表示し、`doctor` が URL 形式を点検する。PR は `session:` ではなく `prs:` に入れる。
- `alloc-id` サブコマンド: タスク id を project ごとのロック下で原子的に採番し、予約ファイルを
  作成する。skill の create がこれを使うことで、ローカル並行 create の id 衝突 (TOCTOU) を防ぐ。
- 操作 skill `/agent-tasks` (登録 / 一覧 / おすすめ / 着手 / spawn / batch / 完了 / 保留)。
- 閲覧 CLI `agent-tasks` (list / show / edit / sync / status / doctor / where)。
- git worktree によるタスクごとの並行開発 (`start`)、別 pane への spawn、複数タスクの直列実行 (`batch`)。
- worktree 作成後フック `worktree-init` (`.worktreeinclude` のコピー + `.worktree-post-create` 実行)。
- worktree 設定の雛形生成 `scaffold-worktree` (firebase / rails テンプレ同梱)。
- セッション状態 (working / waiting / ended) の可視化 (`session-hook` + `list` の SESSION 列)。
- 実行中タスクを表示する status line (`statusline`)。
- bash / zsh のシェル補完 (`completion`)。
- blocked タスクの理由・経過の可視化 (`list` の BLOCKED 列)。
- `started_at` / `completed_at` によるリードタイム表示。

### Changed

- CHANGELOG を **日付セクション (`## YYYY-MM-DD`, main マージ日)** 方式に変更した。実装中は
  `## [Unreleased]` に入れ、**マージ時に実マージ日の日付セクションへ移す** (日跨ぎの PR でも日付が
  マージ日と一致。移動はブランチ上で行い main は直接編集しない = 並行マージでも PR 経由で安全)。
- skill の create が `alloc-id` を `--pull` 無しで呼ぶようにした (ストアは基本 1 マシン前提)。
  ストア側に未コミット変更があると `--pull` の `git pull --rebase` が失敗してノイズが出ていたのを回避。
  CLI の `--pull` フラグ自体は残す (複数マシン共有時に手動で使える)。

### Fixed

- `TestColorEnabled` がターミナルから直接 `go test` すると失敗していた問題を修正
  (テストが実 stdout の TTY 状態に依存していた。TTY 判定を差し替え可能にして決定的にした)。
- zsh 補完の `show`/`edit`/`session-link` で、位置引数の補完時に `i=2` のようなゴミが入力に
  混入する問題を修正 (補完文脈で表示を壊す C 言語形式の `for (( ))` を foreach に置換)。
- zsh 補完で、サブコマンド無しの `agent-tasks --project <TAB>` が project 値を補完せず
  サブコマンド一覧を出していた問題を修正 (値を取る大域フラグの直後を先に処理する)。
