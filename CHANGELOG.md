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

## 2026-07-01

### Added

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
