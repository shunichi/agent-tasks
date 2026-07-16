---
name: agent-tasks
description: "エージェント開発タスクをリポジトリ外の中央ストア (~/agent-tasks-store) で管理する skill。タスクの登録・一覧・着手 (git worktree で並行)・完了・保留・別 pane への spawn・複数タスクの連続実行 (batch) を行う。トリガー: 'タスクを作る/登録', 'タスク一覧', 'タスクに着手', 'タスクを完了', '別 pane で着手/spawn', '複数タスクを順番に処理/連続実行/まとめてやって', '/agent-tasks create|list|start|done|block|spawn|batch' など。"
---

# agent-tasks skill

エージェント (Claude / Codex / ...) に開発させるタスクを、**各コードリポジトリの外**にある
中央ストア `~/agent-tasks-store/` で管理する。リポジトリ内に置かないのは、ブランチごとに
タスク状態がずれるのを避けるため。skill と閲覧用 CLI `agent-tasks` は repo `agent-tasks` に同梱。

## 共通ルール

### バイナリの選択 (herdr / main、意識しなくてよい)

`agent-tasks` というコマンド名は環境で自動的に実体が振り分けられる (agent-tasks/0118)。**herdr 内
(`HERDR_ENV=1`) では herdr 版、tmux 等それ以外では本体版が実行される** (ルーターが PATH 上で解決)。
この skill も1つに集約されている (同目的の skill を2つ入れると選択が不確実になるため)。エージェントは
常に `agent-tasks <サブコマンド>` とだけ打てばよく、バイナリ名の使い分けは通常意識しなくてよい。
**唯一の例外は spawn** (下記) — CLI に相当コマンドが無い経路があるため、SKILL 側で明示的に分岐する。

### エージェント別の注意 (Claude / codex)

この skill と CLI は **agent 非依存**に作ってあり、**Claude / codex 双方から同じ SKILL.md** で使う
(単一の情報源。Claude は `~/.claude/skills/agent-tasks`、codex は `$CODEX_HOME/skills/agent-tasks`
= 既定 `~/.codex/skills/agent-tasks` に、いずれも repo `skills/agent-tasks` への symlink で入る。
`make install` が両方を張る)。タスク管理フロー (create/list/start/spawn/done/block/…) の本体は
どちらでも同じ。**次の一部だけが Claude 固有**なので、codex では読み替える:

- **session-rename (start 手順0)** は Claude Code の `/rename` を発火させる仕組みなので **codex では効かない**。
  codex では**この手順をスキップしてよい** (セッション名の追従が無いだけで、タスク管理自体に影響しない)。
- **session-link の自動検出**は Claude では `CLAUDE_CODE_SESSION_ID` を使うが、**codex でも herdr 内なら
  `agent get` 由来で自動検出される** (agent 中立)。herdr 外で検出できなければ `--session` 明示か cwd 逆引き。
- **statusline / session-hook** の設定スニペットは Claude Code の `settings.json` 用。**codex では未対応**
  (`SESSION` 列は herdr の `agent_status` 由来なので、設定なしでも herdr 内なら working/blocked/idle は出る)。
- **spawn の起動コマンド**は既定 `claude`。codex から別 pane を spawn したいときは環境変数
  `AGENT_TASKS_AGENT=codex` を設定する (初期プロンプトは agent 非依存。`-n` 等の claude 固有フラグは外れる)。

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
# kind: human         # 省略=code (従来型: エージェントが worktree で実装)。human=コードを触らない
                      #   人手タスク (デプロイ設定変更・顧客確認・データ移行など)。human は start で
                      #   worktree を作らず、コンフリクトチェック対象外 (下記 create / start 参照)
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
# PR を作ったとき付ける (複数可。done/review で記録):
# prs:
#   - https://github.com/owner/repo/pull/31
#   - https://github.com/owner/repo/pull/33
# 関連する外部 issue tracker / 課題管理の URL があれば付ける (任意ホスト、複数可):
# tracker:
#   - https://example.com/issues/123
---

# 要件
（タスク内容）

## 進捗ログ
- 2026-06-28 14:30 登録
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
- `## 進捗ログ` の行頭は**日時**で記録する (`date '+%F %H:%M'` の短縮形。例 `2026-06-29 09:24`)。
  日付だけだと同日内に並ぶ複数行の順序や所要が読めないため、分単位まで残す (人が読む履歴なので
  frontmatter の ISO8601 とは別表記でよい)。既存タスクの過去ログは無理に移行せず、新規追記分から日時にする。
- `blocked` のときだけ `blocked_at` (保留にした日時) と `blocked_reason` (理由) を付ける。
  list がこれで「保留からの経過」と理由を表示する。block で設定し、start/done で外す
  (詳細は各操作を参照)。`blocked_at` は ISO8601 日時で `date --iso-8601=seconds` で取得する。
- **PR の URL は `prs:` に YAML リストで持つ** (1 タスクに複数 PR = 分割 PR / 追従修正 もあり得る)。
  PR を `session:` に入れない (`session:` は着手したエージェントのセッション URL 専用)。done/review で
  記録する (下記 done 手順)。`show` が末尾に PR 一覧を表示する (list には出さない)。例:
  ```yaml
  prs:
    - https://github.com/owner/repo/pull/31
    - https://github.com/owner/repo/pull/33
  ```
- **関連する外部 issue tracker / 課題管理の URL は `tracker:` に YAML リストで持つ** (任意ホスト、複数可)。
  `prs:` (PR 専用) とは別枠の汎用フィールドで、どのサービスの URL でも入れられる。登録時や done 時に
  関連 URL があれば記録する。`show` が末尾に一覧を表示し、`doctor` が URL 形式を軽く検査する。例:
  ```yaml
  tracker:
    - https://example.com/issues/123
  ```
- **タスク種別は `kind:` で持つ** (任意)。省略 = **code タスク** (従来型: エージェントが worktree で
  コードを実装する)。`kind: human` = **コードを触らない人手タスク** (デプロイ設定変更・顧客確認・
  データ移行・レビュー依頼など)。human タスクは start で **worktree / branch を作らず**、他タスクとの
  **コンフリクトチェック対象外**になる (コード領域を持たないため。着手側・被チェック側の両方)。
  一覧では `[human]` プレフィックスで識別でき、`--kind human|code` で絞り込める。有効値は `human` か
  `code` (か省略) のみ (それ以外は `doctor` が検出)。詳細は下記 create / start / recommend。

### ユーザーへの報告 — 常に「ID + タイトル」を併記する

タスクの状態をユーザーに報告・言及するとき (着手報告 / 完了・レビュー待ち報告 / 保留報告 / spawn 報告 /
一覧の言及など) は、**タスク ID だけでなくタイトルも必ず併記する**。ID だけだと「何のタスクだっけ？」と
なるため、報告だけで内容が分かるようにする。

- 書式は **`タスク <NNNN>: <タイトル>`** を基本とする (例: `タスク 0033: session マーカーのアトミック書き込み`)。
- 完了/着手などの見出しでも同様に併記する (例: `完了報告 (タスク 0043: タスク状態報告にタスクタイトルを必ず含める)`)。
- タイトルはタスクファイルの frontmatter `title:` をそのまま使う (長い場合も省略せず全文を出す)。
- 各操作 (create / start / done / block / spawn) の報告手順はこのルールに従う。

## 操作の判定

ユーザーの発言から操作を判定する。引数 (`create`/`list`/`start`/`resume`/`done`/`block`/`archive`/`auto-archive`/`unarchive`/`issue`/`sync`/`scaffold`/`recommend`/`batch`) があればそれに従う。

- **create**: 「タスクを作る/追加/登録」「〜というタスク」
- **list**: 「タスク一覧」「タスクの進捗」「何が残ってる」
- **recommend**: 「次に何をやるべき/やればいい」「おすすめ (のタスク)」「next」「recommend」
- **start**: 「〜に着手」「タスク 0001 をやって」「start 0001」 (未着手 = todo から)
- **resume**: 「〜を再開」「保留を解除してやる」「レビュー指摘に対応する」「resume 0001」 (blocked/review から in-progress へ戻す)
- **spawn**: 「別 pane で着手」「新しいセッションで 0001 をやって」「spawn 0001」
- **batch**: 「複数タスクを順番に処理/連続実行」「0042 と 0045 をまとめてやって」「batch 0042 0045」 (直列に start→done。低リスクは自動マージ)
- **done**: 「〜が完了」「done 0001」
- **block**: 「〜を保留」「block 0001」
- **archive**: 「〜をアーカイブ」「もう要らない/やらないので退避」「一覧から消したい (消さずに)」「archive 0001」
- **auto-archive**: 「古い完了タスクを片付けて/整理して」「完了して N 日経ったやつを退避」「auto-archive」 (期間で一括退避)
- **unarchive**: 「アーカイブを戻す/復活」「やっぱりやる」「unarchive 0001」
- **issue**: 「〜を issue にして」「GitHub issue で共有」「issue を立てて/起票して」「issue を更新」「issue 0001」
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
2. `slug` を決める。ユーザー指定があれば英語ケバブケースに変換、なければ確認する。
3. **採番と作成は CLI に任せる (推奨)。** `agent-tasks alloc-id --title` に**フル生成モード**があり、
   project ごとのロック下で「最大連番 + 1」を原子的に確保しつつ、**frontmatter + 本文まで書き込んで**
   くれる。id 依存メタ (`id`/`branch`/`worktree`/`created`/`updated`/`status: todo`) は CLI が採番後に
   埋めるので、こちらは **title と要件本文だけ渡せばよい** (Write ツールを使わない)。要件本文は
   **stdin (ヒアドキュメント) で渡す**。
   ```sh
   agent-tasks alloc-id --slug <slug> --title '<タイトル>' <<'BODY'
   （要件本文。Markdown 可。# 要件 の見出しと ## 進捗ログ は CLI が付けるので不要）
   BODY
   ```
   - stdout に作成したファイルの絶対パスが 1 行返る。**Write / Read は不要** (これが本モードの目的:
     予約済み空ファイルを Write しようとすると Claude Code が `File has not been read yet` で弾く問題を
     回避する)。要件本文をファイルで渡したいときは `--body-file <path>` (`-` で stdin)。
   - **人手タスク (コードを触らない)** のとき (「デプロイ設定を手で変更」「顧客に確認」「本番でデータ移行」
     「レビュー依頼」など、ユーザーが *human/人手/手作業/コードを触らない* と示したとき) は **`--kind human`**
     を付ける (CLI が `kind: human` を書き、`branch`/`worktree` を空にする)。判断に迷うとき (コードを触るか
     曖昧) はユーザーに確認する。既定 (コードを実装するタスク) は `--kind` を付けない (= code)。
   - ストアにアクセスするのは**基本 1 マシン**という前提なので、採番前の pull は不要 (`--pull` は付けない)。
     ストア側に未コミット変更があると `--pull` の `git pull --rebase` が失敗してノイズになるため、
     既定では使わない。同期は別途 `sync` が担う。複数マシンでストアを共有する場合だけ、必要に応じて
     手動で `git pull --rebase` するか `--pull` を付ける (フラグ自体は残っている)。
   - **CLI が無い環境のフォールバック**: 既存 `<root>/<project>/*.md` の最大連番 + 1 を自分で採番し、
     `<root>/<project>/<NNNN>-<slug>.md` を上記形式の中身つきで作る (`status: todo`、`agent`/`session` は空、
     `branch`/`worktree` はファイル名の id・slug に合わせる。人手タスクは `kind: human` を書き
     `branch`/`worktree` は空。`created`/`updated` は `date --iso-8601=seconds`)。この経路は並行時に
     id 衝突があり得るので、手順 4 の doctor 検査を必ず行う。
   - **旧来の空予約モード** (`--title` なしで `path=$(agent-tasks alloc-id --slug <slug>)`) も残っているが、
     返ったパスは未読の空ファイルなので Write が弾かれる。通常はフル生成モード (`--title`) を使う。
4. **作成後に `agent-tasks doctor --project <project>` で重複/不一致を検査する** (alloc-id 利用時も、
   別マシン間衝突などの保険として実行する)。重複 id が出たら、空いている最大連番 + 1 に振り直して
   ファイル名と frontmatter `id:` の両方を直し、再度 doctor が通ることを確認する。
5. 作成したパスを報告して**そこで止まる**。**コードリポジトリには一切コミットしない。**
   報告では作成したタスクを `ID + タイトル` で示す (「ユーザーへの報告」参照)。

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
   - 種別で絞る: `--kind human` (人手タスクのみ) / `--kind code` (従来型のみ)
   - git リポジトリ外で実行した場合は判定不能なので自動で横断にフォールバックする。
2. コマンドが無ければ `<root>/**/*.md` の frontmatter を読み、project / id / status / title を表にして表示する。
   この場合も既定は現在 project (root の basename) のみに絞り、横断したいときだけ全件を出す。

### セッション状態 (working / blocked / idle)

in-progress 行に `SESSION` 列が出て各セッションが **working** (処理中) / **blocked** (承認・許可待ち =
要対応) / **idle** (応答完了・入力待ち) / **ended** (pane 終了) を示す (`?` は判定不能)。並行 pane の
「どれが自分の承認待ちか」を一覧で把握できる。

**状態源は herdr の `agent_status`** (0109)。`list` は herdr の全 agent スナップショット
(session_id → agent_status) を取り、各 in-progress タスクの link (session_id) に突合して状態を出す。
`tmux capture-pane` に頼らず、herdr が alt-screen でも状態を検出する。blocked (承認/許可待ち) と
idle (応答完了) を区別できるのが従来との違い。link はあるが herdr に該当 agent が無ければ ended。
- **フォールバック**: herdr 外 (HERDR_ENV≠1) や link 未記録のときは旧マーカー (session-hook 由来。
  working/waiting/ended) にフォールバックする。導入は `agent-tasks session-hook --print-config` の
  スニペットを `~/.claude/settings.json` に追加 (herdr 内なら不要)。

**突合は `session-link` (session_id ベース) が主経路**。直接 start でも spawn 経由でもセッションの cwd は
メインリポ (worktree の外) なので、start 手順 6 の `agent-tasks session-link` がセッションを紐づける
(自 session_id は CLI が自動検出 = 手順 6 参照)。

設計の切り分け: link の保管・突合は **agent 中立** (CLI 側)。状態の信号源は **herdr** (agent 統合が
検出) に委譲され、agent 中立化した (Claude 固有 hook への依存が減った)。

### 実行中タスクの表示 (status line)

`SESSION` 列が「俯瞰」(どの pane が waiting か) なのに対し、各 pane **自身**に「このセッションが今
どのタスクを実行中か」を Claude Code の status line (プロンプト下部) で常時表示できる。導入は
`agent-tasks statusline --print-config` のスニペットを `~/.claude/settings.json` の `statusLine` に
追加するだけ。Claude Code がセッション情報を JSON で stdin に渡して `agent-tasks statusline` を呼び、
`<project> #NNNN タイトル… [status]` の 1 行を返す。現在タスクの特定は session 状態と同じ情報源
(`session_id` を `session-link` の link で逆引き / 補助で cwd の worktree キー) を再利用するので、
**start 手順 6 の `session-link` が前提**。特定できなければ空表示。これも agent 中立 (CLI 側)。

---

## recommend — 次にやるおすすめタスクの提示

「次に何をやるべき?」「おすすめは?」と訊かれたら、todo と現在の状況をもとに**着手しやすい順に
おすすめを提示する**。判断主体は agent (既存の `agent-tasks` 出力を読んで考える)。専用 CLI は無く、
`list` の結果を使う。**提示のみ。着手はしない** (ユーザーが選んで start/spawn を指示するまで止まる)。

### 手順

1. 候補と現況を集める (既定は現在 project。横断したいときは `--all-projects`):
   ```sh
   agent-tasks --status todo                        # おすすめ候補
   agent-tasks --status in-progress --kind code     # 衝突回避の基準 (今動いている code タスク)
   agent-tasks --status blocked                     # 別枠 (下記)
   ```
   衝突回避の基準は **code タスクだけ**を見る (`--kind code`)。in-progress の human タスクは
   コード領域を持たず衝突しないので、基準から除外する。
2. 各 todo 候補を次の観点で評価する (start のコンフリクトチェックと同じ目線):
   - **in-progress (code) との衝突回避 (最優先)**: in-progress の code タスクの要件・進捗ログを読み、
     触っているファイル/領域を把握する。候補が**それらと重ならない**ほど高評価 (並行で安全)。重なる候補は
     「in-progress 完了後が安全」として見送り側に回す。**候補自身が human タスクなら常に衝突なし**
     (コードを触らない) として高評価にできる。
   - **依存・前提**: 別タスクの完了や**ユーザーの判断**(例: 依存追加=方針決定、設計選択) が前提のものは
     下げる/保留にする。前提が解けていないものは「先に判断が必要」と明示する。
   - **価値 / コスト**: 高価値・低コスト・自己完結 (1 PR で閉じる) を上げる。大規模・要設計は下げる。
   - **blocked は候補から除外**。ただし「解除待ち (何待ちか)」として別枠で一言触れてよい。
3. **上位 2〜3 件**を提示する。各おすすめに**根拠** (なぜ薦めるか: 衝突小/価値/自己完結 など) を必ず添える。
   - 本命 1 件 + 対抗 1〜2 件。さらに**見送り推奨**も理由つきで挙げる (in-progress と衝突する/先に判断が要る)。
   - 横断 (--all-projects) で見た場合はその旨を添える。
4. **そこで止まる**。着手するかはユーザーが決める (「start <id>」「spawn <id>」を待つ)。勝手に start しない。

### 注意
- in-progress が複数あるときは、それぞれの触る領域を踏まえて「どれとも衝突しにくい」候補を優先する。
- 根拠の透明性を重視する (「なぜそれか」「なぜ今これを見送るか」を 1 行で)。

---

## start — 着手 (git worktree で並行開発)

並行実行の肝。**別々のエージェントセッションがそれぞれ別タスクを start することで同時開発できる。**

> **human タスク (kind: human) の start は簡易フロー。** 対象タスクの frontmatter が `kind: human`
> (コードを触らない人手タスク) なら、**worktree / branch を作らず、コンフリクトチェックも行わない**
> (コード領域を持たないため)。具体的には下記のうち **手順 3 (コンフリクトチェック) と手順 4
> (worktree 作成) をスキップ**する。手順 2 の `agent-tasks claim <NNNN>` は human でも実行してよい
> (`status: in-progress` + `started_at` を確定するだけ。worktree は作らない)。手順 5 では
> `branch`/`worktree` は空のままにする。手順 0 の session-rename と手順 6 の session-link は任意
> (セッションを紐づけたいなら実行してよいが、必須ではない)。以降の「実装作業」も無く、人手作業が
> 済んだら `done` にする (worktree 撤去も不要)。**残りの手順は code タスク (kind 省略) の場合。**

0. **まず `agent-tasks session-rename <NNNN>` を叩く (最優先・タスク特定より前)。**
   「start <NNNN>」と指示されたら、**対象タスクの特定・二重着手/コンフリクトチェックより前に**、
   まずこれを 1 回実行する。**Claude セッション自体の名前**を `task <NNNN>: <title>` に変え、claude.ai
   web / スマホアプリのセッション一覧で「今どのタスクか」分かるようにする。
   ```sh
   agent-tasks session-rename <NNNN>
   ```
   - これが id から title を解決し、**自分の pane の入力欄に `/rename task <NNNN>: <title>` を打ち込んで
     発火**させる (Claude はスラッシュコマンドをツールから直接呼べないための回避)。本物の /rename 経路
     なので claude.ai / アプリのセッション名に反映される。経路は環境で自動選択:
     **herdr 内なら `herdr pane send-text` + `pane send-keys Enter`** (tmux 非依存)、herdr 外の tmux 内なら
     `tmux send-keys`、どちらも無ければ `/rename …` 行を出力するのでユーザーが実行する。
   - **なぜ最初か (競合回避)**: 入力欄に打ち込むので、**入力欄が空なうち (指示直後、ユーザーがまだ何も
     打っていない) が最も競合しにくい**。`session-rename` は id を渡すだけで CLI 内で title 解決〜送出まで
     完結する (内容を読まなくてよい) ので、内容取得より前に呼んで問題ない。
   - 打ち込まれた `/rename` はこのターン終了後に実行される (現在の作業は壊さない)。存在しない id なら
     エラーで何もしない (無害)。human タスクでも任意で叩いてよい。spawn で開く子は起動時に `claude -n`
     で自動命名されるので本手順は不要。
1. 対象タスクファイルを特定する (project + id)。frontmatter の `kind:` を見る (human なら上記簡易フロー)。
2. **`agent-tasks claim <NNNN>` で in-progress を原子的に予約する (コンフリクトチェックより前)。**
   これが**二重着手ガード**と **status: in-progress の確定**を project ロック下で一括して行う:
   ```sh
   agent-tasks claim <NNNN> --session <自分の session_id>   # --session は分かれば (任意)
   ```
   ⚠️ `--session` に渡すのは**ローカル session_id (UUID。スクラッチパッドのパス末尾)** で、frontmatter の
   `session:` に入れる**人が開く web URL とは別物**。この UUID を `session:` に貼らないこと (手順 5 参照)。
   - **なぜ最初か (TOCTOU 回避)**: 旧手順は in-progress の確定が worktree 作成の後で、着手指示から確定まで
     長い窓があった。その間に別セッションが同じ project を start すると、双方のコンフリクトチェック (手順3)
     が互いを in-progress と観測できず素通りしていた。claim をチェックより前に置くと窓が「ロック下の一瞬」に
     縮み、並行 start が互いを必ず観測できる。詳細は CLI 側 `claim.go` 冒頭コメント。
   - claim は `status: in-progress` / `agent` / `started_at` (未設定時のみ) / `updated` を書き、
     `blocked_at`/`blocked_reason`/`completed_at` を落とす (blocked 再開・done 再オープンも 1 操作で正す)。
     **frontmatter の既知キーだけを書き換え、本文・進捗ログ・コメントは保全する**。
   - **二重着手ガード (fail-safe)**: 既に `in-progress` なら claim は**エラーで止まる**。別セッションが
     作業中の可能性があるので、ユーザーに確認し、明示の指示 (引き継ぎ/再着手) があるときだけ `--force` を付けて
     再実行する (同一 `--session` の再 start は自動で通る)。
   - **human タスクでも claim してよい** (status を in-progress にするだけ。worktree は作らない)。
   - CLI が無い環境のフォールバック: frontmatter を手で `status: in-progress` 等に更新する
     (この場合レースは残るので手順3のチェックを丁寧に行う)。
3. **コンフリクトチェック**: 同じ project に他の `in-progress` の **code タスク**があるか調べる
   (`agent-tasks --project <project> --status in-progress --kind code`。`--kind code` で human タスクを
   除外する — human はコード領域を持たず衝突しないため。無ければ各ファイルの frontmatter を見る)。
   - 自分は手順2で既に in-progress なので、**他の in-progress = 自分以外**。並行する別 start も同様に自分を
     観測できる。各 in-progress タスクの要件・進捗ログから「触る予定のファイル/領域」を読み取り、本タスクの
     想定変更と重なりそうか判断する。
   - 重なりそうなら **worktree を作る前にユーザーへ伝える**: どのタスク (id/branch) と、どのファイル/領域で
     衝突しそうかを具体的に示し、(a) 先に片方を終えてから進める / (b) 承知の上で並行する のどちらにするか確認する。
   - **ユーザーが「やめる」を選んだら claim を戻す**: `agent-tasks claim <NNNN> --release`
     (todo に戻す。blocked から再開していたなら `--release --to blocked` など戻し先を指定)。
   - 重なりが無さそうなら、その旨を一言添えてそのまま進める。
   - **本タスク自身が human のときはこの手順ごとスキップ** (上記簡易フロー)。
4. worktree とブランチを用意する (**冪等**)。**human タスクではこの手順ごとスキップ** (worktree を
   作らない)。**worktree の作成は start の責務** — 直接 start でも
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
5. **残りの frontmatter を更新する** (status/agent/started_at は手順2の claim が済ませている):
   - `branch: task/<NNNN>-<slug>` / `worktree: ../<project>--<NNNN>`
     (**human タスクは worktree を作らないので `branch`/`worktree` は空のまま**にする)
   - `session:` 自分のセッションの **web URL** が分かれば記録する (Claude Code なら会話フッタの
     `Claude-Session` = `https://claude.ai/code/session_…`)。**必ず `https://` で始まる URL を入れる**。
     - ⚠️ **claim / session-link の `--session` に渡すローカル session_id (UUID) を `session:` に貼らない。**
       UUID (例 `0e177131-…`) は人が開けるリンクではなく、両者は別物 (混同しやすいので注意)。
     - **web URL が取れなければ空のままでよい** (UUID を残すより空が「人が開く URL」の定義に忠実)。
       `doctor` が「session: が URL 形式でない」を検出する。
     - なお `session-link` が web URL を自動記録できる環境 (`CLAUDE_CODE_BRIDGE_SESSION_ID` あり) では、
       手順 6 の session-link がここを埋めるので手で書かなくてよい。
   - `## 進捗ログ` に「着手」を追記 (`updated` は claim が更新済みだが、追記に合わせて再更新してよい)
   - これらは本文/ログの整形を含むので skill 側で行う (claim はクリティカルな status 確定だけを担う)。
6. **セッション状態を紐づける** (任意・冪等。session 状態 hook を導入している場合のみ意味がある)。
   これで `list` の `SESSION` 列に working/waiting が出る。**直接 start でも spawn 経由でも、セッションの
   cwd はメインリポ (worktree の外) なので、この紐づけが追跡の主経路**になる (cwd=worktree に依存しない)。
   **通常は `--session` を省略してよい (CLI が自 session_id を自動検出する)**:
   ```sh
   agent-tasks session-link <NNNN>   # コードリポジトリ root で実行
   ```
   CLI は自 session_id を次の優先順で自動検出する (0110): **`CLAUDE_CODE_SESSION_ID` env
   (Claude Code が export。herdr 内外で有効) → herdr `agent get` (HERDR_ENV=1 のとき) → cwd 逆引き**。
   出力に検出経路 (`(CLAUDE_CODE_SESSION_ID)` 等) が出る。これでスクラッチパッドのパスから
   session_id を抜く裏技は不要になった。
   - **明示したいとき (任意)**: 自動検出が効かない環境や、別 session_id を紐づけたい特殊ケースは
     `--session <id>` で上書きできる (明示が最優先)。スクラッチパッドのパス
     `…/<session_id>/scratchpad` 末尾から取った id を渡すのはこの経路 (最終フォールバック)。
     ```sh
     agent-tasks session-link <NNNN> --session <自分の session_id>
     ```
   - worktree に `cd` してから呼ぶと cwd 逆引き経路がずれるので、root で実行する。
   - セッションを特定できなくてもエラーにはならない (案内が出るだけ)。
   - (セッション名の変更 `session-rename` は**手順 0** で既に実行済み。ここでは扱わない。)
   - **他の agent (codex 等)**: `CLAUDE_CODE_SESSION_ID` を持たなくても、herdr 内なら `agent get` 由来で
     自動検出される (agent 中立)。それも無ければ `--session` 明示か cwd 逆引き。
7. **以降の実装作業は作成した worktree (`../<project>--<NNNN>`) の中で行う。** 元のチェックアウトは汚さない。
8. プロジェクトの作法に従って実装する (`CLAUDE.md` / `AGENTS.md` を読む)。完了に近づいたら `done` へ。
   - 着手をユーザーに報告するときは `ID + タイトル` で示す (「ユーザーへの報告」参照)。

---

## resume — 作業再開 (blocked / review → in-progress)

一度 `blocked` (保留) / `review` (レビュー待ち) にしたタスクを再び触るとき、status を `in-progress` に
戻す。**start との違い**は「既に着手済み = worktree が残っている」前提であること: start は未着手 (todo) の
タスクに worktree を新規作成するが、resume は**既存の worktree の中で作業を再開するだけ**なので worktree は
作らない。手順は start と対称で、**セッション同一化 (session-rename + session-link) をセットで行う**
(resume は「別セッションでの再開」が主局面なので、pane 名の追従と session_id の紐づけが start と同じく要る)。

> **todo / done は resume ではない。** 未着手 (todo) は `start` (worktree を作る)。完了済み (done) の
> 再開は worktree を撤去済みなので作り直しが要る → `start` を使う。CLI の `resume` はこの 2 つを
> エラーで弾いて誘導する。

0. **まず `agent-tasks session-rename <NNNN>` を叩く (start 手順 0 と同じ・最優先)。**
   別セッションで再開する場合、この pane 名をタスク名に追従させる。理由・経路は start 手順 0 と同じ
   (入力欄が空なうちに送るのが最も競合しにくい)。
1. 対象タスクを特定する (project + id)。frontmatter の `status` が `blocked` / `review` であることを確認する
   (`kind: human` の人手タスクも同様に resume してよい。worktree は元々持たない)。
2. **`agent-tasks resume <NNNN>` で in-progress に戻す**:
   ```sh
   agent-tasks resume <NNNN>            # blocked/review → in-progress。started_at は保持
   agent-tasks resume <NNNN> --agent <name> --session <url>   # 引き継ぎ時など (任意)
   ```
   - `status: in-progress` / `agent` (未指定なら既存を保持) / `updated` を確定し、`blocked_at` /
     `blocked_reason` / `completed_at` を落とす。`started_at` は初回着手のまま保持する (再開では変えない)。
   - todo / done を渡すとエラーで `start` へ誘導される (上記)。CLI が無い環境のフォールバックは frontmatter を
     手編集する (`status: in-progress` にし、`blocked_at`/`blocked_reason` を削除、`updated` を更新)。
3. `## 進捗ログ` に「再開」を 1 行追記する (何を機に再開したか。これは skill が行う)。
4. **セッション状態を紐づける** (start 手順 6 と同じ): `agent-tasks session-link <NNNN>` を
   コードリポジトリ root で実行する (別セッションでの再開でも worktime が合算されるよう、再開セッションの
   session_id を必ずリンクする)。CLI が自 session_id を自動検出する (詳細は start 手順 6)。
5. **既存の worktree (`../<project>--<NNNN>`) の中で作業を続ける。** worktree は作らない (既に在る)。
   万一 worktree が消えている場合だけ、start 手順 4 と同じ手順で作り直す。
6. 再開をユーザーに報告するときは対象タスクを `ID + タイトル` で示す (「ユーザーへの報告」参照)。

---

## spawn — 別 pane で新セッションを開いて start させる (herdr / tmux 両対応、fire-and-forget)

spawn は「**別 pane を開いてそこで新セッションに start させるだけ**」。重い per-task オーケストレーション
ではなく、**親は pane を開いて指示を送ったら忘れてよい**。worktree の作成・session-link・frontmatter
確定はすべて子の `start` がやる (start が worktree ライフサイクルの唯一の所有者)。spawn ＝「別 pane で
start」と考えればよい。

**経路は環境で分岐する** (agent-tasks/0118)。herdr 版 CLI には `agent-tasks spawn` (herdr `agent start`
経由) があるが、本体版 (tmux 環境) にはこれに相当するサブコマンドが無い (旧来どおり tmux コマンドを
直接使う)。そのため、他の手順と違いここだけは**エージェントが env を見て手順を選ぶ**必要がある。

### 手順

1. 対象タスクを特定する (project + id)。
2. 環境を確認して分岐する: `$HERDR_ENV` = `1` → 3a、そうでなく `$TMUX` が設定済み → 3b、
   どちらでもない → 3c (フォールバック出力)。

#### 3a. herdr 内 (`HERDR_ENV=1`)

```sh
agent-tasks spawn <NNNN>                # 現在 project。別 project は spawn <project> <id>
agent-tasks spawn <NNNN> --split right  # 分割方向 (既定 down)。--focus で前面起動
```

CLI が内部で行うこと (agent は意識しなくてよい):
- **メインリポ root で pane を開く** (worktree 内から実行しても `git-common-dir` で main root に解決)。
  worktree はまだ無い (子の start が作る) ので cwd は main root。追跡は session-link (session_id ベース)
  なので cwd が worktree でなくてよく、子が done で worktree を消しても親の足元を消さない (安全)。
- **二重着手ガード**: 対象が `in-progress` + `session` ありなら「別セッション作業中かも」とエラーで止まる。
  引き継ぐ/再着手すると分かっているときだけ `--force` を付けて再実行する (ユーザーに確認してから)。
- **子の起動**: `herdr agent start "task <NNNN>: <title>" --cwd <main root> --split <dir> --no-focus --
  <AGENT> …` を実行。AGENT は `AGENT_TASKS_AGENT` (既定 claude)。claude は `-n` でセッション名を付け、
  初期プロンプト「タスク <NNNN> に着手して」を渡す (他 agent は `-n` を外す。agent 非依存)。

#### 3b. tmux (herdr 外、`$TMUX` あり)

**二重着手チェック**を先に行う (CLI 側の保証が無いため手動): `status` が `in-progress` で `session` が
埋まっていれば別セッション作業中の可能性。ユーザーに確認し、明示の指示がなければ中止する。

**メインリポ root で** pane を開き、子セッションを起動して start を指示する。開いた pane の id を
取得し (`-P -F '#{pane_id}'`)、`send-keys` はその id を明示ターゲットにする (target 省略だと別 pane に
送られる事故が起きる):

```sh
ROOT="$(git rev-parse --show-toplevel)"      # メインリポ root (worktree 内から実行しても可)
AGENT="${AGENT_TASKS_AGENT:-claude}"          # agent 非依存。既定 claude、env で上書き可
PANE=$(tmux split-window -h -P -F '#{pane_id}' -c "$ROOT")
# Claude Code は -n で起動時にセッション名を付けられる (web/アプリのセッション一覧で
# どのタスクか分かる)。<title> は対象タスクの title を入れる。-n は Claude Code 固有なので
# 他 agent (codex 等) では非対応なら外す。
tmux send-keys -t "$PANE" "$AGENT -n 'task <NNNN>: <title>' 'タスク <NNNN> に着手して'" Enter
echo "spawned pane: $PANE (cwd: $ROOT)"
```

うまく起動しないとき (claude の初回起動が遅い/プロンプト未達) は `tmux select-pane -t "$PANE"` で覗き、
必要なら `tmux send-keys -t "$PANE" "$AGENT 'タスク <NNNN> に着手して'" Enter` を再送する。

#### 3c. フォールバック出力 (herdr/tmux どちらでもないとき)

副作用を出さず、ユーザーが別ターミナルで手実行できる形を表示する (メインリポ root で起動):

```sh
cd "$(git rev-parse --show-toplevel)"
# -n で起動時にセッション名を付ける (<title> は対象タスクの title)。子が
# /agent-tasks start <NNNN> を実行し worktree も作る。
claude -n 'task <NNNN>: <title>' 'タスク <NNNN> に着手して'
```

### 共通 (3a/3b とも)

**親はここで完了**。worktree 作成も worktree-init もポーリングもしない。子の `start` が
worktree 作成・作成後フック・session-link・frontmatter 確定まで行う (start 手順参照)。
- 初期プロンプト「タスク <NNNN> に着手して」は agent 非依存 (codex 等は将来対応、今は claude 既定)。
- 起動を確認したいときだけ、`agent-tasks --watch --status in-progress` で SESSION 列を眺めれば
  子が start を終えた時点で in-progress / working として現れる (能動ポーリングは不要)。
- spawn したことをユーザーに報告するときは対象タスクを `ID + タイトル` で示す (「ユーザーへの報告」参照)。

---

## scaffold — worktree 設定をプロジェクトに展開

プロジェクトを並行開発対応にする一度きりのセットアップ。スタック (firebase / rails / ...) を検出し、
推奨の `.worktreeinclude` / `.worktree-post-create` / `.worktree-post-remove` を
**プロジェクト root に書き出す**。以降は start/spawn の作成後フック (worktree-init) と done の
撤去フック (worktree-remove) がそれを適用する。

1. `agent-tasks scaffold-worktree [<stack>]` を実行する (コードリポジトリ root で):
   - スタック自動検出 (firebase.json/.firebaserc → firebase、bin/rails 等 → rails)。検出できなければ
     `<stack>` を指定するか `--list` で候補を見る。
   - 既存の設定ファイルは上書きしない (`--force` で上書き)。別ディレクトリは `--dir <path>`。
2. 生成された 3 ファイルを**ユーザーと確認・調整**する (ポート計算・依存コマンド・コピー対象・
   後始末対象はプロジェクト固有なので、テンプレはあくまで叩き台)。post-create と post-remove の
   CONFIG・DB 名の導出は**必ず揃える** (ずれると後始末対象がずれて DB が孤児化する)。
3. 問題なければコードリポジトリにコミットする (これは**コードリポジトリ側**の変更。ストアではない)。

> テンプレはバイナリに同梱 (`templates/<stack>/`)。スタックを増やすときは
> `templates/<新stack>/{worktreeinclude,post-create,post-remove}` を足すだけ (必要なら detectStack も拡張)。

---

## 作成後フック (worktree-init) / 撤去フック (worktree-remove) の設定

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

- **`.worktree-post-remove`** — post-create の**対**。`done` の `agent-tasks worktree-remove` が
  worktree 撤去の**直前** (worktree がまだ在るうち) に worktree 内で実行する。post-create が worktree
  ごとに作った外部副作用 (worktree 固有 DB / puma-dev 登録など) の後始末を書く (`dropdb` / `pdl unlink` 等)。
  渡る環境変数は post-create と同じ (`AGENT_TASKS_WORKTREE` / `AGENT_TASKS_MAIN` / `AGENT_TASKS_PROJECT`)
  なので、**同じ決定的オフセットで DB 名などを再計算**できる (CONFIG と導出は post-create と揃える)。例:
  ```sh
  #!/bin/sh
  WT_NAME=$(basename "${AGENT_TASKS_WORKTREE:-$PWD}")
  DB_SUFFIX=_$(printf '%s' "$WT_NAME" | tr -c 'a-zA-Z0-9' '_')
  dropdb --if-exists "myapp_development$DB_SUFFIX"   # post-create と同じ命名で後始末
  ```

冪等性: コピーは既存ファイルを上書きしない。post-create は worktree ごとのマーカーで一度だけ実行される
(`agent-tasks worktree-init <dir> --force` で再実行)。post-remove はマーカーを持たず、撤去のたびに
1 度だけ走る (撤去は 1 回なので二重実行ガードは不要)。

> これら 3 ファイルはスタック (firebase / rails) ごとに `scaffold` で雛形を生成できる
> (上記「scaffold」参照)。worktree-init / worktree-remove はその設定を**実行するだけ**の汎用機構。

---

## done — 完了

> **human タスク (kind: human) の done は簡易。** worktree もコミットも無いので、手順 2〜4・6 を
> スキップする。人手の作業が済んだことを確認したら、frontmatter を `status: done` +
> `completed_at` にし (手順 5)、`## 進捗ログ` に完了内容を 1 行残すだけ。撤去する worktree は無い。
> 関連 URL があれば `tracker:` に、成果に PR があれば `prs:` に記録してよい。

1. 対象タスクファイルを特定する (`kind:` を見る。human なら上記簡易フロー)。
2. worktree 内で最終確認 (型/Lint/テストなど、プロジェクトの作法に従う)。
3. 変更をコミットする (worktree 内で。コミット先はコードリポジトリ)。
4. ユーザーが PR を望めば作成する (`gh pr create`)。PR 待ちの段階なら `status: review`、マージまで完了したら `status: done`。
5. **frontmatter の status 確定は `agent-tasks done` に任せる** (claim が start でやるのと同じ。
   completed_at 付け忘れや blocked_* の消し忘れ・日付推測ミスを避けるため、クリティカルな scalar キーは
   CLI が project ロック下で決定的に確定する。**本文・進捗ログ・コメントは保全される**):
   ```sh
   agent-tasks done <NNNN>            # status: done + completed_at (未設定時のみ) を確定、blocked_* を落とす
   agent-tasks done <NNNN> --review   # status: review にする (completed_at はまだ付けない)
   ```
   - PR 待ちの段階は `--review` (`status: review`)、マージまで完了したら `--review` 無し (`status: done`)。
     `completed_at` は done になった時点で初回のみ記録される (再 done で上書きしない)。blocked から直接
     完了/レビューへ抜ける場合の `blocked_at`/`blocked_reason` 削除も CLI が行う。
   - **PR を作成したら URL を `prs:` に追記する** (YAML リスト。複数 PR なら各行に足す)。`prs:` は
     ブロックリストなので **CLI ではなく skill が frontmatter に追記する** (done コマンドは scalar キー専用)。
     `session:` には入れない (`session:` は着手セッション URL 専用)。`show` が末尾に PR 一覧を表示する。例:
     ```yaml
     prs:
       - https://github.com/owner/repo/pull/31
     ```
   - **`## 進捗ログ` への対応内容の追記は skill が行う** (CLI は frontmatter のみ)。`updated` は CLI が更新済み。
   - **`done` は完了直後にそのタスク 1 件へ軽量な整合チェック (doctor と同じ検査) を走らせ、矛盾があれば
     stderr に警告を出す** (`started_at`/`completed_at` の欠落・逆転、`blocked_*` のクリア漏れ)。**警告が
     出たらユーザーに報告する** (例: `started_at` が無いまま `completed_at` が付いた = claim=start を経ずに
     done した可能性。必要なら frontmatter を直す)。警告が出ても done 自体は成功している。
   - CLI が無い環境のフォールバック: frontmatter を手編集する (`status`/`completed_at`/`updated` を更新し、
     blocked から抜けるなら `blocked_at`/`blocked_reason` を削除)。
   - 完了 / レビュー待ちをユーザーに報告するときは対象タスクを `ID + タイトル` で示す (「ユーザーへの報告」参照)。
6. `done` まで来たら worktree を撤去する。**メインリポ root から実行する** (start/spawn とも
   セッションの cwd はメインリポなので、対象 worktree は別ディレクトリ＝安全に消せる)。
   **`agent-tasks worktree-remove` を使う** (撤去前フックを流してから `git worktree remove` する):
   ```sh
   agent-tasks worktree-remove ../<project>--<NNNN>
   ```
   - これは worktree-init (作成後フック) の対。撤去の**直前**に、worktree がまだ在るうちに
     コードリポジトリ root の **`.worktree-post-remove`** を実行してから撤去する。post-create が
     worktree ごとに作った副作用 (worktree 固有 DB / puma-dev 登録など) の後始末がここで走る
     (フックが無ければ撤去だけ。詳細は下記「撤去フック」)。
   - **未コミット変更があると `git worktree remove` が失敗する** (CLI がエラーを返す)。意図的に捨てる
     ときだけ確認のうえ `--force` を付ける。フックがエラー終了したときも撤去を中止する (後始末を
     直して再実行するか、無視するなら `--force`)。フックだけ流したいときは `--hook-only`。
   - ⚠️ **自分の cwd がその worktree の中にあるセッションでは撤去しない** (cwd ごと消えて以降の hook が
     `ENOENT posix_spawn` で壊れる)。`worktree-remove` は cwd が対象 worktree 内だと**自動で中止**して
     エラーにするが、通常フローでは cwd=メインリポなので起きない。worktree 内で直接起動したセッションの
     場合は、撤去を**メインリポ / 別セッションから**行う (または撤去せず残す)。
   - CLI が無い環境のフォールバック: 手動で後始末してから `git worktree remove ../<project>--<NNNN>`。

---

## block — 保留

1. 対象タスクファイルを特定する。
2. **frontmatter の保留確定は `agent-tasks block` に任せる** (done と同じく scalar キーを CLI が
   決定的に確定する。日時取得・`blocked_at`/`blocked_reason` の付け忘れを避ける。本文・進捗ログは保全):
   ```sh
   agent-tasks block <NNNN> --reason "<保留理由>"
   ```
   - `status: blocked` + `blocked_at` (現在時刻) + `blocked_reason` + `updated` を確定する。
     `blocked_at` は list が「保留からの経過」を測る基点 (`updated` ではなくここから測る)。
   - `--reason` は必須 (一覧で title に添えて表示される。何の判断/確認待ちか 1 行で)。
   - CLI が無い環境のフォールバック: frontmatter を手編集する (`status: blocked` / `blocked_at`
     (`date --iso-8601=seconds`) / `blocked_reason` / `updated`)。
3. `## 進捗ログ` にも**保留理由**を追記する (frontmatter は「現在の理由」、ログは履歴。これは skill が行う)。
4. worktree は残す (再開できるように)。判断材料が揃ったら `start` で再開、または直接実装を続ける。
   **再開・完了で blocked を抜けるときは `blocked_at` / `blocked_reason` を削除する** (start / done 参照)。
5. 保留をユーザーに報告するときは対象タスクを `ID + タイトル` で示す (「ユーザーへの報告」参照)。

---

## archive / unarchive — アーカイブ (退避と復帰)

不要になった (やらないと決めた / 重複 / 陳腐化した) タスクを**削除せず退避**する。`done` (やり切った)
とは別で、「もう見なくてよいが残しておきたい」状態。退避すると `<project>/archive/<NNNN>-<slug>.md`
へ移り、**通常の `list` / `-a` / `doctor` から外れる** (明示的に `--archived` を付けたときだけ見える)。

> **block との違い**: block は「いずれやる、今は止まっている」。archive は「やらない / もう見ない」。
> 迷ったら `block` (理由付きで保留) を勧める。archive は本当に一覧から外したいときだけ。

### archive — 退避

1. 対象タスクを特定する (project + id)。**in-progress (作業中・worktree あり) のタスクは原則退避しない** —
   作業を畳むなら先に done / block する。ユーザーが承知のうえで退避を望むなら進めてよい
   (worktree は CLI では消えないので、不要なら別途 `git worktree remove` を案内する)。
2. 退避理由を `## 進捗ログ` に 1 行追記しておくと、後で `--archived` で見たとき分かりやすい
   (例: `2026-07-01 10:00 アーカイブ (重複: 0042 に統合)`)。frontmatter の `status` は変えない
   (退避は状態変更ではない)。
3. CLI で退避する (ファイルを `archive/` へ移動):
   ```sh
   agent-tasks archive <project> <id>     # 現在 project 内なら project 省略可
   ```
   出力の `from:` / `to:` は store からの相対パス (リネームの旧/新)。これを scoped sync に渡す。
4. ストアを同期する (リネームの旧パス削除 + 新パス追加を両方 stage):
   ```sh
   agent-tasks sync --path <from> --path <to>    # 出力の from:/to: をそのまま渡す
   ```
   (まだ一度も sync していない新規タスクで scoped が失敗する場合のみ、全体 `agent-tasks sync` で同期する。)
5. 退避をユーザーに報告するときは対象タスクを `ID + タイトル` で示す (「ユーザーへの報告」参照)。

### auto-archive — 期間で一括退避 (古い完了タスクの片付け)

**完了後に一定日数が経過した done タスク**をまとめて `<project>/archive/` へ退避する
(一覧に古い完了タスクが溜まって見づらくなるのを防ぐ)。単一退避の archive と違い、id ではなく
**期間で対象を選ぶ**。破壊的操作なので、**まず `--dry-run` で対象を確認してから**実行する。

- **対象**: `status: done` かつ `completed_at` が閾値より古いタスクだけ。review / in-progress や
  `completed_at` 無しの done は対象外 (期間判定できないため)。
- **閾値**: `--older-than <days>` (既定 30)。スコープは list と同じ規則
  (`--project` / `--projects` / `--all-projects`、既定は現在 project)。

1. **まず `--dry-run` で対象を確認する** (移動しない):
   ```sh
   agent-tasks auto-archive --dry-run                 # 現在 project、既定 30 日超
   agent-tasks auto-archive --older-than 60 --dry-run # 60 日超に変更
   agent-tasks auto-archive --all-projects --dry-run  # 全 project 横断
   ```
   対象一覧 (project/id・完了日・経過・タイトル) をユーザーに提示し、**実行してよいか確認する**。
2. 合意できたら `--dry-run` を外して実行する:
   ```sh
   agent-tasks auto-archive [--older-than <days>] [--project <p>|--all-projects]
   ```
   退避した各タスクを archive と同じ `from:` / `to:` (store 相対パス) 形式で出力する。
3. **出力された全 `from:` / `to:` をまとめて scoped sync に渡す** (複数 path を一度に stage できる):
   ```sh
   agent-tasks sync --path <from1> --path <to1> --path <from2> --path <to2> ...
   ```
   件数が多く煩雑なら、区切りとして全体 `agent-tasks sync` を一言確認のうえ使ってもよい。
4. 途中で一部の退避に失敗しても、成功した分の `from:`/`to:` は出力済みなので**先にそれを同期**し、
   失敗分 (末尾に表示) は理由を確認して対処する (残りは best-effort で試行済み)。
5. 一括退避したことをユーザーに報告するときは件数と、必要なら主な対象を `ID + タイトル` で示す。

> **定期実行したい場合**: 常駐は持たず、コマンド一発で完結するので cron などから
> `agent-tasks auto-archive --older-than <days>` を定期的に呼べばよい (その場合は sync も併せて自動化する)。

### unarchive — 復帰

1. 対象タスクを特定する。退避済み一覧は `agent-tasks list --archived` (`--all-projects` で横断) で見られる。
2. CLI で戻す:
   ```sh
   agent-tasks unarchive <project> <id>
   ```
   戻し先に同 id のアクティブタスクがあるとエラーになる (番号は再利用しない方針なので通常起きない)。
3. 出力の `from:` / `to:` を使って同期する: `agent-tasks sync --path <from> --path <to>`。
4. 復帰をユーザーに報告するときは対象タスクを `ID + タイトル` で示す。

### 閲覧

- 一覧: `agent-tasks list --archived` (通常の一覧には出ない。`--project` / `--all-projects` と併用可)。
- 全文: `agent-tasks show <project> <id> --archived`。
- 採番・doctor: 退避済みの番号も `alloc-id` の採番に算入され (再利用しない)、`doctor` の id 重複検査も
  アクティブとアーカイブを横断して点検する (戻すときに番号が衝突しないことを担保)。

---

## issue — GitHub issue 連携 (タスク内容を他の開発者と共有)

ストアは private で人に見せづらいので、**共有したいタスクだけ**を GitHub issue として起票し、
本文を共有する。**store → issue の一方向**で、起票後の本文変更も `issue` を再実行すれば issue 側へ
反映できる (issue 側の編集を store に取り込む双方向同期はしない)。**明示操作のみ**で、自動連携はしない。

- frontmatter の `issue:` に issue URL を 1 つ記録する (1 タスク 1 issue)。**記録は CLI が行う**
  (起票後に `issue:` を書き込む) ので、agent が frontmatter を手編集する必要はない。
- issue 本文は frontmatter (branch/worktree/session 等の内部メタ) を除いた **Markdown 本文**だけを送る。
- **`gh` (GitHub CLI) が必要**。未導入/未ログインならコマンドがその旨を案内するので、ユーザーに伝える。

### 手順

1. 対象タスクを特定する (project + id)。共有してよい内容かをユーザーに確認する
   (機密・社内固有情報が本文に含まれていないか。含むなら起票前に本文を一般化する)。
2. **作成先 repo を決める**。CLI はストアから起動されると repo を知らないので、次のどちらかにする:
   - **対象のコード repo 内で実行する**と cwd から `gh` で repo を推論する (推論結果が project 名と
     食い違うときは取り違え防止で停止する)。
   - もしくは `--repo owner/repo` を明示する (ストア内などコード repo の外で実行するときはこちら)。
3. 起票する (未連携なら作成、連携済みなら本文を更新):
   ```sh
   agent-tasks issue <project> <id> [--repo owner/repo]
   ```
   出力に作成/更新した issue の URL が出る。新規作成時は `from:` に scoped sync 用の store 相対パスが出る。
4. **新規作成したとき**は frontmatter (`issue:` / `updated`) が書き換わるので、ストアを同期する:
   ```sh
   agent-tasks sync --path <from>       # 出力の from: をそのまま渡す
   ```
   (更新だけのときは frontmatter は変わらないので sync 不要。)
5. `## 進捗ログ` に「issue 起票/更新 (URL)」を 1 行残しておくとよい。
6. 連携をユーザーに報告するときは対象タスクを `ID + タイトル` で示し、issue URL を添える。

### 閲覧
- `agent-tasks show <project> <id>` の末尾に `issue: <URL>` が出る (`--json` にも `issue` フィールド)。
- `doctor` は `issue:` が URL 形式かを軽く検査する。

---

## batch — 複数タスクの連続実行 (直列、低リスクは自動マージ)

「気軽にマージしてよい」プロジェクトで、**複数タスクを 1 セッションで順番に処理する**モード。
spawn の並行 (別 pane を増やす) とは別物で、本モードは **1 worktree ずつ start→実装→done を直列に**
回す。判断主体は agent (専用 CLI は持たず、既存の `agent-tasks` 出力と各操作手順を使う)。
領域が衝突するタスクを安全に流す用途にも向く (並行だと衝突するものを直列化する)。

> **暴走させない。** batch は「合意したキューを順に流す」だけ。**開始前の認識合わせ (手順 1〜2) を
> 必ず通す**。合意なしにいきなり実装・マージへ進まない。各タスクの実体作業は既存の start/done
> 手順をそのまま使う (batch はそれを順に呼ぶオーケストレーションにすぎない)。

### 1. 開始前の認識合わせ (ここで必ず止まって合意する)

1. **対象キューを確定する**: ユーザー指定の id 群 (例 `batch 0042 0045 0047`)。順序の希望が無ければ
   agent が提案する (依存・衝突の少ない順)。recommend の評価観点 (in-progress との衝突回避 / 依存・前提 /
   価値・コスト) を流用してよい。
2. **このプロジェクトが「自動マージ可」かを確認する**: 設定ファイルは持たない方針なので、**batch 開始時に
   都度ユーザーへ確認する** (「このプロジェクトは低リスクなら自動マージしてよいか?」)。可と合意できた場合のみ
   手順 3 の自動マージを行う。否なら全タスク review 止まり (人がマージ) にする。
3. **各タスクのリスクを評価し、自動マージ対象を仕分ける**。評価の目安 (start のコンフリクトチェックと同目線):
   - **低リスク (自動マージ候補)**: 変更が局所的・自己完結 (1 PR で閉じる)、テスト/型チェックがある、
     他タスクや既存コードと領域が重ならない、後方互換を壊さない。
   - **高リスク (自動マージから外す)**: 広範囲・横断的変更、設計判断や依存追加を伴う、テストが薄い、
     他の (キュー内/in-progress) タスクと領域衝突、破壊的・不可逆。判断に迷うものは**高リスク扱い**にする。
   - 仕分け結果 (どれを自動マージ、どれを review 止まり) を**ユーザーに提示して合意を取る**。
4. **各タスクで事前に確認したいことがあれば質問する**。出た Q&A は**そのタスクファイルに追記**して
   着手時に迷わないようにする (`## 確認したいこと` があれば回答を追記、無ければ `## 進捗ログ` に
   「batch 事前確認: <Q> → <A>」の形で残す)。
5. 合意した **実行計画 (順序 / 各タスクの auto-merge or review / 事前 Q&A)** を一度ユーザーに示してから
   手順 6 へ進む。ここまでは**実装・worktree 作成をしない**。

### 2. 順次実行 (合意した順に 1 つずつ)

6. キューの先頭タスクから、既存の **start 手順**で着手する (worktree 作成・session-link・frontmatter 更新)。
   - batch は **1 セッションを使い回す**ので、**各タスクの処理に入ったらまず** start 手順 0 の
     **`agent-tasks session-rename <NNNN>`** を呼ぶ (タスク内容取得・チェックより前)。スマホアプリの
     セッション名が「今処理中のタスク」に追従する (使い回しでも名前が古いままにならない)。理由は
     start 手順 0 と同じ (入力欄が空なうちに送るのが最も競合しにくい)。
7. プロジェクトの作法に従って実装し、**done 手順**で最終確認 (型/Lint/テスト) → コミット → PR 作成。
   - **低リスクと合意したタスク**: PR を作成し、CI 等が通れば**自動マージ**まで進める。マージは
     `gh pr merge --auto --squash` (リポジトリの作法に合わせる) で **CI green を待ってからマージ**する
     (`--auto` 非対応なら CI 確認後に手動 `gh pr merge`)。マージ後、frontmatter を `status: done` +
     `completed_at` にし、**メインリポ root から worktree を撤去**する (done 手順 6)。
   - **高リスクと合意したタスク**: PR 作成までで止め `status: review` にする (worktree は残す)。
     **マージはユーザーに委ねる** (理由を添えて「要確認」と報告)。`completed_at` はまだ付けない。
8. **1 タスクごとに結果を報告**してから次へ進む (`ID + タイトル` 併記、成否・マージ可否・PR URL を添える)。

### 3. 中断・再開

9. あるタスクで**失敗・要相談・想定外の衝突**が起きたら、**そのタスクで止まってユーザーに相談する**
   (勝手に高リスク化して飛ばさない)。残りのキューは保持し、ユーザーの指示で「このタスクを block して
   次へ」「ここで batch を中断」などを選ぶ。
10. 最後まで流したら、**処理したタスクの一覧 (done / review / block の別)** をまとめて報告する。
    ストア更新を伴うので、区切りで `sync` を一言促す (勝手に push しない)。

> 自動マージの可否・順序・リスク仕分けはすべて**手順 1〜2 の合意が根拠**。合意に無い自動マージは
> しない。CI が無い/通らないリポジトリでは「低リスク」でも自動マージせず review 止まりにする。

---

## sync — ストアの同期 (git commit & push)

タスクファイルはコードリポジトリの外 (`~/agent-tasks-store`、git 管理) にある。
create/start/done/block でファイルを更新したあと、ストアを commit & push してマシン間で同期する。

- **未同期の確認**: `agent-tasks status` でストアの未コミット/未 push の状況を1行で確認できる
  (例: `未コミット 3 ファイル / 未 push 2 コミット (origin/main)`、同期済みなら `クリーン (同期済み)`)。
  未同期があれば exit 1 を返すので、「sync が必要か」を事前に判断したいときに使う。
- **基本は CLI に任せる**: `agent-tasks sync` がストアで `add` → コミットメッセージ自動生成 →
  `commit` → `pull --rebase --autostash` → `push` まで行う。push したくない時は `agent-tasks sync --no-push`
  (commit で止める)。upstream 未設定なら初回 `push -u origin <branch>` で追跡を設定する。
  並列セッションの sync はストアロックで直列化され、push 競合 (non-fast-forward) は取り込み直して
  自動リトライする。
- **scoped sync (推奨。並列で安全)**: `agent-tasks sync <id>` (または `sync <project> <id>` /
  `sync --path <相対パス>`) は**そのタスクファイルだけ**を stage・commit する。引数なし `sync` は
  従来どおり**全体** (`add -A`)。**並列で別セッションが他タスクを書きかけのときは scoped を使う**
  (`add -A` は書きかけを巻き込むため)。
- コミットメッセージは変更ファイルから自動生成される (例: `tasks: agent-tasks/0005 (in-progress)`、
  複数なら `tasks: update N tasks` + 本文に列挙)。
- **いつ実行するか**:
  - **scoped sync (`sync <id>`) は確認なしで自動実行してよい**。create/start/done/block で 1 タスクを
    更新した直後など、自分が触ったタスクだけを同期する用途。ロック + scoped add で並列セッションと
    干渉しないため、毎回ユーザーに確認しなくてよい。
  - **全体 sync (引数なし `sync` = `add -A`) はユーザーに一言確認してから**実行する (他セッションの
    書きかけや無関係な変更まで巻き込み得るため)。ユーザーが「同期」「push」と言ったときも全体でよい。
- `pull --rebase` でコンフリクトした場合や push が失敗した場合は CLI がエラーを返すので、
  内容をユーザーに伝えてストア (`~/agent-tasks-store`) での手動解決を促す。
- CLI が無い環境では手動: `cd ~/agent-tasks-store && git add -A && git commit && git pull --rebase && git push`。
