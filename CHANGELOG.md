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

- codex 用 skill の設置先を `$CODEX_HOME/skills` (既定 `~/.codex/skills`) から **`~/.agents/skills`** に
  変更した (codex の skill 探索先が `~/.agents/skills` に移行したため)。`make install` / `make link` が
  `~/.agents/skills/agent-tasks` へ symlink し、旧設置先 (`~/.codex/skills/agent-tasks`) に自分が張った
  symlink が残っていれば skill の二重登録を避けるため撤去する。空ディレクトリを作らない検出ガードは
  codex バイナリ有無 / `~/.agents`・`~/.codex` の存在で維持。

## 2026-07-17

- herdr プラグインの tui pane を **popup 表示 (幅/高さとも 80%)** に変更した (従来は overlay)。
  manifest (`herdr-plugin.toml`) の `[[panes]]` を `placement = "popup"` + `width/height = "80%"` にし、
  action 経由のラッパー (`tui-overlay`) も `--placement popup --width 80% --height 80%` を渡す。

- `tui` に **`e` キー**を追加。選択中タスクのファイルをエディタで開く (一覧/詳細のどちらでも)。
  エディタ解決は `edit` サブコマンドと同じ (`AGENT_TASKS_EDITOR > VISUAL > EDITOR`、既定 `code`)。
  TUI を一時中断して端末を渡すのでターミナルエディタ (vim 等) も使え、閉じると一覧を再読込する。

- `session-rename` の Claude 限定 gating (前日の変更) を**撤回**し、Claude / codex 双方で `/rename` を
  送出する元の挙動に戻した。codex にも `/rename` ("rename the current thread") があると判明したため
  (no-op 化は誤った前提だった)。SKILL.md の session-rename 記述も「両対応」に更新。

## 2026-07-16

- `session-rename` を **Claude セッションのときだけ `/rename` を送出**するようにした (codex 等では
  自動 no-op)。従来は codex で実行すると `/rename …` が入力欄に混入する誤送信があった。判定は
  `CLAUDE_CODE_SESSION_ID` env / herdr の自 pane agent 種別で行う (確証がなければ送出しない fail-safe)。
  あわせて SKILL.md に skill の呼び出し記法 (Claude `/agent-tasks` / codex `$agent-tasks`) を追記。

- agent-tasks skill を **codex でも使えるように**した。Claude と同一の `SKILL.md` を単一の情報源として
  `$CODEX_HOME/skills/agent-tasks` (既定 `~/.codex/skills/agent-tasks`) へ symlink する
  (`make install` / `make link` が codex 検出時に自動で張る)。SKILL.md に「エージェント別の注意
  (Claude / codex)」を追記し、Claude 固有の手順 (session-rename の `/rename`、statusline/session-hook、
  spawn の既定 `claude`) の codex での読み替えを明記。

## 2026-07-15

- `tui` の一覧で `n` キーを追加。AI エージェントを通さず、**タイトル (必須) と簡単な説明 (任意)**
  だけでタスクを簡易登録できる (Tab でフィールド切替 / Ctrl+S で登録 / Esc で中止)。作成される
  タスクは frontmatter に `draft: true` が付き、一覧 (`tui` / `list` / `--json`) に `[draft]` バッジで
  表示される。要件は未整理の下書きで、着手前にエージェントが詳細化する前提 (本文にその導線を残す)。
  作成はローカルのみ (自動 sync しない。ポーリングで一覧に即反映)。
- 一覧の識別バッジを英語表記に統一。human タスクのプレフィックスを **`[人手]` → `[human]`** に変更
  (frontmatter の `kind: human` / draft バッジ `[draft]` と表記を揃える)。

## 2026-07-14

- `tui` の詳細ビューで `t` キーを追加。選択タスクの `tracker:` (外部 issue tracker / 課題管理の URL、
  複数可) を既定ブラウザで開く (`o` = PR / `O` = セッション と対の導線)。

## 2026-07-10

- 開発ふりかえりドキュメント **`docs/retrospective-2026-07.md`** を追加。初回コミット (2026-06-28)
  から 2026-07-10 までの開発を、規模感・時系列 (4 フェーズ)・設計上うまくいった点・作り直しからの
  学び・作者の実感 (対話)・今後の課題として整理した。README からもリンク。
- serve の一覧ビューに「実行中(セッション外)」セクションを追加。着手済み (in-progress) だが
  セッションが動いていない (ended/unknown/未リンク、human の in-progress) タスクを「その他」から
  分離して表示する (「その他」には todo/blocked/done だけが残る)。
- **`--flag=value` 形式を全サブコマンドのフラグで受けられるようにした** — これまで `--flag value`
  (分離形) のみ対応で、`--project=webapp` のような `=` 形式は `--color` 以外「unknown option」に
  なっていた。共通パーサ (`argScan`) を拡張し、`--project=webapp` / `--status=done` など全フラグで
  インライン形が使えるようにした (分離形も従来どおり)。標準的な CLI の挙動に揃えた。
- **シェル補完のフラグ取りこぼしを解消した** — `serve` (`--addr` 等) / `cost` (`--json`/`--record`) /
  `spawn` (`--split`/`--focus`/`--force`) のフラグが bash 補完に出ていなかったのを、コマンド定義から
  補完を生成するようにして自動的に埋めた (機能追加後は `make install` で最新化)。
- (内部) コマンド定義を 1 つのレジストリ (`commands.go`) に集約し、dispatch・補完のサブコマンド
  一覧・bash 補完のフラグをそこから派生させるようにした。追加時の記載漏れ (dispatch にあるのに
  補完/ヘルプに無い等) をテストで検出する。

## 2026-07-09

- **`tui` で「今 herdr にライブなローカルセッションを持つタスク」を緑の ● で示すようにした** — link された
  session_id が今 `herdr agent list` に存在する (= 実体のあるセッションと結びついた) タスクの行に緑の ●
  マーカーを出す (自分/他 pane を問わない)。ヘッダに `●ライブ:N` の件数、詳細ペインにも `herdr セッション:
  ライブ (session_id)` を表示。マーカー列は可視行にライブがあるときだけ出す (SESSION 列と同じ「必要な
  ときだけ」方式)。あわせて tui の SESSION 列が herdr の `blocked` / `idle` 状態も表示するようにした
  (従来 working/waiting/ended のみで取りこぼしていた)。ライブ判定は `herdrStateSnapshot` (TTL キャッシュ)
  と link 突合を tick / reload で 1 回だけ計算してキャッシュし、描画は参照のみ (速度: `herdr agent list`
  の実呼び出しは高々 1 回/秒)。herdr 外は印なし (degrade)。
- **`agent-tasks focus [<project>] <id>` を追加した** — 指定タスクを実行中の herdr pane に
  フォーカスを移す。spawn で別 pane に散らばった作業セッションへ一覧から素早く飛べる
  (特に blocked = 承認/許可待ちのタスクへの取りこぼしが減る)。pane 特定は session-link
  (task → session_id) と `herdr agent list` (session_id → pane) の突合を再利用し、
  `herdr agent focus` で切り替える (workspace / tab をまたぐ移動は herdr が面倒を見る)。
  herdr 内のみ (HERDR_ENV≠1 はエラー)。未リンク / 終了済み pane (ended) は理由を案内して終了。
- **`tui` に pane フォーカス (`f` キー) を追加した** — TUI の一覧から選択タスクの herdr pane に
  直接飛べる (CLI の `focus` と同じ突合を共有)。フッター/ヘルプ (`?`) にも追記。
- **並列稼働ビューの「1日の詳細」で、その日の全タスクを表示するようにした** (`/worktime?view=parallel`) —
  これまで稼働の多い上位 12 件だけをタスク別レーンに出し、残りは「＋ 他 N タスク… を省略」と畳んでいたが、
  上限をなくして全タスクをレーン表示するようにした (タスクが多い日は縦に伸びる)。省略ノートは不要になり削除。

- **`agent-tasks tui` のヘルプ画面 (`?`) をターミナル幅に合わせるようにした** — これまで枠の内側幅を
  72 桁で固定クランプしていたため、ターミナルが広くても説明文が不要に折り返されていた。内容の必要幅
  (最長行) に追随させ、広い端末では折り返さずに収める (必要幅を超えては広げず間延びも防ぐ)。狭い端末では
  従来どおり端末幅に収める。

- **`agent-tasks tui` のヘルプ画面 (`?`) にバージョンを表示するようにした** — キーバインド一覧の末尾に
  `agent-tasks version` 相当の 1 行 (commit + CalVer + clean/dirty) を淡色で出す。TUI を開いたまま
  どの commit 時点のバイナリで動いているか確認できる。version 文字列は既存の `version.go` を再利用。

- **`agent-tasks serve` の起動時にバージョンを表示するようにした** — 待受アドレスの前に
  `agent-tasks version` 相当の 1 行 (commit + CalVer + clean/dirty) を出す。どの commit 時点のバイナリで
  serve が動いているか起動ログですぐ分かり、リビルド忘れ (古いバイナリのまま) にも気づきやすくなる。

- **serve の並列稼働ビューのツールチップを日本語グリフで表示するようにした** (`/worktime?view=parallel`)。
  ヒートマップ／スイムレーン／詳細レーンのツールチップはネイティブ SVG `<title>` で描画しており、
  ブラウザ UI 描画のためページの `font-family` も `lang="ja"` も継承せず、環境によっては日本語が
  中国語グリフで表示されていた (例: ヒートマップの「平均並列」)。ページ内 DOM のカスタムツールチップに
  置き換え、`--sans` + `lang=ja` を効かせて日本語グリフで表示する (アクセシビリティ名は `aria-label` で保持)。

- **serve の並列稼働ビューのフロントエンドを外部ファイル化した** (`/worktime?view=parallel`。内部変更、
  表示・挙動は不変) — これまで Go の raw 文字列 const に直書きしていた HTML/CSS/JS (約 400 行) を
  `webassets/parallel.{html,css,js}` の実ファイルに分離し、`//go:embed` でバイナリに取り込む形にした
  (単一バイナリ・`go build` だけ、は不変)。エディタ補完/lint/整形が効くようになり、描画ロジックの
  純粋関数 (並列度・日付・整形) を **vitest で単体テスト**できるようにした (`make test` が Go テストに
  加えて `webassets/` の JS テストも実行する。JS テストの実行には pnpm + node が要る)。後続で他ビュー
  (時間配分・ダッシュボード) も同様に移行予定。

- **`tui` で選択タスクを `S` キーで spawn できるようにした** — 一覧を眺めながら、そのまま別 pane で
  新セッションを開いて着手 (start) させられる (ターミナルに戻って `agent-tasks spawn <NNNN>` を打つ
  手間が不要)。背面起動 (フォーカスは TUI に残る) の fire-and-forget で、worktree 作成・追跡は子の
  start が行う。spawn の中核 (`spawnTask`) は CLI の `spawn` サブコマンドと共有。現在のリポジトリの
  タスクにのみ対応し (別 project は worktree の場所を特定できないため)、完了済み (done) は対象外。
  herdr 内でのみ有効。

- **serve のビュー切替を共通のセグメントタブに統一した** (`serve`) — 一覧 / 時間配分 / 時間帯・並列 の
  3 ビューを、全ビュー共通のタブ (現在地をアクセント色で明示) で切り替えられるようにした。以前は各
  ビューの見出しに個別のリンクが散らばり、一覧から時間帯・並列へ直接行けない・呼び名が「稼働時間 /
  時間配分」で不統一・現在地が分からない、という問題があった。名前と並び順・URL・スタイルを 1 箇所
  (`viewnav.go`) に集約した。

- **時間帯・並列稼働ビューを自動更新しないようにした** (`/worktime?view=parallel`) — 他ビューと同様に
  `--interval` 秒ごとにポーリングして全再描画していたのをやめ、**ロード時のスナップショット表示**に変更。
  日別ページング・日の選択・全期間トグル・タスク別ドリルダウンを備えた振り返り用の分析ビューでは、
  定期的な全再描画が操作の邪魔になるため。最新データが要るときは手動リロード。ダッシュボード `/` と
  時間配分ビュー `/worktime` の自動更新は従来どおり。

- **`tui` で選択タスクをアーカイブできるようにした** — 閲覧専用だった TUI に初めての書き込み操作を追加。
  `Space` で行を選択トグル (マルチセレクト。選択は絞り込み/再読込をまたいで保持し、行頭に `*` 印)、
  `x` で選択中の全タスク (無ければカーソル行) を確認プロンプト (件数表示) の上でアーカイブする。
  実体は既存の `archive` と同じ `<project>/archive/` への移動 (`os.Rename` でアトミック)。移動後は
  scoped sync (commit + push) を非同期で実行し結果を通知する。`sync` の中核を `syncStore` に切り出して
  CLI/TUI で共有。

- **時間帯・並列稼働ビューの文字コントラストを改善** (`/worktime?view=parallel`) — 軸ラベル・凡例ヒント・
  範囲表示・見出しなどの補助テキストが薄すぎて明るい環境で読みづらかったのを、WCAG AAA 目安 (7:1) 以上へ
  引き上げた (dim #b3bac6=9.7:1 / dim2 #9aa2b1=7.4:1。bg 比)。一番小さい軸ラベルは 9px→10.5px・やや太字に。
  fg>dim>dim2 の階層は維持。

- **時間帯・並列稼働ビューで昔のデータも遡れるように** (`/worktime?view=parallel`) — 日別スイムレーンが
  直近 14 日固定だったのを、14 日単位のページング (「← 古い14日」/「新しい14日 →」) と「全期間」トグルに
  拡張した。現在の表示範囲 (日付・全 N 日中 a–b 日目) も表示する。全データは元々クライアントに埋め込み
  済みなのでページングはサーバ往復なしで完結する。ヒートマップ (典型的な1週間) は従来どおり全期間集計。

- **`serve` に「時間帯・並列稼働」ビューを追加 (PC 限定)** — `/worktime?view=parallel`。1日のどの
  時間帯に、どれだけ並列で作業が走っていたかを俯瞰する。3 段のドリルダウン: 曜日×時刻ヒートマップ
  (俯瞰) → 日別スイムレーン (各日 0–24h に稼働区間を重ね描き。重なり=並列度) → 日をクリックすると
  その1日をタスク別レーン+並列度ストリップに展開。既存の時間配分ビュー (0104) とは別ビュー
  (時刻・並列度に集中)。既存 `/worktime` からナビリンクで相互移動できる。稼働は短バーストの集まりで
  絶対時刻の色帯はスマホで針化するため PC 向け (狭幅では案内を出す)。

## 2026-07-08

**herdr (エージェント対応マルチプレクサ) への全面移行 + 本採用。** これまで tmux + Claude Code 固有
hook に依存していた spawn / セッション状態検出 / 自 session_id・pane 特定 / session-rename を、herdr の
socket API 経由に作り直した (herdr 上で最も素直に動き、herdr 外＝素の tmux 等でも degrade して動く)。
開発中は別名ビルド `agent-tasks-herdr` + state dir 隔離で稼働中の tmux 版と共存していた (#0113) が、
本採用に伴い命名・state dir とも `agent-tasks` に復帰した (state dir のマーカー/リンク/worktime の
フォーマットは両版で互換なので、既存データの移行は不要)。

### Added
- herdr の socket/CLI クライアント層を追加 (全面移行の基盤)。herdr の unix socket 越しに agent / pane を
  操作する薄いクライアントで、以降の spawn・状態検出・session-rename はこの層を通す。(#0106)
- `agent-tasks alloc-id` に **`--title` フル生成モード**を追加 (#0122)。採番と同時に frontmatter + 本文
  まで書き込むので、skill の create が Write ツールを使わずに済む。予約済みの空ファイルを Write しようと
  すると Claude Code が `File has not been read yet` で弾き Read→Write の往復が必要になっていた問題を
  解消する。本文 (要件) は stdin (既定) か `--body-file <path>` (`-` で stdin) から受け、id 依存のメタ
  (`id`/`branch`/`worktree`/`created`/`updated`) は採番後に CLI が埋める。`--kind human` で人手タスク
  (branch/worktree を空にする)。`--title` なしは従来どおり空予約ファイルを作る (後方互換・フォールバック)。
- worktime (実稼働時間) の記録源を herdr プラグインの event hook に移行 (#0114)。同梱
  `herdr-plugin.toml` (event hook `pane.agent_status_changed` → `agent-tasks-herdr worktime-record`)
  を `herdr plugin link` で導入すると、herdr が状態遷移ごとに worktime ログを追記する。状態ソースが
  herdr に一本化され、worktime が Claude 固有 hook に依存しなくなる。
- `agent-tasks tui` を herdr の overlay pane で開けるようにした (#0117。tmux `display-popup` の置換)。
  同梱プラグイン `herdr-plugin.toml` に pane entrypoint `tui` (placement=overlay) と、それを開く
  action `open-tui` を追加。config.toml に `[[keys.command]] type="plugin_action"
  command="agent-tasks.open-tui"` を足すと、どの pane からでもキー一発で tui を前面表示できる。
- 上記 overlay の tui が **開いた時点でアクティブだった pane のプロジェクト**のタスクを初期表示する
  ようにした (#0124)。action `open-tui` を `agent-tasks-herdr tui-overlay` 経由にし、
  `HERDR_PLUGIN_CONTEXT_JSON` の `focused_pane_cwd` を読んで overlay pane をその cwd で開く
  (tui の現在 project 判定が cwd 依存なので、アクティブ pane の project になる)。cwd が取れなければ
  従来どおりプラグイン root の project にフォールバック。
- `session-link` がセッションの **claude.ai web URL を frontmatter `session:` に自動記録**する
  ようになった (#0123)。Remote Control 接続中に Claude Code が export する
  `CLAUDE_CODE_BRIDGE_SESSION_ID` (web セッション id = `session_01...`) から
  `https://claude.ai/code/<id>` を組み立てて記録する。これで serve / tui の既存リンク
  (`claudeAppURL` 等) が大半のタスクで有効になる (従来は `session:` が手動任せでほとんど空だった)。
  状態突合用のローカル session_id (UUID) は従来どおり link.json に記録され、両方が残る。既存の
  `session:` が空のときだけ埋め (手動記録を尊重)、env が無い (Remote Control 非接続) 環境では
  従来どおり空のまま。
- `agent-tasks tui` に **`O` キー**を追加 (#0125。#0123 の後続)。選択タスクのセッション URL
  (`session:` = claude.ai の Claude Code セッション) を既定ブラウザで開く。`o` (PR を開く) と対の
  導線。`session:` が URL でなければフッターにメッセージを出すだけ。#0123 で `session:` が
  自動記録されるようになったので、大半のタスクで使える。
- `done` / `done --review` が完了直後に**軽量な整合チェック**を走らせ、frontmatter に矛盾があれば
  stderr に警告するようにした (#0120)。検査は doctor と同じ (`started_at`/`completed_at` の欠落・逆転、
  `blocked_*` のクリア漏れ) を**そのタスク 1 件だけ**に適用する scoped なもの。`claim` (start) を経ずに
  done して `started_at` が無いまま `completed_at` が付くケースを、その場で気づけるようにする狙い
  (再発防止)。警告は stdout の完了行を汚さず done 自体は成功する。

### Changed
- `spawn` を herdr の `agent start` ベースに刷新し、独立サブコマンドとして新設した (#0108)。tmux の
  `split-window` + `send-keys` ハックを置き換え、別 pane での新セッション起動を herdr が正規化して行う。
  herdr 外では従来の tmux フォールバックが残る。
- **SESSION 状態**の信号源を herdr の `agent_status` に移行した (#0109)。従来の hook 由来マーカーに代えて
  herdr が状態遷移を push するので、working / **blocked** / idle を区別できる (旧マーカーはフォールバック)。
- 自 session_id / 自 pane の特定を herdr 化した (#0110)。`agent get self` (+ `HERDR_PANE_ID` / env) で
  自動検出するようになり、`session-link` の `--session` 明示が不要になった (取れなければ従来の cwd 逆引き)。
- `session-rename` を herdr の `agent rename` に移行した (#0111)。Claude セッション自体を rename するので
  web / アプリのセッション一覧にも反映される。herdr 外では従来の send-keys `/rename` がフォールバック。
- 一覧の **UPDATED 列**で、`updated:` が未記録 (空) のタスクは `created:` にフォールバック表示する
  ようにした (#0121。tui / `list` / `serve` 共通)。旧データや状態遷移を経ていないタスクで列が空欄に
  なるのを防ぐ。表示専用のフォールバックで、`--json` の `updated` は生値のまま (機械可読の意味は不変)。
- `session-hook` は worktime ログを書かなくなった (プラグインへ移行したため。二重記録の解消)。
  SESSION 状態のマーカー・フォールバック (herdr 外・link 未記録時) としては存続する。
- `serve` の稼働時間ビュー (`/worktime`) を「時刻タイムライン」から**時間配分ビュー**へ作り直した
  (#0104)。一次ビューは **日/週/月 × プロジェクトの積み上げバー** (表示中で最も稼働の多い期間を
  100%幅の基準にしてスケールを揃え、期間をまたいで量を比較できる)。プロジェクト帯をタップすると
  **その期間だけのタスク別内訳**へドリルダウン。色はプロジェクト単位にして色数を削減 (旧: タスク単位で
  色が循環・衝突)。スマホ前提の設計。
- 自動更新を「全ページ再読込 (meta refresh)」から「JSON をポーリングしてその場で再描画」へ変更
  (#0104)。粒度トグル (日/週/月) やドリルダウンの選択・スクロール位置が更新のたびに戻らなくなった
  (`/worktime?format=json` を追加)。

### Removed
- `/worktime` の旧「日 × 24時間 絶対時刻トラック」表示を廃止 (#0104)。稼働は短バーストの集まりで、
  24h 軸ではスマホ幅で帯が針化して読めなかったため。時間帯別・並列稼働の可視化は **PC 限定で別途** (#0127)。

### Fixed
- `agent-tasks session-rename` (herdr 内) で `/rename` が送信されず入力欄に改行だけ入る不安定さを修正
  (#0131)。`herdr pane send-text` + 別呼び出しの `pane send-keys Enter` の 2 リクエストに分けていたのを、
  文字列 + Enter を 1 リクエストでアトミックに送る `herdr pane run` に変更。2 プロセス起動間のギャップで
  Enter が「送信」でなく「改行」として食われるレースを解消した (負荷/タイミング依存で稀に発生していた)。
- `agent-tasks tui` のタスク ID・ヘッダーが暗くて読みづらい問題を修正 (#0126)。dim 表示に使っていた
  固定色 `Color("8")` (bright black。端末/テーマによっては潰れる) を ANSI faint (SGR 2) に変更し、
  端末の前景色に追従して読めるようにした (CLI 側の dim と同じ方式)。タスク ID は主キーなので
  `list` と同様デフォルト前景で描き、ヘッダの状態情報も dim をやめた。

## 2026-07-07

### Added

- `resume [<project>] <id> [--agent <name>] [--session <url>]` サブコマンドを追加。`blocked` / `review`
  にしたタスクを `in-progress` に戻して作業を再開する正規の手段 (これまでは frontmatter を手編集する
  しかなかった)。`done`/`block`/`claim` と同じく project ロック下で scalar キーを決定的に確定する:
  `status`=in-progress + `agent` (未指定なら既存を保持) + `updated`、`blocked_at`/`blocked_reason`/
  `completed_at` は削除、`started_at` は初回着手のまま保持。未着手 (todo) と完了済み (done) は worktree の
  新規作成/作り直しが要るため `start` へ誘導する (エラー)。skill の resume 手順は start と対称で、
  セッション同一化 (session-rename + session-link) をセットで行う。(#0128)
- `doctor` に **`session:` の URL 形式チェック**を追加。`session:` は「人が開く web セッション URL」
  (`https://claude.ai/code/session_…`) を入れる定義だが、`claim`/`session-link` の `--session` に渡す
  ローカル session_id (UUID) と混同され UUID が貼られることがある。空でなく `http(s)://` で始まらない値を
  「session URL の形式 (ローカル session_id の貼り間違い?)」として検出する (prs:/tracker: の URL 検査と同じ流儀)。(#0130)

### Fixed

- `agent-tasks help` の USAGE 一覧に `session-rename` が漏れていたのを追加 (実装・補完には既にあったが
  help だけ未記載だった)。
- SKILL (`/agent-tasks`): start 手順 5 と claim (手順 2) の案内に「**`--session` のローカル session_id (UUID) を
  `session:` に貼らない。web URL が取れなければ空でよい**」を明示 (UUID 混入の再発防止)。あわせて過去に
  UUID が入っていた既存タスク (agent-tasks/0111・0112、team-invoice/0005、workforce/0005) を、transcript の
  bridge session id / 自コミットの `Claude-Session` トレーラーから web URL を復元して置換 (store 側データ修正)。(#0130)

## 2026-07-02

### Added

- `done` / `block` サブコマンドを追加。skill の done/block 手順のうち frontmatter の scalar キー確定
  (done: `status`=done/review + `completed_at` (初回のみ) + blocked_* の削除、block: `status`=blocked +
  `blocked_at` + `blocked_reason`) を、`claim` と同じく project ロック下で決定的に行う。LLM の手編集による
  completed_at 付け忘れ・blocked_* 消し忘れ・日付推測ミスを防ぐ。worktree 撤去・PR 作成・進捗ログ追記・
  `prs:` (ブロックリスト) 追記は従来どおり skill 側が担う。(#0002)

- `agent-tasks cost [<project>] <id>` を追加。タスクごとの **Claude トークン消費 / 概算コスト**を、
  Claude Code のローカル transcript (JSONL) から集計する。既存の session-link (session_id) で
  タスク⇄セッションを解決し、`started_at`..`completed_at` の時間窓で usage を合算 (batch でセッションを
  使い回しても切り分け)、同一 message.id の重複行は 1 度だけ数える。トークン内訳 (入力/出力/キャッシュ
  読取/書込) とモデル別価格での概算コスト ($) を表示。`--json` で機械可読、`--record` で frontmatter
  `cost:` に 1 行サマリを保存 (transcript は揮発データなので消えても残る)。`show` の末尾と list/show の
  `--json` にも `cost:` を表示。サブスク (Pro/Max) 利用時は「API 換算の目安」で実請求額とは別。
  依存を増やさず `net/http` 不要・標準ライブラリのみ。
- 稼働区間の**可視化タイムライン**を追加 (worktime Phase 2)。
  - `agent-tasks serve` に **`/worktime`** ルートを追加。各タスクの稼働区間を**日 × 時刻 (0–24h) の帯**で
    俯瞰する自己完結ページ (外部依存なし。タスク色分け + 凡例 + 日別/タスク別合計)。ダッシュボード (`/`)
    のヘッダから相互リンク。
  - `agent-tasks worktime --all [--project|--all-projects] [--json]`: スコープ内の全タスクを横断集計
    (実稼働の多い順)。`--json` はタイムライン等の入力にできる配列 (task × intervals)。

- タスクの**実稼働時間**を記録・集計できるようにした (Phase 1: 記録 + CLI)。
  - session-hook が working/waiting/ended の**状態遷移を追記ログ** (`<state dir>/worktime/<session_id>.jsonl`)
    に残す (状態が変わった時だけ追記するので肥大化しない)。working に入った時刻〜抜けた時刻のペアから
    「稼働区間」を復元でき、**ユーザーの入力待ち (waiting) を除いた実際に動いていた時間**が分かる。
  - `agent-tasks worktime [<project>] <id> [--json]`: そのタスクの working 合計と稼働区間 (日×時刻) を表示。
    着手〜完了の壁時計 (リードタイム) とは別指標。`--json` は稼働区間の可視化 Web アプリの入力にできる形。
  - タスクへの帰属は session-link (task↔session_id) + タスクの `[started_at, completed_at]` 窓クリップ。
  - `session-prune` が未参照・古い worktime ログも掃除対象にするよう拡張。
  - 記録はマシンローカル (state dir)。稼働区間の可視化 Web アプリは Phase 2 (別 PR) 予定。

- `serve` を Cloudflare Tunnel + Access で公開する詳細手順を独立ページ
  **`docs/serve-cloudflare-tunnel.md`** として追加 (実運用手順を一般化: cloudflared ログイン /
  named tunnel 作成 / DNS / 専用 config / 新 UI での Access アプリ作成 / 日々の起動 / 迂回不可の検証 /
  トラブルシュート)。`docs/details.md` の該当節は概要+リンクに整理し、README のポインタも更新。

- `agent-tasks claim <id>` を追加。着手 (start) 時に `status: in-progress` を **project ロック下で
  原子的に予約**する。旧 start は in-progress の確定が worktree 作成の後で、着手指示から確定までの窓で
  並行 start のコンフリクトチェックが互いを観測できず素通りする **TOCTOU** があった。claim をチェックより
  前に置くことで窓が「ロック下の一瞬」に縮み、並行 start が互いを必ず in-progress として観測できる。
  二重着手ガード (既に in-progress ならエラー。`--force` / 同一 `--session` で通す) と、
  `--release [--to todo|blocked|review|done]` (着手取りやめ時の戻し) を持つ。`alloc-id` と同じ
  `.alloc.lock` を共有する。frontmatter の既知キーだけを行単位で書き換え、本文・進捗ログ・コメントは保全。

### Changed

- worktime を**マルチセッション対応**にした。タスクを中断→**別セッションで再開**しても、そのタスクが
  使った**全セッションの working を合算**する (従来は最後に link したセッション分しか集計されなかった)。
  - session-link (`<key>.link.json`) がセッションを**履歴 (Sessions 配列) として蓄積**するようにした
    (同一セッションの再 link は重複させない)。旧形式 (単一 session_id) も読める (後方互換)。
  - `worktime` は link された全セッションの区間を union し、`[started_at, completed_at]` 窓でクリップ→
    重なりをマージして合算。並行タスク (別セッション) の時間は混ざらない。`--json` に `session_ids` を追加。
  - `session-prune` の参照判定を全 link セッションに拡張 (生存タスクのログを誤って掃除しない)。

- skill (`/agent-tasks`): start の手順を「タスク特定 → **claim** → コンフリクトチェック → worktree →
  残り frontmatter → session-link」に組み替えた (上記 claim による TOCTOU 解消)。二重着手チェックと
  in-progress 確定は claim が一括で担い、コンフリクトで取りやめる場合は `claim --release` で戻す。

- skill (`/agent-tasks`): start 着手時の `session-rename` を **手順 0 (最初)** に引き上げ、
  「`start <NNNN>` を受けたら、タスク内容の取得・チェックより前に**まず** `agent-tasks session-rename <NNNN>`
  を叩く」よう明確化した。send-keys は入力欄が空なうち (指示直後) が最も競合しにくいため。batch も
  各タスクの処理に入ったらまず rename する形に揃えた。
- `agent-tasks serve` の HTML を project 別グループから **状態別セクション**表示に変更。
  「今すぐ対応が要るもの」から並ぶよう **入力待ち → レビュー待ち → 実行中 → その他** の固定順で
  セクション分けする (入力待ち = in-progress で SESSION 待ち、レビュー待ち = status review、
  実行中 = in-progress で SESSION 処理中、その他 = todo/blocked/done や SESSION 未取得の in-progress)。
  空セクションは出さない。
- `agent-tasks serve` の各状態セクション内を、さらに **project 別のサブグループ**に分けて表示する
  (project 名の小見出し + 件数)。合わせて、タスク特定に重要な **ID を目立つ色 (太字・金色)** にした。
- `agent-tasks serve` の **project 名サブ見出しを目立つ色 (明るいシアン・太字)** にした
  (従来は薄グレーで埋もれていた。▸ マーカーも同色に)。project の切れ目が一目で分かるようにする。
- `agent-tasks serve` のカードに **「アプリで開く」リンク** (`claude://code/<session-id>` の
  ディープリンク) を追加。`session:` が claude.ai の Claude Code セッション URL のとき、
  タップで Claude アプリ (スマホ/デスクトップ) を直接開ける。従来の https リンクは「web」として
  併記し、アプリ未インストール時のフォールバックにする (universal link はブラウザに落ちることがあるため)。

### Added

- `serve` を **Cloudflare Tunnel + Cloudflare Access** で外出先から閲覧する手順を
  `docs/details.md` に追加。serve は無認証・`127.0.0.1` バインドのまま、認証は Cloudflare Access
  (Zero Trust) に任せる構成 (named tunnel + 独自ドメイン前提)。ポート開放せず公開経路を Tunnel に
  一本化することで直アクセスを塞ぐ。コード変更はなし (README の serve 節にもポインタを追記)。

- `agent-tasks serve`: 同一 LAN のスマホから全 project のタスク一覧を閲覧できる簡易 HTTP サーバを追加。
  `-w --all-projects` 相当の情報を HTML で返す。既定は `127.0.0.1:8080` (localhost のみ)、`--addr :8080`
  のように host を省くと `0.0.0.0` = LAN 公開 (認証なし・LAN 内前提)。起動時にスマホから開く URL を案内する。
  `--interval` 秒ごとに meta refresh で自動更新 (既定 5、0 で無効)。スマホ向けレスポンシブなカード表示で、
  SESSION 状態 (working/waiting/ended)・blocked 経過・各タスクの Claude セッション URL / PR リンクを出す。
  依存は増やさず `net/http` + `html/template` のみ。

- `agent-tasks session-prune` を追加。state dir (`~/.local/state/agent-tasks/sessions/`) に溜まる
  古いマーカー/link を掃除する。対応タスクが無い/done の worktree マーカー・link と、生存 link から
  参照されず一定日数 (既定 7、`--older-than` で調整) 更新の無い sess マーカーを削除。`--dry-run` で
  対象のみ表示。稼働・保留中 (in-progress/blocked/review/todo) のマーカーや新しい sess マーカーは
  残す。揮発情報 (次の hook で再生成) なのでストアには一切触れない。

- **コードを変更しない「人手タスク」(`kind: human`)** を登録できるようにした。frontmatter に
  `kind: human` を持つタスク (デプロイ設定の手動変更・顧客確認・データ移行・レビュー依頼など) は、
  着手しても **worktree / branch を作らず、コンフリクトチェックの対象外**になる (コード領域を
  持たないため。着手側・被チェック側の両方)。既定 (省略) は従来型の code タスク。
  - `list` で **`--kind human|code`** で絞り込み可能。一覧・TUI では human タスクに **`[人手]`**
    プレフィックスを付けて識別し、SESSION 列は `-` (セッションを持たない) と表示する。
  - `--json` / `show --json` の出力に `kind` を追加 (human のときのみ。code は省略)。human の
    in-progress では `session_state` を出さない。
  - `doctor` が `kind:` の未知の値 (human/code/空 以外の typo) を検出する。
  - SKILL.md の create / start / recommend / done を種別対応に更新 (human は簡易フロー)。

### Fixed

- tui: 詳細表示中に最上部のヘッダ (status 行) が消える不具合を修正。横分割の区切り線が末尾改行で
  1 行余計に描画され、View 全体が端末 height+1 行になって最上部がスクロールで押し出されていた。
- tui: コピー結果などの一時メッセージ (flash) をヘッダ末尾から footer 先頭へ移動。狭い端末
  (tmux popup / 縦分割の詳細表示) でも status 情報を潰さずコピー確認が見えるようにした。
- tui: `c` のクリップボードコピーを `Setsid` で親端末から切り離し、tmux popup 内で起動しても
  popup を閉じた際に常駐 (wl-copy / xclip) が kill されずクリップボード内容が残るようにした。

## 2026-07-01

### Added

- `auto-archive` サブコマンド: 完了後に一定日数 (既定 30、`--older-than <days>` で指定) を過ぎた
  done タスクを一括で `<project>/archive/` へ退避する。`completed_at` 無しや review/in-progress は
  対象外。スコープは list と同じ (`--project`/`--projects`/`--all-projects`、既定は現在 project)。
  `--dry-run` で対象一覧のみ表示。退避結果は `archive` と同じ `from:`/`to:` 形式で出力し、
  まとめて `sync --path` に渡して同期できる。

- `agent-tasks session-rename [<project>] <id>`: 現在の Claude セッション名を **`task <NNNN>: <title>`**
  に変えるサブコマンドを追加。tmux 内なら**自分の pane (`$TMUX_PANE`) へ `send-keys` で
  `/rename …` を打ち込む** (Claude はスラッシュコマンドをツールから直接呼べないための回避)。本物の
  `/rename` 経路なので web / スマホアプリのセッション名 (Bridge 同期先) にも反映される。tmux 外では
  `/rename …` 行を出力してユーザーに実行を促すフォールバック。skill は着手指示の直後 (worktree より前、
  入力欄が空なうち) にこれを 1 回叩く。batch (1 セッション使い回し) では各タスク着手ごとに呼んで
  セッション名を追従させる。

- `agent-tasks open [<project>] <id>`: タスクに紐づく **worktree (作業ツリー) をエディタで開く**
  サブコマンドを追加。frontmatter の `worktree:` (コードリポジトリ root からの相対パス) を現在の
  リポジトリ root を基点に絶対パスへ解決して開く (絶対パスならそのまま)。エディタの決定は `edit` と
  同じ (`AGENT_TASKS_EDITOR` > `VISUAL` > `EDITOR`、既定 `code`)。worktree 未記録や撤去済み
  (done で削除) のときは分かりやすくエラーにする。ストアのタスクファイルを開く `edit` とは別コマンド。

- タスクの**文字列検索**を追加。CLI は `--search <q>` (別名 `--grep`) で**タイトル部分一致**
  (大文字小文字を区別しない)、`--content` (別名 `--full`) を併用すると**本文も対象**にする。
  既存の status/project/done フィルタや `--json` / `--recent` と併用可。フッターに検索条件を注記。
  TUI は `/` で**インクリメンタル検索**に入り、入力に応じて一覧を絞り込む (`Tab` で本文検索トグル、
  `Enter` 確定、`Esc` 解除。自動更新・選択保持と両立)。タイトル検索は既存フィールドで完結し、
  本文検索は該当時のみファイル本文を読む (無駄な I/O を避ける)。

- 指定した**複数 project だけを横断表示**できるようにした (`--all-projects` の部分集合版)。
  `--project` を**繰り返し**指定 (`--project a --project b`) するか、`--projects a,b,c` (カンマ区切り) で
  その集合だけを横断する。単一 `--project` と `--all-projects` の既存挙動は不変 (後方互換)。
  list / `--json` / `--recent` / `report` / `tui` すべてに適用され、フッター注記が対象 project 群を示す
  (実効スコープを単一 string から集合に一般化)。bash/zsh 補完も `--project` 繰り返し / `--projects` に対応。

- `agent-tasks tui`: `o` キーで選択タスクの **PR (`prs:`) を既定ブラウザで開く**ようにした
  (複数 PR があれば全部開く)。OS の URL オープナー (`xdg-open` / `open` / `wslview`) を検出し、
  alt-screen を邪魔しないよう非同期でバックグラウンド起動する。PR が無いタスクや対応オープナーが
  無い環境ではフッターに短く表示 (無害)。フッター/ヘルプに `o PR` を追記。

- `agent-tasks report`: 一定期間に完了したタスク (done かつ `completed_at` が期間内) を **markdown** で
  出力するサブコマンドを追加。`--month [YYYY-MM]` (既定は今月) / `--week [YYYY-MM-DD]` /
  `--since <d> --until <d>` で期間を指定。各タスクに ID・タイトル・開始・完了・所要時間 (リードタイム) を
  表で出し、合計件数・所要合計/平均のサマリを添える。スコープは list と同じ (既定は現在 project、
  `--all-projects` で横断し project ごとにセクション分け)。

- タスクに関連する外部 issue tracker / 課題管理の URL を記録する `tracker:` フィールドを追加
  (YAML リスト、複数可)。`prs:` (PR 専用) とは別枠の汎用フィールドで、任意ホストの URL を入れられる。
  `show` の末尾と `--json` に一覧を表示し、`doctor` が URL 形式を軽く検査する。

### Changed

- `tui` のキー操作を tig 風にした。**`Ctrl+n` = 次のタスク / `Ctrl+p` = 前のタスク** (詳細表示中も選択移動
  できる)。**詳細表示中は `j`/`k` を詳細の 1 行スクロール**に切り替える (一覧のみのときは従来どおり選択移動)。
  `Ctrl+d`/`Ctrl+u` の半画面スクロールは従来どおり。フッター/ヘルプの説明も更新。

- skill (`/agent-tasks`): 着手時に Claude セッション名を **`タスク <NNNN>: <title>`** にする手順を追加。
  web / スマホアプリのセッション一覧 (Bridge 同期先) で「今どのタスクか」が分かる。
  spawn で開く子は起動時に `claude -n` で**自動命名**。直接 start / batch (1 セッション使い回し) の
  現在セッションは、**tmux 内なら自分の pane へ `tmux send-keys` で `/rename タスク <NNNN>: <title>` を
  打ち込んで自動リネーム**する (本物の `/rename` 経路を通すのでスマホ表示=クラウド側にも同期される)。
  tmux 外ではユーザーへ `/rename …` の 1 行を提示するフォールバック。
  (`/rename` を起こすツール/フックは無いため send-keys がほぼ唯一の自動手段。Issue #54325 / #29355 参照。)

### Added

- タスクを GitHub issue として共有する機能を追加 (store → issue の一方向)。
  - `agent-tasks issue [<project>] <id> [--repo owner/repo]` でタスクを issue として起票し、URL を
    frontmatter `issue:` に記録する (CLI が記録するので手編集不要)。連携済みなら本文を更新する。
  - 本文は frontmatter (内部メタ) を除いた Markdown 本文のみ。作成先 repo は `--repo` 明示が最優先、
    省略時は cwd のコード repo を `gh` で推論 (project と食い違うときは取り違え防止で停止)。
  - `show` の末尾と `--json` に issue を表示。`doctor` が `issue:` の URL 形式を検査。
  - skill (`/agent-tasks`) に `issue` 操作、bash/zsh 補完も対応。`gh` (GitHub CLI) が必要。

- `agent-tasks tui`: 一覧で選択中タスクの **`start task <NNNN>` をクリップボードへコピー**する `c` キーを
  追加 (任意の pane の claude に貼って着手できる)。コピーは OS のクリップボードコマンド
  (`wl-copy` / `xclip` / `xsel` / `pbcopy` / `clip.exe`) でシステムクリップボードへ直接書き、無い環境
  (SSH 先など) では OSC52 にフォールバックする。非同期実行で UI をブロックせず、実際の成否を
  ヘッダに表示する。対象は `todo` / `blocked` のタスクのみ。フッター/ヘルプにキーを追記。

- タスクのアーカイブ機能を追加 (不要になったタスクを削除せず退避する)。
  - `agent-tasks archive [<project>] <id>` でタスクを `<project>/archive/` へ退避。通常の
    `list` / `-a` / `doctor` から外れる。`agent-tasks unarchive <id>` で元に戻す。
  - `agent-tasks list --archived` / `agent-tasks show <id> --archived` で退避済みを閲覧
    (`--json` にも `archived: true` が出る)。
  - 採番 (`alloc-id`) はアーカイブ済みの番号も算入するので、退避した番号を再利用しない。
  - `doctor` の id 重複検査はアクティブとアーカイブを横断して点検する。
  - skill (`/agent-tasks`) に `archive` / `unarchive` 操作を追加。

- worktree 撤去フック **`.worktree-post-remove`** と、それを呼ぶ **`agent-tasks worktree-remove <dir>`**
  を追加 (worktree-init / post-create の対)。`done` の worktree 撤去を `git worktree remove` から
  `worktree-remove` に置き換えると、撤去の直前に worktree 内で `.worktree-post-remove` が走り、
  post-create が worktree ごとに作った外部副作用 (worktree 固有 DB / puma-dev 登録など) を後始末する。
  cwd が対象 worktree 内なら撤去を中止、未コミット変更やフック失敗でも中止 (捨てるなら `--force`、
  フックだけなら `--hook-only`)。`scaffold-worktree` が 3 つ目のファイル `.worktree-post-remove` も
  展開するようになり、rails テンプレは post-create と同じ命名で `dropdb` / `pdl unlink` する雛形を含む。

### Fixed

- 作成途中の中途半端なタスク (alloc-id が作る **0 バイトの空予約ファイル**) を一覧に出さないようにした。
  従来は `(no title)` / `todo` として list・`--json`・`--recent`・`tui` に並んでいた。空予約は表示から
  除外し、`doctor` では「作成途中/空の予約ファイル」として検出する (放置された予約に気付ける)。

- `tui` の一覧でセッション状態が**タイトル列を侵食**していた問題を修正。タイトル前の色付きバッジ
  だったのを、STATUS の右の**独立した SESSION 列**にした (CLI list と同じく in-progress 行が
  あるときだけ表示)。これでタイトルの開始桁が全行でそろう。

- `tui` の詳細表示で行が横方向に切り詰められて読めない問題を修正。詳細本文を viewport 幅で
  **折り返す**ようにし (長い行が切れない)、さらに分割の向きの判定を改善: 一覧をタイトルが
  収まる自然幅にしたとき詳細ペインに読み幅 (約 50 桁) が残らないなら、一覧を切り詰めてまで
  横に並べず**縦分割 (詳細を下)** にしてウィンドウ全幅を詳細に与える。ウィンドウ幅だけの固定
  閾値ではなく、一覧の自然幅も加味して向きを決める。

- `tui` の一覧で、横幅が広いときにタイトルと `UPDATED` の間が大きく空いて見づらい問題を修正。
  `UPDATED` を行の右端に右寄せするのをやめ、**タイトル列の直後の桁にそろえて**置くようにした
  (右側の余白は空ける)。

### Added

- `agent-tasks tui`: `?` キーで**ヘルプ (キーバインド一覧)** を開閉できるようにした。全キーの説明を
  枠付きパネルで表示し、`?`/`q`/`Esc` で閉じる (表示中は他キーを無効化)。フッターに `? ヘルプ` を追加。
  枠幅は端末幅に収め、狭い端末でも破綻しない。

- `agent-tasks sync` に **scoped 同期**を追加。`sync <id>` / `sync <project> <id>` / `sync --path <p>`
  で**そのタスクファイルだけ**を stage・commit する (引数なしは従来どおり全体 = `add -A`)。
  並列セッションが他タスクを書きかけでも巻き込まず、確認なしの自動同期に使える。

- `agent-tasks tui`: 一覧+詳細をインタラクティブに閲覧する常駐ビューワーを追加 (Bubble Tea)。
  起動直後は**一覧のみ**を表示し、`Enter` で選択タスクの詳細 (frontmatter + 本文 + PR/所要時間サマリ) を
  開く。詳細表示中の `q`/`Esc` は詳細を閉じ、一覧のみのときの `q`/`Esc` で終了する。
  端末が広いときは詳細を**右**に (リスト幅はタイトルに合わせて伸縮)、狭い/縦長のときは詳細を**下**に
  積む (tig 風) よう自動で分割の向きを切り替える。一覧にはタイトル列の直後に `UPDATED` 列を、
  in-progress 行には色付きのセッション状態 (working/waiting/ended) を表示する。
  別セッションが裏でストアを更新しても、一定間隔のポーリング (mtime 差分検知。`--interval <秒>`、既定 2)
  で自動再描画し、選択は project/id で保持する。`↑↓`/`jk` で選択、`PgUp`/`PgDn` で詳細スクロール、
  `a` で done 表示トグル、`s` で status フィルタ循環、`p` で現在 project ↔ 横断トグル、`r` で手動更新。
  スコープ指定は list と同じ (`--project` / `--all-projects` / `--status` / `--all`)。
  端末専用 (非 TTY では案内して終了)。

### Changed

- `make install` が補完スクリプト (`install-completions`) も再生成するようになった。CLI バイナリは
  symlink で常に最新だが補完は静的ファイルのため、従来は機能追加後に補完だけ古いまま残っていた。
  今後は `make install` 一度でバイナリ + skill + 補完がすべて最新になる
  (zsh は `~/.zcompdump` キャッシュのため反映は新しいシェルから)。

- `agent-tasks sync` を並列セッションで安全にした。複数セッションの同時 sync を**ストアロックで
  直列化** (git の index.lock 衝突を回避)、push 競合 (non-fast-forward) は `pull --rebase --autostash`
  で取り込み直して**自動リトライ**、dirty な作業ツリーでも `--autostash` で rebase する。これにより
  scoped 同期 (上記 Added) と合わせて auto-sync を安全に有効化できる。

- `tui` の一覧で in-progress 行の**セッション状態を色付きで表示**するようにした (list の `SESSION` 列と
  同じ working/waiting/ended。waiting を目立たせる)。従来の無色プレフィックスから格上げし、詳細ペインの
  先頭にも現在のセッション状態を出す。

## 2026-06-30

### Added

- 全サブコマンドが `--` (オプション終端) を解釈するようになった。`--` 以降は位置引数として扱い、
  フラグとして解釈しない (例: `agent-tasks show -- 0031`)。`-` 始まりの project/id を渡す逃げ道。
  あわせて各コマンドに散らばっていたフラグ解析 (値フラグの欠落チェック / 位置引数の収集) を
  共通スキャナ (`argScan`) に集約した (挙動は不変)。

### Fixed

- `worktree-init`: `.worktreeinclude` の対象が symlink のとき、リンク先 (repo 外も含む) の中身を
  コピーしてしまう問題を修正。`os.Lstat` で symlink を判定し追従せず除外する (ディレクトリ配下の
  symlink も同様)。dst の既存チェックも `Lstat` にして、壊れた symlink を「無い」と誤判定して
  追従コピーしないようにした。

### Changed

- `list` / `--watch` の `TITLE` 列を端末幅で truncate するようにした。端末幅 (ioctl(TIOCGWINSZ)、
  取れなければ `COLUMNS`) から他列の幅を引いた残りに収まるよう長い title を末尾 `…` で丸める。
  watch (折返し OFF) で長い title が桁ずれ・前フレームの残骸を出していたのを防ぐ。端末幅が
  取れない (パイプ等) ときは従来どおり truncate しない。
- 表示幅計算 (テーブルの列幅・truncate) を自前の East Asian Width テーブルから
  `github.com/mattn/go-runewidth` に差し替えた。絵文字・結合文字・ゼロ幅文字の幅を正しく数え、
  `list` のテーブルが崩れない。あわせて方針を「依存ゼロ」→「**依存は最小限**」に変更
  (確立した小さなライブラリは採用可)。

### Fixed

- `list` のタスク ID ソートを数値順に変更 (文字列比較だと ID が4桁を超えたとき
  `"10000" < "9999"` で逆転する境界を予防)。パースできない ID は従来どおり文字列比較にフォールバック。

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
