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
`agent-tasks session-hook` を呼びマーカーを更新する)。`tmux capture-pane` が alt-screen で当てに
ならない問題 (spawn の起動確認参照) を、hook 由来の確実なシグナルで補う位置づけ。

突合は 2 経路: spawn 子は worktree 内で動くので cwd の git root (= `<project>--<NNNN>`) で自動的に
紐づく。**同一セッション** (メインリポで start してそのまま作業) は cwd が worktree の外なので、
start 手順の `agent-tasks session-link` がセッションを明示的に紐づける (上記 start の手順 6)。

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
6. **セッション状態を紐づける** (任意・冪等。session 状態 hook を導入している場合のみ意味がある):
   ```sh
   agent-tasks session-link <NNNN>   # コードリポジトリ root (= このセッションの cwd) で実行する
   ```
   - これで**同一セッションで start したタスク** (cwd がメインリポのままで worktree の外) でも
     `list` の `SESSION` 列に working/waiting が出る。spawn 子セッションは worktree 内で start するため
     元々紐づくが、本コマンドを呼んでも冪等で無害。
   - **必ずコードリポジトリ root (このセッションの cwd) で実行する。** hook が書いたマーカーを cwd で
     逆引きして自セッションを特定するため、worktree に `cd` してから呼ぶと特定できない。
   - hook 未導入/未発火でセッションを特定できなくてもエラーにはならない (案内が出るだけ)。
7. **以降の実装作業は作成した worktree (`../<project>--<NNNN>`) の中で行う。** 元のチェックアウトは汚さない。
8. プロジェクトの作法に従って実装する (`CLAUDE.md` / `AGENTS.md` を読む)。完了に近づいたら `done` へ。

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
   agent-tasks worktree-init ../<project>--<NNNN>                # 作成後フック (子が開く前に環境を整える)
   ```
   作成後フックを親側で流しておくと、子セッションが整った環境で開ける。マーカーで冪等なので、
   子の start が再度呼んでも post-create は二重実行されない。
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
6. `done` まで来たら worktree を撤去する:
   ```sh
   git worktree remove ../<project>--<NNNN>
   ```
   未コミットがある場合は撤去せず、ユーザーに知らせる。

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
