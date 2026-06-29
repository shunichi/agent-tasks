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
project: webapp
title: ブックマークのドラッグ並び替え
status: todo          # todo | in-progress | blocked | review | done
agent:                # 着手したエージェント (claude / codex / ...)
session:              # エージェントのセッション URL
branch: task/0001-bookmark-dnd
worktree: ../webapp--0001
created: "2026-06-28T14:30:00+09:00"
updated: "2026-06-28T14:30:00+09:00"
# start したとき付ける (初回着手の日時。再 start では上書きしない):
# started_at: "2026-06-28T15:00:00+09:00"
# done にしたとき付ける (done→in-progress で再オープンするとクリア):
# completed_at: "2026-06-30T11:00:00+09:00"
# blocked のときだけ付ける (block で設定し、start/done で外す):
# blocked_at: "2026-06-28T14:30:00+09:00"   # 保留にした日時 (ISO8601)。経過算出の基点
# blocked_reason: API 仕様の確認待ち         # 保留理由 (一覧表示用)
---

# 要件
（タスク内容）

## 進捗ログ
- 2026-06-28 登録
```

- `id` は project ごとの連番 (4桁ゼロ埋め)。既存ファイルの最大 + 1。
- `slug` は内容を表す英語ケバブケース。
- `status` を更新したら必ず `updated` を現在の日時に変え、`## 進捗ログ` に 1 行追記する。
- frontmatter の時刻系 (`created` / `updated` / `started_at` / `completed_at` / `blocked_at`) は
  **ISO8601 日時** (ローカルオフセット込み) で持つ。`date --iso-8601=seconds` で取得する
  (推測しない。例 `2026-06-28T14:30:00+09:00`)。値はダブルクォートで囲む (`:` を含むため)。
  一覧では日付だけ表示され、`show` で時刻まで見える。
- `started_at` / `completed_at` で着手〜完了の所要期間 (リードタイム) を追える。start で `started_at`
  (初回のみ)、done で `completed_at` を記録する (詳細は各操作)。`show` が所要時間/経過を要約表示する。
- `## 進捗ログ` の行頭の日付は `date +%F` (日付のみ) でよい (人が読む履歴のため)。
- `blocked` のときだけ `blocked_at` (保留にした日時) と `blocked_reason` (理由) を付ける。
  list がこれで「保留からの経過」と理由を表示する。block で設定し、start/done で外す
  (詳細は各操作を参照)。`blocked_at` は ISO8601 日時で `date --iso-8601=seconds` で取得する。

## 操作の判定

ユーザーの発言から操作を判定する。引数 (`create`/`list`/`start`/`done`/`block`/`sync`/`scaffold`) があればそれに従う。

- **create**: 「タスクを作る/追加/登録」「〜というタスク」
- **list**: 「タスク一覧」「タスクの進捗」「何が残ってる」
- **start**: 「〜に着手」「タスク 0001 をやって」「start 0001」
- **spawn**: 「別 pane で着手」「新しいセッションで 0001 をやって」「spawn 0001」
- **done**: 「〜が完了」「done 0001」
- **block**: 「〜を保留」「block 0001」
- **sync**: 「タスクを同期」「ストアを push/commit」「sync」
- **scaffold**: 「worktree 設定を作る/入れる」「このプロジェクトを並行開発対応に」「scaffold」

判断できなければユーザーに確認する。

---

## create — タスク登録

> **登録のみ。着手はしない。** create はタスクファイルを作るだけで完結する。
> ユーザーが明示的に「着手」「start」「やって」「直して」等と言わない限り、
> 実装・修正・worktree 作成・コミットには一切進まない。複数タスクをまとめて登録しても同じ
> (1 件も着手しない)。着手したい場合は別途 `start` を指示してもらう。

1. project キーを決める (上記)。
2. `<root>/<project>/` がなければ作成する。
3. **採番直前にストアを最新化する** (`agent-tasks sync --no-push` か、ストアで `git pull --rebase`)。
   別マシン/別セッションが先に採番した番号を取り込んでから決めることで、id 衝突を起きにくくする。
4. 既存 `<root>/<project>/*.md` の最大連番 + 1 で `id` を決める (なければ `0001`)。
   - 採番は「既存最大 + 1」なので、**並行 create では同じ id を引く競合 (TOCTOU) があり得る。**
     最新化 (手順 3) で大幅に減らせるが完全には防げないため、作成後に手順 7 で検査する。
5. `slug` を決める。ユーザー指定があれば英語ケバブケースに変換、なければ確認する。
6. `<root>/<project>/<NNNN>-<slug>.md` を上記形式で作成する。`status: todo`、`agent`/`session`/`branch`/`worktree` は空、`created`/`updated` は現在の日時 (`date --iso-8601=seconds`)。
7. **作成後に `agent-tasks doctor --project <project>` で重複/不一致を検査する。** 重複 id が出たら
   (同じ id のファイルが他にある)、空いている最大連番 + 1 に振り直してファイル名と frontmatter `id:` の
   両方を直し、再度 doctor が通ることを確認する。
8. 作成したパスを報告して**そこで止まる**。**コードリポジトリには一切コミットしない。**

---

## list — 一覧

**既定は現在の project のみ、横断は明示。** `agent-tasks` は引数なしだと
**現在のコードリポジトリ (project) のタスクだけ**を表示する (現在 project = cwd の
git リポジトリのメイン repo 名)。全 project を横断したいときだけ `--all-projects` を付ける。

1. `agent-tasks` コマンドがあればそれを使う (`command -v agent-tasks`):
   - 現在 project のみ (既定): `agent-tasks`
   - 全 project を横断: `agent-tasks --all-projects`
   - 別 project を指定: `agent-tasks --project <project>`
   - done も含める: `--all` / `-a` を併用
   - git リポジトリ外で実行した場合は判定不能なので自動で横断にフォールバックする。
2. コマンドが無ければ `<root>/**/*.md` の frontmatter を読み、project / id / status / title を表にして表示する。
   この場合も既定は現在 project (root の basename) のみに絞り、横断したいときだけ全件を出す。

### セッション状態 (working / waiting)

hook を導入していると、in-progress 行に `SESSION` 列が出て各セッションが **working** (処理中) /
**waiting** (入力・許可待ち) / **ended** (終了) を示す (`?` はマーカー未取得)。並行 pane の
「どれが自分の応答待ちか」を一覧で把握できる。導入は `agent-tasks session-hook --print-config` の
スニペットを `~/.claude/settings.json` に 1 度追加するだけ (各セッションが状態変化時に
`agent-tasks session-hook` を呼びマーカーを更新する)。`tmux capture-pane` は claude の TUI が
alt-screen のため当てにならないので、起動/状態の確認はこの hook 由来のシグナル (= `list` の SESSION 列)
で行う。

**突合は `session-link` (session_id ベース) が主経路**で、spawn の挙動に依存しない。直接 start でも
spawn 経由でもセッションの cwd はメインリポ (worktree の外) なので、start 手順 6 の
`agent-tasks session-link` がセッションを明示的に紐づける。
(補助として、もし cwd が worktree 内のセッションがあれば cwd の git root = `<project>--<NNNN>` でも
突合できるが、現行フローでは通常使われない。)

設計の切り分け: マーカー/link の保管・突合は **agent 中立** (CLI 側)。一方で状態の信号源 (hook) と
自分の session_id の取得方法は **agent 固有**で、SKILL の手順 6 に agent 別に書く。Claude は自分の
session_id を `--session` で明示でき、それが無理な agent は cwd 逆引きにフォールバックする。

---

## start — 着手 (git worktree で並行開発)

並行実行の肝。**別々のエージェントセッションがそれぞれ別タスクを start することで同時開発できる。**

1. 対象タスクファイルを特定する (project + id)。
2. **二重着手チェック**: `status` が `in-progress` で `session` が埋まっている場合、別セッションが作業中の可能性。ユーザーに確認し、明示的な指示がなければ中止する。
3. **コンフリクトチェック**: 同じ project に他の `in-progress` タスクがあるか調べる (`agent-tasks --project <project> --status in-progress`、無ければ各ファイルの frontmatter を見る)。
   - 各 in-progress タスクの要件・進捗ログから「触る予定のファイル/領域」を読み取り、本タスクの想定変更と重なりそうか判断する。
   - 重なりそうなら **着手前にユーザーへ伝える**: どのタスク (id/branch) と、どのファイル/領域で衝突しそうかを具体的に示し、(a) 先に片方を終えてから進める / (b) 承知の上で並行する のどちらにするか確認する。明示の指示があるまで worktree は作らない。
   - 重なりが無さそうなら、その旨を一言添えてそのまま進める。
4. worktree とブランチを用意する (**冪等**)。**worktree の作成は start の責務** — 直接 start でも
   spawn 経由 (別 pane で start) でも同じ。**メインリポ root** で実行する (cwd が worktree 内でも
   `git rev-parse --show-toplevel` で root を求めてそこで):
   ```sh
   git worktree add ../<project>--<NNNN> -b task/<NNNN>-<slug>
   ```
   - 既定ブランチ (main) の最新から分岐する。必要なら事前に `git fetch` / 最新化する。
   - **既に同じ worktree/branch が存在する場合は作成をスキップする** (`git worktree list` に
     `../<project>--<NNNN>` があれば再作成しない)。再 start でここに来たときはエラーにせず
     frontmatter 更新 (手順 5) へ進む。
   - worktree を用意したら**作成後フック**を流して環境を整える (冪等。設定が無ければ no-op なので
     無条件に呼んでよい)。`.worktreeinclude` の gitignored ファイル (`.env` 等) をコピーし、
     `.worktree-post-create` (依存インストール・ポート分離など) を worktree 内で実行する:
     ```sh
     agent-tasks worktree-init ../<project>--<NNNN>
     ```
     CLI が無い環境では手動で `.env` 等をコピーし、必要なセットアップ (依存インストール等) を worktree 内で行う。
5. タスクファイルの frontmatter を更新する:
   - `status: in-progress`
   - `agent: claude` (自分のエージェント名)
   - `session:` 自分のセッション URL が分かれば記録 (Claude Code なら会話フッタの `Claude-Session` URL)
   - `branch: task/<NNNN>-<slug>` / `worktree: ../<project>--<NNNN>`
   - `updated:` 現在の日時、`## 進捗ログ` に「着手」を追記
   - **`started_at:` が未設定なら現在の日時を記録する** (`date --iso-8601=seconds`)。
     既に設定済み (再 start) なら**上書きしない** — 初回着手を保持してリードタイムを正しく測るため。
   - **blocked から再開する場合は `blocked_at` / `blocked_reason` を削除する** (もう保留ではないため)。
   - **done から再オープンする場合 (done→in-progress) は `completed_at` を削除する** (まだ完了ではないため)。
6. **セッション状態を紐づける** (任意・冪等。session 状態 hook を導入している場合のみ意味がある)。
   これで `list` の `SESSION` 列に working/waiting が出る。**直接 start でも spawn 経由でも、セッションの
   cwd はメインリポ (worktree の外) なので、この紐づけが追跡の主経路**になる (cwd=worktree に依存しない)。
   **取得方法は agent 固有**なので下記から選ぶ:
   - **Claude Code (推奨: 自分の session_id を明示)**: Claude は自分のローカル session_id を知り得る
     (スクラッチパッドのパス `…/<session_id>/scratchpad` の末尾ディレクトリ名が session_id)。それを渡す:
     ```sh
     agent-tasks session-link <NNNN> --session <自分の session_id>
     ```
     明示すると cwd 逆引きの曖昧性 (同一 cwd 複数セッション) を完全に回避できる。session_id が分からなければ
     `--session` を省略してフォールバック (下) に任せる。
   - **フォールバック (cwd 逆引き。agent 中立)**: `--session` を省略すると、hook が書いた sess マーカーを
     **現在 cwd** で逆引きして自セッションを特定する:
     ```sh
     agent-tasks session-link <NNNN>   # 必ずコードリポジトリ root (= このセッションの cwd) で実行
     ```
     worktree に `cd` してから呼ぶと cwd がずれて特定できないので注意。
   - どちらも hook 未導入/未発火でセッションを特定できなければエラーにはならない (案内が出るだけ)。
   - **他の agent (codex 等)**: その agent の hook 相当でマーカーを同じ形式で書ける場合に対応 (現状未整備)。
     自分の session_id を言えるなら `--session`、言えないなら cwd 逆引きを使う。
7. **以降の実装作業は作成した worktree (`../<project>--<NNNN>`) の中で行う。** 元のチェックアウトは汚さない。
8. プロジェクトの作法に従って実装する (`CLAUDE.md` / `AGENTS.md` を読む)。完了に近づいたら `done` へ。

---

## spawn — 別 pane で新セッションを開いて start させる (tmux, fire-and-forget)

spawn は「**別 pane を開いてそこで新セッションに start させるだけ**」。重い per-task オーケストレーション
ではなく、**親は pane を開いて指示を送ったら忘れてよい**。worktree の作成・session-link・frontmatter
確定はすべて子の `start` がやる (start が worktree ライフサイクルの唯一の所有者)。spawn ＝「別 pane で
start」と考えればよい。

**pane はメインリポ root で開く** (worktree 内では開かない)。理由:
- worktree はまだ存在しない (子の start が作る) ので、pane の cwd にはできない。
- セッション追跡は `session-link` (session_id ベース、0027) なので cwd が worktree でなくてよい。
  子は cwd=メインリポのまま start し、worktree は絶対パス / `cd` サブシェルで触る (同一セッション
  start と同じ。実証済み)。それでも `list` の SESSION 列に working/waiting が出る。
- cwd がメインリポなので、子が done で `git worktree remove` しても**自分の足元を消さない** (安全)。

### 前提とフォールバック

- **tmux 内で実行する必要がある** (`$TMUX` が設定されているか確認)。
- tmux 外なら副作用を出さず「フォールバック出力」を表示して終了する。

### 手順

1. 対象タスクを特定する (project + id)。
2. **二重着手チェック**: `status` が `in-progress` で `session` が埋まっていれば別セッション作業中の
   可能性。ユーザーに確認し、明示の指示がなければ中止する (これだけは pane を開く前に見ておく)。
3. `$TMUX` を確認する。未設定なら「フォールバック出力」に進む。
4. **メインリポ root で** pane を開き、子セッションを起動して start を指示する。開いた pane の id を
   取得し (`-P -F '#{pane_id}'`)、`send-keys` はその id を明示ターゲットにする (target 省略だと別 pane に
   送られる事故が起きる):
   ```sh
   ROOT="$(git rev-parse --show-toplevel)"      # メインリポ root (worktree 内から実行しても可)
   AGENT="${AGENT_TASKS_AGENT:-claude}"          # agent 非依存。既定 claude、env で上書き可
   PANE=$(tmux split-window -h -P -F '#{pane_id}' -c "$ROOT")
   tmux send-keys -t "$PANE" "$AGENT 'タスク <NNNN> に着手して'" Enter
   echo "spawned pane: $PANE (cwd: $ROOT)"
   ```
5. **親はここで完了**。worktree 作成も worktree-init もポーリングもしない。子の `start` が
   worktree 作成・作成後フック・session-link・frontmatter 確定まで行う (start 手順参照)。
   - 初期プロンプト「タスク <NNNN> に着手して」は agent 非依存 (codex 等は将来対応、今は claude 既定)。
   - 起動を確認したいときだけ、`agent-tasks --watch --status in-progress` で SESSION 列を眺めれば
     子が start を終えた時点で in-progress / working として現れる (能動ポーリングは不要)。
   - うまく起動しないとき (claude の初回起動が遅い/プロンプト未達) は `tmux select-pane -t "$PANE"` で
     覗き、必要なら `tmux send-keys -t "$PANE" "$AGENT 'タスク <NNNN> に着手して'" Enter` を再送する。

### フォールバック出力 (tmux 外のとき)

副作用を出さず、ユーザーが別ターミナルで手実行できる形を表示する (メインリポ root で起動):

```sh
cd "$(git rev-parse --show-toplevel)"
claude 'タスク <NNNN> に着手して'   # 子が /agent-tasks start <NNNN> を実行し worktree も作る
```

---

## scaffold — worktree 設定をプロジェクトに展開

プロジェクトを並行開発対応にする一度きりのセットアップ。スタック (firebase / rails / ...) を検出し、
推奨の `.worktreeinclude` / `.worktree-post-create` を**プロジェクト root に書き出す**。以降は
start/spawn の作成後フック (worktree-init) がそれを毎回適用する。

1. `agent-tasks scaffold-worktree [<stack>]` を実行する (コードリポジトリ root で):
   - スタック自動検出 (firebase.json/.firebaserc → firebase、bin/rails 等 → rails)。検出できなければ
     `<stack>` を指定するか `--list` で候補を見る。
   - 既存の設定ファイルは上書きしない (`--force` で上書き)。別ディレクトリは `--dir <path>`。
2. 生成された 2 ファイルを**ユーザーと確認・調整**する (ポート計算・依存コマンド・コピー対象は
   プロジェクト固有なので、テンプレはあくまで叩き台)。
3. 問題なければコードリポジトリにコミットする (これは**コードリポジトリ側**の変更。ストアではない)。

> テンプレはバイナリに同梱 (`templates/<stack>/`)。スタックを増やすときは
> `templates/<新stack>/{worktreeinclude,post-create}` を足すだけ (必要なら detectStack も拡張)。

---

## 作成後フック (worktree-init) の設定

worktree は新規チェックアウトなので `.env` も `node_modules` も無い。`agent-tasks worktree-init` は
**コードリポジトリ root に置いた次の 2 ファイル**を見て環境を整える (両方とも任意。無ければ no-op)。
プロジェクトごとに用意しておけば start/spawn が自動で流す。

- **`.worktreeinclude`** — worktree にコピーする gitignored ファイル。`.gitignore` 構文のサブセット
  (リテラルパス / `*` グロブ / ディレクトリ)。Claude Code の `--worktree` と同名・互換なので、既に
  置いてあればそのまま使われる。tracked ファイルは安全のためコピーされない。例:
  ```
  .env
  .env.local
  config/secrets.json
  ```
- **`.worktree-post-create`** — worktree 内 (cwd = その worktree) で実行されるスクリプト。依存インストール、
  ポート分離用 `.env.local` 生成、DB 準備などを書く。実行ビットがあれば直接 (shebang 尊重)、無ければ `sh`
  で実行。環境変数 `AGENT_TASKS_WORKTREE` / `AGENT_TASKS_MAIN` / `AGENT_TASKS_PROJECT` が渡る。例:
  ```sh
  #!/bin/sh
  pnpm install                       # or: bundle install && bin/rails db:prepare
  # ポート分離: worktree 名から一意なオフセットを作って .env.local に書く等
  ```

冪等性: コピーは既存ファイルを上書きしない。post-create は worktree ごとのマーカーで一度だけ実行される
(`agent-tasks worktree-init <dir> --force` で再実行)。

> これら 2 ファイルはスタック (firebase / rails) ごとに `scaffold` で雛形を生成できる
> (上記「scaffold」参照)。worktree-init はその設定を**実行するだけ**の汎用機構。

---

## done — 完了

1. 対象タスクファイルを特定する。
2. worktree 内で最終確認 (型/Lint/テストなど、プロジェクトの作法に従う)。
3. 変更をコミットする (worktree 内で。コミット先はコードリポジトリ)。
4. ユーザーが PR を望めば作成する (`gh pr create`)。PR 待ちの段階なら `status: review`、マージまで完了したら `status: done`。
5. タスクファイルの frontmatter を更新: `status` を `review` または `done`、`updated` を現在の日時、`## 進捗ログ` に対応内容を追記。
   - **`status` を `done` にするとき `completed_at:` に現在の日時を記録する** (`date --iso-8601=seconds`)。
     `review` 止まりのときはまだ記録しない (`done` になった時点で記録する)。
   - blocked から直接完了する場合は `blocked_at` / `blocked_reason` を削除する (もう保留ではないため)。
6. `done` まで来たら worktree を撤去する。**メインリポ root から実行する** (start/spawn とも
   セッションの cwd はメインリポなので、対象 worktree は別ディレクトリ＝安全に消せる):
   ```sh
   git worktree remove ../<project>--<NNNN>
   ```
   - 未コミットがある場合は撤去せず、ユーザーに知らせる。
   - ⚠️ **自分の cwd がその worktree の中にあるセッションでは撤去しない** (cwd ごと消えて以降の hook が
     `ENOENT posix_spawn` で壊れる)。通常フローでは cwd=メインリポなので起きないが、worktree 内で
     直接起動したセッションの場合は、撤去を**メインリポ / 別セッションから**行う (または撤去せず残す)。

---

## block — 保留

1. 対象タスクファイルを特定する。
2. frontmatter を更新する:
   - `status: blocked`
   - **`blocked_at:`** に保留にした日時を記録する。`date --iso-8601=seconds` で取得した
     ISO8601 日時 (例 `"2026-06-28T14:30:00+09:00"`)。list がここから「保留からの経過」を出す
     (`updated` ではなく `blocked_at` から測る。`updated` は他の更新でも動くため)。
   - **`blocked_reason:`** に保留理由を1行で記録する (何の判断/確認待ちか)。一覧で title に添えて表示される。
   - `updated` を現在の日時に。
3. `## 進捗ログ` にも**保留理由**を追記する (frontmatter は「現在の理由」、ログは履歴)。
4. worktree は残す (再開できるように)。判断材料が揃ったら `start` で再開、または直接実装を続ける。
   **再開・完了で blocked を抜けるときは `blocked_at` / `blocked_reason` を削除する** (start / done 参照)。

---

## sync — ストアの同期 (git commit & push)

タスクファイルはコードリポジトリの外 (`~/agent-tasks-store`、git 管理) にある。
create/start/done/block でファイルを更新したあと、ストアを commit & push してマシン間で同期する。

- **未同期の確認**: `agent-tasks status` でストアの未コミット/未 push の状況を1行で確認できる
  (例: `未コミット 3 ファイル / 未 push 2 コミット (origin/main)`、同期済みなら `クリーン (同期済み)`)。
  未同期があれば exit 1 を返すので、「sync が必要か」を事前に判断したいときに使う。
- **基本は CLI に任せる**: `agent-tasks sync` がストアで `add -A` → コミットメッセージ自動生成 →
  `commit` → `pull --rebase` → `push` まで行う。push したくない時は `agent-tasks sync --no-push`
  (commit で止める)。upstream 未設定なら初回 `push -u origin <branch>` で追跡を設定する。
- コミットメッセージは変更ファイルから自動生成される (例: `tasks: agent-tasks/0005 (in-progress)`、
  複数なら `tasks: update N tasks` + 本文に列挙)。
- **いつ実行するか**: ユーザーが「同期」「push」と言ったとき、または create/done などストア更新を伴う
  操作の区切りで「ストアを sync するか」を一言促す (勝手に push しない。明示の指示か確認の上で実行)。
- `pull --rebase` でコンフリクトした場合や push が失敗した場合は CLI がエラーを返すので、
  内容をユーザーに伝えてストア (`~/agent-tasks-store`) での手動解決を促す。
- CLI が無い環境では手動: `cd ~/agent-tasks-store && git add -A && git commit && git pull --rebase && git push`。
