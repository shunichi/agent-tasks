# agent-tasks

[![CI](https://github.com/shunichi/agent-tasks/actions/workflows/ci.yml/badge.svg)](https://github.com/shunichi/agent-tasks/actions/workflows/ci.yml)

エージェント (Claude / Codex / ...) に開発させるタスクを管理するための一式。
**操作 (skill)** と **閲覧 (CLI)** を1リポジトリにまとめている。

タスクデータは各コードリポジトリの**外**、`~/agent-tasks-store/` に置く
(repo `agent-tasks` = ツール、`agent-tasks-store` = タスクの中身、という役割分担)。
リポジトリ内に置かないのは、ブランチごとにタスク状態がずれるのを避けるため。

## インストール

`make install` で CLI をビルドし、CLI (`~/.local/bin`) と skill (`~/.claude/skills`)
を symlink し、bash/zsh の補完スクリプトも書き出す (`~/.local/bin` が PATH にある前提):

```sh
make install
```

- skill は symlink なので編集すれば即反映。
- CLI は Go バイナリ (依存は最小限) なので、ソースを変えたら `make build` で再ビルドする。
- **補完は静的ファイル**なので、コマンドやフラグを増やしたら `make install` で再生成する
  (CLI 自体は symlink で最新だが補完だけ古くなるため)。zsh は `~/.zcompdump` キャッシュの都合で
  反映は新しいシェルから (即時にしたいときは `rm -f ~/.zcompdump && compinit`)。

## 使い方

操作は**エージェントに頼む** (skill)、閲覧は**ターミナルから** (CLI) の 2 系統。

### 操作 (エージェントに対して)

| 操作 | 言い方の例 |
| --- | --- |
| 登録 | 「〜というタスクを作って」「/agent-tasks create」 |
| 一覧 | 「タスク一覧」「/agent-tasks list」 |
| おすすめ | 「次に何をやるべき?」「/agent-tasks recommend」(着手候補を提示。着手はしない) |
| 着手 | 「タスク 0001 に着手」「/agent-tasks start 0001」(git worktree で並行開発) |
| 別 pane で着手 | 「別 pane で 0001 をやって」「/agent-tasks spawn 0001」(tmux の別 pane に新セッション) |
| 連続実行 | 「0042 と 0045 をまとめてやって」「/agent-tasks batch 0042 0045」(直列に start→done) |
| 完了 | 「0001 を完了」「/agent-tasks done 0001」 |
| 保留 | 「0001 を保留」「/agent-tasks block 0001」 |
| アーカイブ | 「0001 をアーカイブ」「/agent-tasks archive 0001」(削除せず退避。一覧から外す) |
| アーカイブ解除 | 「0001 を戻して」「/agent-tasks unarchive 0001」 |
| worktree 設定の展開 | 「worktree 設定を入れて」「/agent-tasks scaffold」(firebase/rails を検出して雛形生成) |

**並行開発**: 別々のエージェントセッションでそれぞれ別タスクを `start` すると、タスクごとに
git worktree + ブランチが切られ、衝突なく同時に開発できる。`spawn` は tmux の別 pane に新セッションを
開いて `start` を任せる (fire-and-forget)。`batch` は逆に複数タスクを 1 セッションで直列に流す。

### 閲覧 (ターミナルから)

```sh
agent-tasks                      # 現在 project の未完了タスク一覧 (done は非表示)
agent-tasks --all-projects       # 全 project を横断
agent-tasks --all                # done も含める (-a も可)
agent-tasks --status in-progress # status で絞り込み
agent-tasks --watch              # 一覧を自動更新表示 (-w)
agent-tasks tui                  # 一覧+詳細をインタラクティブに閲覧 (自動更新。端末専用)
agent-tasks --archived           # アーカイブ済みタスクだけを一覧 (通常は非表示)
agent-tasks show 0001            # 1 タスクの全文 (--archived で退避済みを開く)
agent-tasks edit 0001            # 1 タスクをエディタで開く
agent-tasks archive 0001         # タスクを退避 (削除せず archive/ へ移動。一覧から外す)
agent-tasks unarchive 0001       # 退避したタスクを元に戻す
agent-tasks status               # ストアの未同期状態を1行表示
agent-tasks sync                 # ストアを add/commit/push して同期
agent-tasks doctor               # id 重複・不整合を点検
agent-tasks where                # データディレクトリのパス
```

既定は**現在のコードリポジトリ (project) のタスクだけ**を表示する (横断は `--all-projects`、
別 project は `--project <name>`)。`<id>` は `1` でも `0001` でも同じタスクを指す。

## データの場所

```
~/agent-tasks-store/
  <project>/            # コードリポジトリ root の basename
    <NNNN>-<slug>.md    # 1 タスク = 1 Markdown ファイル
```

`AGENT_TASKS_STORE` で場所を変更可。データ形式の詳細は `~/agent-tasks-store/README.md` を参照。

## さらに詳しく

並行開発まわりの作り込み (worktree 作成後フック、scaffold、セッション状態の可視化、status line、
シェル補完、色出力、日時フィールドなど) と CLI の全コマンドは
**[docs/details.md](docs/details.md)** にまとめている。
