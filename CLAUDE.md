# CLAUDE.md

## このリポジトリは何か

エージェント (Claude / Codex / ...) に開発させるタスクを管理する一式。
**操作 (skill)** と **閲覧 (CLI)** を1リポジトリにまとめている。
タスクデータ自体は**各コードリポジトリの外** (`~/agent-tasks-store/`) に置く
(ブランチごとに状態がずれるのを避けるため)。

ユーザー向けの使い方は `README.md`、データ形式は `~/agent-tasks-store/README.md` を参照。

## 構成

```
agent-tasks/                    ← このリポジトリ = ツール (github.com/shunichi/agent-tasks, private)
  skills/agent-tasks/SKILL.md   # 操作 (agent 用): /agent-tasks。登録/一覧/着手/完了/保留
  main.go                       # 閲覧 CLI: コマンド振り分け + list/show/where
  store.go                      # タスクの model + ストア走査 + frontmatter パース
  render.go                     # 色付け + CJK 幅対応のテーブル描画
  store_test.go                 # テスト
  Makefile                      # build / install / link / test / fmt / vet
  bin/agent-tasks               # ビルド成果物 (gitignore)

~/agent-tasks-store/            ← データ (このリポジトリの外)
  <project>/<NNN>-<slug>.md     # 1 タスク = 1 ファイル。project = コード repo root の basename
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
  `git worktree add ../<project>--<NNN> -b task/<NNN>-<slug>` を作る。別セッションで start すれば
  衝突なく同時進行。二重着手は status=in-progress + session でガード。

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
- ⏳ 旧 repo `agent-dashboard` (GitHub, private, 中身は trivial) の削除はユーザーが後で対応予定。
- 💡 未着手の発展案:
  - `~/agent-tasks-store` 自体の git 化 (マシン間同期)。
  - skill 側にある `create` / `start` / `done` / `block` の一部を CLI サブコマンド化するか検討
    (今は手順書として skill が担当)。
  - Web ダッシュボード化 (このリポジトリ内で発展)。
  - blocked の理由・経過時間の可視化。

## コミットメッセージ

末尾に付ける:
```
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
```
