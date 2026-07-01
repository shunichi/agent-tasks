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

## 2026-07-02

### Changed
- `agent-tasks serve` の HTML を project 別グループから **状態別セクション**表示に変更。
  「今すぐ対応が要るもの」から並ぶよう **入力待ち → レビュー待ち → 実行中 → その他** の固定順で
  セクション分けする (入力待ち = in-progress で SESSION 待ち、レビュー待ち = status review、
  実行中 = in-progress で SESSION 処理中、その他 = todo/blocked/done や SESSION 未取得の in-progress)。
  空セクションは出さない。
- `agent-tasks serve` の各状態セクション内を、さらに **project 別のサブグループ**に分けて表示する
  (project 名の小見出し + 件数)。合わせて、タスク特定に重要な **ID を目立つ色 (太字・金色)** にした。
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
