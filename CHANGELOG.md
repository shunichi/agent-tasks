# Changelog

このプロジェクトの主な変更を記録する。形式は [Keep a Changelog](https://keepachangelog.com/ja/1.1.0/) に倣う。

タグによるバージョニングは行わず、`Unreleased` セクションに追記していく運用。機能追加・破壊的変更・
利用者に影響する修正があったときに、その変更を含む PR で `Unreleased` に1行追記する
(内部リファクタの細かい話は不要)。

## [Unreleased]

### Added

- シェル補完の動的補完: `--project` の値 (ストアの project 一覧) と `show`/`edit`/`session-link` の
  id 引数 (現在 project のタスク id) をタブ補完できる。内部コマンド `completion-values` が候補を列挙する。
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
