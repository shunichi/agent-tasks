---
name: agent-tasks
description: "エージェント開発タスクをリポジトリ外の中央ストア (~/agent-tasks-store) で管理する skill。タスクの登録・一覧・着手 (git worktree で並行)・完了・保留・別 pane への spawn を行う。トリガー: 'タスクを作る/登録', 'タスク一覧', 'タスクに着手', 'タスクを完了', '別 pane で着手/spawn', '/agent-tasks create|list|start|done|block|spawn' など。"
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
- **spawn**: 「別 pane で着手」「新しいセッションで 0001 をやって」「spawn 0001」
- **done**: 「〜が完了」「done 0001」
- **block**: 「〜を保留」「block 0001」

判断できなければユーザーに確認する。

---

## create — タスク登録

> **登録のみ。着手はしない。** create はタスクファイルを作るだけで完結する。
> ユーザーが明示的に「着手」「start」「やって」「直して」等と言わない限り、
> 実装・修正・worktree 作成・コミットには一切進まない。複数タスクをまとめて登録しても同じ
> (1 件も着手しない)。着手したい場合は別途 `start` を指示してもらう。

1. project キーを決める (上記)。
2. `<root>/<project>/` がなければ作成する。
3. 既存 `<root>/<project>/*.md` の最大連番 + 1 で `id` を決める (なければ `0001`)。
4. `slug` を決める。ユーザー指定があれば英語ケバブケースに変換、なければ確認する。
5. `<root>/<project>/<NNNN>-<slug>.md` を上記形式で作成する。`status: todo`、`agent`/`session`/`branch`/`worktree` は空、`created`/`updated` は当日。
6. 作成したパスを報告して**そこで止まる**。**コードリポジトリには一切コミットしない。**

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
3. **コンフリクトチェック**: 同じ project に他の `in-progress` タスクがあるか調べる (`agent-tasks --project <project> --status in-progress`、無ければ各ファイルの frontmatter を見る)。
   - 各 in-progress タスクの要件・進捗ログから「触る予定のファイル/領域」を読み取り、本タスクの想定変更と重なりそうか判断する。
   - 重なりそうなら **着手前にユーザーへ伝える**: どのタスク (id/branch) と、どのファイル/領域で衝突しそうかを具体的に示し、(a) 先に片方を終えてから進める / (b) 承知の上で並行する のどちらにするか確認する。明示の指示があるまで worktree は作らない。
   - 重なりが無さそうなら、その旨を一言添えてそのまま進める。
4. worktree とブランチを用意する (**冪等**)。コードリポジトリの root で:
   ```sh
   git worktree add ../<project>--<NNNN> -b task/<NNNN>-<slug>
   ```
   - 既定ブランチ (main) の最新から分岐する。必要なら事前に `git fetch` / 最新化する。
   - **既に同じ worktree/branch が存在する場合は作成をスキップする** (`git worktree list` に
     `../<project>--<NNNN>` があれば再作成しない)。`spawn` から起動された子セッションは worktree が
     既に在る状態で start するため、ここでエラーにせず frontmatter 更新 (手順 5) へ進む。
5. タスクファイルの frontmatter を更新する:
   - `status: in-progress`
   - `agent: claude` (自分のエージェント名)
   - `session:` 自分のセッション URL が分かれば記録 (Claude Code なら会話フッタの `Claude-Session` URL)
   - `branch: task/<NNNN>-<slug>` / `worktree: ../<project>--<NNNN>`
   - `updated:` 当日、`## 進捗ログ` に「着手」を追記
6. **以降の実装作業は作成した worktree (`../<project>--<NNNN>`) の中で行う。** 元のチェックアウトは汚さない。
7. プロジェクトの作法に従って実装する (`CLAUDE.md` / `AGENTS.md` を読む)。完了に近づいたら `done` へ。

---

## spawn — 別 pane に新セッションを開いて着手 (tmux)

並行開発をワンステップで。**worktree を用意 → tmux の別 pane を開く → そこで新しいエージェント
セッションを起動 → そのセッションが自分で `start` して着手する** までを通す。`start` を内包するのでは
なく、worktree だけ用意して**着手 (frontmatter 確定) は子セッションに任せる**。理由: frontmatter の
`session:` には着手したセッション自身の URL が入るべきで、それを知っているのは子だけだから。

### 前提とフォールバック

- **tmux 内で実行する必要がある** (`$TMUX` が設定されているか確認)。
- tmux 外、または確認だけしたい場合は副作用を出さず、**手で貼り付けられるコマンド一式を表示して終了**
  する (下記「フォールバック出力」)。新ターミナルの自動起動はしない (環境依存で脆いため)。

### 手順

1. 対象タスクファイルを特定する (project + id)。slug から `branch: task/<NNNN>-<slug>` /
   `worktree: ../<project>--<NNNN>` を決める。
2. **二重着手チェック**: `status` が `in-progress` で `session` が埋まっていれば別セッション作業中の
   可能性。ユーザーに確認し、明示の指示がなければ中止する。
3. **コンフリクトチェック**: start と同様、同 project の他 `in-progress` と触る領域が重ならないか確認する
   (重なりそうならユーザーに伝えて判断を仰ぐ)。
4. `$TMUX` を確認する。未設定なら「フォールバック出力」に進む。
5. worktree を用意する (**冪等**。frontmatter はここでは触らない — 子の start に任せる):
   ```sh
   git worktree add ../<project>--<NNNN> -b task/<NNNN>-<slug>   # 既存ならスキップ
   ```
6. worktree の**絶対パス**を求め、その場所でシェルを持つ pane を開いて子セッションを起動する。
   **開いた pane の id を必ず取得し (`-P -F '#{pane_id}'`)、`send-keys` はその id を明示ターゲットにする**
   (target 省略だと別 pane に送られる事故が起きる):
   ```sh
   WT="$(cd ../<project>--<NNNN> && pwd)"
   AGENT="${AGENT_TASKS_AGENT:-claude}"        # agent 非依存。既定 claude、env で上書き可
   PANE=$(tmux split-window -h -P -F '#{pane_id}' -c "$WT")   # 開いた pane の id を控える
   tmux send-keys -t "$PANE" "$AGENT 'タスク <NNNN> に着手して'" Enter
   echo "spawned pane: $PANE (cwd: $WT)"
   ```
   - pane が最初から worktree 内にあるので、子セッション終了後もそのまま作業継続できる。
   - 初期プロンプト「タスク <NNNN> に着手して」は agent 非依存。codex など固有の渡し方が要る場合は
     将来対応 (今は claude 既定)。
7. 子セッションは起動後に `/agent-tasks start <NNNN>` を実行する。worktree は既存なので冪等にスキップ
   され、frontmatter (status/agent/**session**/branch/worktree) を子自身が確定する。
8. **親 (spawn した側) は frontmatter を更新しない。** 代わりに次の「起動確認」を行う。

### 起動確認 (子セッションがちゃんと開始したか)

`tmux capture-pane` は claude の TUI が alt-screen のため空になりがちで当てにならない。**確実な起動シグナルは
frontmatter の変化**: 子が `start` を完了すると `status: in-progress` になり `session:` に**子自身の URL**が入る。
これを親がポーリングして確認する。

1. spawn 直前のタスクの `status` / `session` を控えておく (基準。通常は `todo` / 空)。
2. pane 起動後、最大 ~90 秒ほど数秒間隔でポーリングし、frontmatter が
   **`status: in-progress` かつ `session:` に URL が入った** (=基準から変化した) ら起動成功と判断する:
   ```sh
   for i in $(seq 1 30); do
     sleep 3
     s=$(agent-tasks show <project> <NNNN>)
     echo "$s" | grep -q 'status: in-progress' && echo "$s" | grep -Eq 'session: *https?://' && { echo OK; break; }
   done
   ```
3. **成功**: 起動できた旨と pane id・記録された子の session URL をユーザーに報告する。
4. **タイムアウト (変化なし)**: 自動では分からないので、**pane id を伝えて「その pane で claude が起動して
   いるか確認してほしい」とユーザーに促す**。よくある原因と対処も併せて示す:
   - claude の初回起動が遅い / 入力待ちで止まっている → `tmux select-pane -t "$PANE"` で覗く。
   - プロンプトが届いていない → `tmux send-keys -t "$PANE" "<AGENT> 'タスク <NNNN> に着手して'" Enter` を再送。
   - それでも駄目なら pane を閉じ (`tmux kill-pane -t "$PANE"`)、worktree はそのままに再 spawn するか、
     「フォールバック出力」を手で実行してもらう。

### フォールバック出力 (tmux 外 / 確認だけのとき)

副作用を出さず、ユーザーが手で実行できる形を表示する:

```sh
git worktree add ../<project>--<NNNN> -b task/<NNNN>-<slug>   # 未作成なら
cd ../<project>--<NNNN>
claude 'タスク <NNNN> に着手して'
```

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
