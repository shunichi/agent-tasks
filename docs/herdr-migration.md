# herdr 前提での再設計メモ (tmux/Claude 依存の妥協点の作り直し)

タスク `agent-tasks/0105`。**設計メモ (調査・比較・移行方針の提案)** であり、コード実装は含まない
(実装は後続タスクに切り出す)。tmux + Claude Code の仕様上の制約で妥協していた一連の機能を、
**herdr** (エージェント対応の tmux ライクなマルチプレクサ。単一バイナリ、socket/CLI API 付き) を
前提にすると素直に作り直せる — その対応付けと移行方針を、**実機検証の結果**を根拠にまとめる。

## 検証環境 (実機)

- herdr client/server **v0.7.1** (stable, protocol 14)。単一バイナリ (`~/.local/bin/herdr`)、
  server はローカル socket (`~/.config/herdr/herdr.sock`) で常駐。
- Claude 統合 **v7** (`herdr integration install claude` 済み)。hook = `~/.claude/hooks/herdr-agent-state.sh`。
- 状態検出 manifest = リモート由来 `claude.toml` **v2026.06.10.3** (herdr 側で集中管理・自動更新)。
- **本セッションは herdr の pane `w3:p1` 内で動作**。env に `HERDR_ENV=1` / `HERDR_PANE_ID=w3:p1` /
  `HERDR_SOCKET_PATH` / `HERDR_WORKSPACE_ID=w3` / `HERDR_TAB_ID=w3:t1` が注入されている。
- 補足: **`TMUX` も同時にセットされていた** (herdr が tmux の上/下で共存している可能性)。移行時に
  「tmux 由来の env・挙動が残る」前提で設計する必要がある (herdr 単体で完結すると決め打ちしない)。

## 要検証の結果 (実機で確認)

| # | 検証項目 | 結果 | 根拠 (実機) |
|---|---|---|---|
| 1 | 承認/許可待ちが `blocked` として確実に検出されるか | **概ね確認**。screen manifest に許可プロンプト用ルールが実在 (`bash_permission_prompt`="do you want to proceed?"、`generic_permission_prompt`、`live_blocked_form`="enter to select"/"esc to cancel")。working スピナー稼働中は working が優先され誤検出しない。**取りこぼしリスクはプロンプト文言のマッチ精度と manifest 更新に依存** | `herdr agent explain w3:p1 --json` の `evaluated_rules` |
| 2 | env 名・socket/CLI API の正確な仕様 | **確認 (差分あり)**。env は予測どおり `HERDR_PANE_ID` 等。CLI は Web 要約より豊富 (下記「CLI サーフェス」)。source の値は `visible\|recent\|recent-unwrapped` で、予測にあった `detection` は無い | `env`、`herdr {agent,pane,wait} --help` |
| 3 | 既存 claude pane のプロンプトへ文字列を流し込めるか | **確認**。`herdr pane send-text` = Enter なしのリテラル注入、`herdr pane run` = command+Enter 実行。スクラッチ pane でラウンドトリップ実証 (split→run→read→send-text→close) | スクラッチ pane `w3:p3` での実測 |
| 4 | クリップボードブリッジが CLI からのシステムクリップボード書込を代替できるか | **代替不可の見込み**。`herdr` に `clipboard` サブコマンドは無く、top-level help にも無し。クリップボードは keybinding/プラグイン層の機能で、**CLI から叩ける書込コマンドが無い** → タスク 0083 の外部ツール方式 (xclip 等) は概ね残る | `herdr --help`、`herdr clipboard --help` (無し) |
| 5 | 統合が捕捉する native session id が claude.ai URL と対応づくか | **部分的に解決 + 穴は残る**。herdr が捕捉するのは **ローカル session UUID** (`0e177131-…`。スクラッチパッド由来と一致) で、`agent get`/`agent list` の `agent_session.value` で取得できる。しかし claude.ai URL の id (`session_<base62>` 形式) とは別物なので、**URL 突合の本質差は残る** | `herdr agent get w3:p1` の `agent_session` |
| 6 | 導入コスト・安定性 (単一バイナリ/リモート SSH/スマホ attach) | **一部確認**。単一バイナリで server 常駐、`herdr status` で client/server/update 状態を確認可。**リモート SSH (`herdr --remote`) / スマホ attach は本検証では未実施** (別途要確認) | `herdr status`、`herdr --help` |

### 追加で判明した重要事項

- **hook は「状態」を送らず「session 情報」を送る**。`herdr-agent-state.sh` は `session` フックで
  `pane.report_agent_session` を呼び、`agent_session_id` (= `session_id`) と
  **`agent_session_path` (= transcript の JSONL パス)** を herdr に報告する。状態自体は screen manifest 検出由来
  (タスクの前提どおり)。
- → herdr の hook は transcript_path を herdr に**報告**するが、**引き出す read API は無い** (0112 で確定)。
  socket 全 76 メソッドを確認: `pane.report_agent_session` は書き込み専用で、`agent.get`/`pane.get` の
  agent_session は session_id (value) のみ・path を返さない。→ **コスト計測 (0101) は現状維持** (cost.go は
  session_id だけで `<claudeProjectsDir>/*/<sid>.jsonl` を glob しており堅牢。herdr で置換する利点なし)。
- **working/idle 検出は OSC タイトル駆動が最優先** (`osc_title_working` 優先度 1100 = 点字スピナー
  `⠂`、`osc_title_idle` = `✳`)。tmux `capture-pane` が alt-screen で空になる問題 (0067) と異なり、
  herdr は OSC タイトル + 画面領域を alt-screen でも読める。
- agent-tasks 自身の `session-hook` と herdr の hook は**現在も並存**して動いている (settings.json に両方)。
  ただし **worktime (実稼働ログ) の記録源は herdr プラグインの event hook に移行済み (0114)**。
  session-hook はもう worktime を書かず、残る役割は SESSION 状態のマーカー・フォールバック (0109) のみ。

## CLI サーフェス (実機で確認したもの。移行の道具箱)

- **入力送出**: `herdr pane send-text <pane> <text>` (リテラル、Enter なし) /
  `herdr pane send-keys <pane> <key…>` / `herdr pane run <pane> <cmd>` (command+Enter) /
  `herdr agent send <target> <text>`。
- **pane 起動**: `herdr agent start <name> [--cwd] [--split right|down] [--env K=V] [--focus|--no-focus] -- <argv…>`
  (新 agent を直接起動) / `herdr pane split [<pane>] --direction right|down [--cwd] [--env] [--no-focus]`。
- **列挙/検査**: `herdr agent list` / `herdr agent get <target>` / `herdr pane list [--workspace]` /
  `herdr pane get <pane>` (`agent_status` = idle|working|blocked|unknown)。
- **出力読取**: `herdr {agent,pane} read <target> --source visible|recent|recent-unwrapped [--lines N] [--format text|ansi]`
  (**alt-screen でも読める**)。
- **待機 (push)**: `herdr wait agent-status <pane> --status idle|working|blocked|done|unknown [--timeout MS]` /
  `herdr wait output <pane> --match <text> [--regex] [--timeout MS]` / `herdr agent wait …`。
- **ラベル**: `herdr agent rename <target> <name>` / `herdr pane rename <pane> <label>`。
- **状態報告 (統合以外からの任意報告)**: `herdr pane report-agent <pane> --state idle|working|blocked` 他。
- **検出説明**: `herdr agent explain <target> [--json]` (どのルールがマッチして状態が決まったか)。
- 識別子形式: `w<workspace>:p<pane>` / `w<workspace>:t<tab>`。自 pane は env `HERDR_PANE_ID`。

## 妥協点 → herdr 対応付け (実機検証で改訂)

| 妥協 (既存タスク) | 現状の回避策 | herdr での置き換え | 判定 (改訂) |
|---|---|---|---|
| 自 session_id を知る env が無い (0027) | スクラッチパッドのパス末尾から抜く裏技 | `agent get` の `agent_session.value` (= ローカル UUID) で確実取得。env `HERDR_PANE_ID` で自 pane 特定 | **解消** (実証) |
| hook JSON に pane_id が無い (0079/0067 停滞) | 送信先を特定できず未実装 | `HERDR_PANE_ID` + `pane.list`/`agent.list` で特定、`pane run`/`send-text` で送出 | **解消** (実証) |
| 「入力待ち」の直接シグナルが無い (0020/0080) | Notification/Stop フックでマーカー間接推定 | `agent_status=blocked` が一級。`wait agent-status`/`events.subscribe` で push 検知 | **概ね解消** (manifest のプロンプト文言マッチ精度に依存) |
| alt-screen で capture-pane が空 (0067) | pane 内容を判定できない | `pane read --source visible/recent` で正規に読める | **解消** (実証) |
| send-keys の割り込み/tmux 外縮退 (0089/0092/0010) | 入力欄が空なうち 1 回だけ等の制約 | 送信前に `agent_status` を確認し idle/blocked にだけ送出。安定 pane id 指定 | **改善** (実証: 状態確認 → 送出が可能) |
| セッション名変更 API 無し (0085/0089) | send-keys で `/rename` 打鍵ハック | pane/agent ラベルは `herdr agent rename` で正規化 (send-keys 不要・tmux 非依存) | **部分** (herdr 内ラベルは正規化。**claude.ai web/アプリ側の名前は変わらない**) |
| hook session_id ≠ claude.ai URL (0020/0027) | worktree basename で突合 | 統合が native session id (ローカル UUID) を pane に紐付け → 突合面が clean | **部分** (ローカル UUID↔claude.ai URL の差は残る) |
| OSC52 が tmux 越しに届かない (0083) | 外部ツール優先 (xclip 等) | クリップボードブリッジは keybinding/プラグイン層で **CLI 書込コマンド無し** | **不変〜部分** (CLI 代替不可の見込み。外部ツール方式を継続) |
| 端末タイトルが TUI に上書き (0037) | status line 方式に | herdr が自前ラベル/状態 UI (サイドバー) を持つ | **軽微改善** |
| per-task コスト計測 API 無い (0101) | transcript JSONL 自前集計・揮発・目安表記 | herdr は transcript_path を報告受けするが read API 無し (0112 で確定)。cost.go は session_id で glob 済 | **不変** (herdr で置換する利点なし。現状維持) |

## 設計方針: 全面移行 (案 A) を採用 (2026-07-02 合意)

再プラットフォーム化の是非が最大の論点だったが、**herdr 前提に一本化する「全面移行」を採用**する
(ユーザーと合意、2026-07-02)。agent-tasks の「tmux + hook マーカー」層
(spawn の split-window/send-keys、session-hook のマーカー、`tmux capture-pane` 回避策) を
herdr socket/CLI API に載せ替える。

- **利点**: 妥協点の多く (0027/0079/0067/0020/0080/0089/0092) が素直に解消。分岐が消えコードが簡潔に。
  状態検出・pane 送出・待機が一級 API になる。
- **引き受けるトレードオフ**: herdr 非利用環境では動かなくなる。herdr (バイナリ・server・manifest 更新)
  への依存が前提化する。スマホ/リモートの実用性は未検証 (要検証#6) だが、方針は全面移行で確定。
- **移行の進め方**: いきなり全 tmux コードを消さず、機能単位で herdr 経路に置換 → 旧 tmux 経路を撤去、
  を繰り返す (後続タスク単位)。開発は長命な `herdr` ブランチ上で行い、main には都度マージしない。

> 検討したが不採用: **案 B (tmux/herdr 両対応のアダプタ層)**。段階移行・herdr 無し環境の互換という
> 利点はあるが、抽象層のメンテと両系統テストのコスト、各所の分岐増加を嫌って見送り。全面移行で
> コードを簡潔に保つことを優先した。

### 役割分担 (どちらの案でも共通の指針)
- **セッション/pane/状態の取得は herdr に委譲**、タスク管理 (frontmatter/PR/コスト/稼働時間/blocked 理由/
  複数プロジェクト) は agent-tasks が持つ。
- herdr のサイドバー (blocked/working/idle/done 俯瞰) は `list` の SESSION 列と機能が重なる。
  重複を herdr に寄せるか、agent-tasks 側で独自の付加価値 (タスク紐付け) を保つか要判断。
- **agent 中立性は保てる/むしろ向上**。herdr は複数 agent 統合 (codex/opencode 等) を持つので、
  「保管・突合は agent 中立、信号源は herdr 統合」に整理しやすい。

## 移行の制約 (稼働中の main 版との共存) — 2026-07-02 追加

herdr 対応版を開発する間、**main ブランチ版が裏で常時稼働している**。開発版が稼働版を壊さないための
制約 (ユーザー指定):

### 1. バイナリ/skill/補完は別名にする (稼働版を上書きしない)

現状 (実測):
- `~/.local/bin/agent-tasks` → **main worktree の `bin/agent-tasks` への symlink**。
- `~/.claude/skills/agent-tasks` → **main worktree の `skills/agent-tasks` への symlink**。
- 補完は `agent-tasks` / `_agent_tasks` で固定名。

⚠️ herdr worktree で `make install` すると、これらの symlink が **herdr worktree 側を指すよう張り替わり、
稼働中の main 版を破壊する**。→ herdr 版は **別名** (例 `agent-tasks-herdr`) でビルド/インストールし、
main 版の symlink・skill・補完に一切触れないようにする。Makefile の `BIN` / `link` / `install-completions` を
名前でパラメータ化する (or herdr 専用ターゲットを足す)。skill も別名にして別 CLI を叩くようにすれば
両版が同一マシンで併走できる。

### 2. agent-tasks-store の互換性を当面保つ

- ストアは `AGENT_TASKS_STORE` (既定 `~/agent-tasks-store`) を **両版で共有**する。
- 移行中は frontmatter/ファイル形式を**破壊的に変えない** (main 版が読めなくなるため)。新フィールドは
  任意 (省略可) で足す程度に留め、既存キーの意味・必須性を変えない。データ側の非互換変更は移行完了後に検討。

### 3. セッションリンクの state dir を壊さない

- マーカー/link/worktime は `AGENT_TASKS_STATE_DIR` > `$XDG_STATE_HOME/agent-tasks/sessions` >
  `~/.local/state/agent-tasks/sessions` に書かれる (現状 env 未設定 = 既定パス)。**main 版がここに
  書き込み中** (`*.link.json` / `*.json` / worktime ログ)。
- herdr 版は状態検出を herdr へ移す (0109) ため最終的にはこのマーカー機構を使わなくなるが、**移行中に
  この共有ディレクトリを削除・再フォーマット・掃除してはならない** (main 版が壊れる)。→ herdr 版は
  **別の state dir** (`AGENT_TASKS_STATE_DIR` を herdr 用に向ける or 既定名を変える) を使い、共有ディレクトリを
  read-only 扱い/不可侵にする。0109 の「旧マーカー撤去」も、稼働 main 版が使う間は撤去せず**隔離**に留める。

> これら 3 点は後続タスク 0113 (共存セットアップ) で最初に固め、0106 以降が別名・別 state dir 前提で
> 動くようにする。0106 (store 互換方針) / 0109・0110 (state dir 隔離) にも反映済み。

## 作り直しタスクの優先順位 (全面移行の後続タスク案)

全面移行 (案 A) 前提。**まず共存セットアップ (0113) と herdr クライアント層 (0106) を用意**し、その上で
機能単位に置換していく。各タスクは「herdr 経路に置換 → 旧 tmux 経路を撤去」をセットで行う。

- **[前提・最優先] 稼働 main 版との共存セットアップ (0113)** — herdr 版を別名 (`agent-tasks-herdr` 等) で
  ビルド/インストールし、稼働中 main 版の symlink・skill・補完・state dir を壊さない。「移行の制約」参照。
  これを最初に固めないと、以降のタスクをビルド/検証するだけで main 版を破壊しうる。

0. **[基盤・最優先] herdr socket/CLI クライアント層の導入** — agent-tasks から herdr を叩く共通ヘルパ
   (`herdr agent/pane/wait` 呼び出し or socket 直叩き、JSON パース、`HERDR_PANE_ID`/`HERDR_ENV` の参照)。
   以降のタスクが全部依存するので先に作る。herdr 前提を明示 (env 検出、未起動時のエラー方針)。
1. **[検証先行] blocked 実発火 + `events.subscribe` 実挙動の確認** — 承認プロンプトで実際に
   `wait agent-status --status blocked` が返るか、非標準プロンプトの取りこぼし、push 購読の CLI/socket 経路を
   実測。2 の設計を固める前提。
2. **spawn の herdr 化 (0079/0067)** — `agent start`/`pane split` + `pane run` で spawn を正規化し、
   空き pane 分配も実装。tmux `split-window`/`send-keys` ハックを撤去。
3. **状態検出の herdr 移行 (0020/0080)** — `session-hook` のマーカー間接推定を `wait agent-status` /
   `events.subscribe` (`pane.agent_status_changed`) の push に置換。`list` の SESSION 列・statusline を
   herdr 由来の状態で更新。旧マーカー機構を撤去。
4. **自 session_id / pane 特定の herdr 化 (0027)** — スクラッチパッド裏技を `agent get` の
   `agent_session.value` + `HERDR_PANE_ID` に置換。session-link/statusline の特定経路を簡素化。
5. **session-rename の herdr 化 (0085/0089/0092)** — send-keys `/rename` ハックを `herdr agent rename`
   に置換。claude.ai 側名称の穴は別問題として残す (herdr 内ラベルのみ正規化)。
6. ~~コスト計測の herdr 連携 (0101)~~ **完了 (0112): 現状維持**。herdr に transcript_path の read API が
   無いこと (socket 全 76 メソッド確認済) を確定。cost.go は session_id で glob しており置換不要。

## 状態検出/イベントの実機検証 (0107)

0109 (状態検出移行) の前提として、push 機構を実 herdr で検証した (スクラッチ pane + `report-agent` で
状態を注入し、`wait` / `events.subscribe` の反応を実測)。

### 状態の任意注入 — `pane report-agent`
- `herdr pane report-agent <pane> --source <id> --agent <label> --state idle|working|blocked|unknown`
  で**任意の状態を注入でき、`agent get` に反映される** (Claude でない shell pane でも付く)。
  manifest 検出とは別経路の「報告」。agent-tasks が能動的に状態を出したいとき (将来) に使える。

### `wait agent-status` (CLI push、単発待ち)
- **マッチ時に `pane.agent_status_changed` イベント JSON を stdout に返す** (例:
  `{"event":"pane.agent_status_changed","data":{"pane_id":"w3:p4","agent_status":"blocked","agent":"…"}}`)。
- **現在値にも即マッチ**する (既に blocked なら待たずに exit 0)。状態変化だけでなく現状確認にも使える。
- **タイムアウトは exit 1** + `timed out waiting for agent status change`。→ 0106 の `herdrWaitAgentStatus`
  はこのエラーで判定でき、そのまま使える。

### `events.subscribe` (socket stream、全体監視向け)
- CLI コマンドは無く **socket (`$HERDR_SOCKET_PATH`) の JSON-RPC メソッド**。
  `{"id":…,"method":"events.subscribe","params":{"subscriptions":[ <Subscription>… ]}}`。
- **Subscription は内部タグ付きオブジェクト** `{"type":"<event 名>", …固有フィールド}` (文字列ではない)。
  `pane.agent_status_changed` は **`pane_id` 必須 = pane 単位購読**。`pane.created`/`pane.closed` 等は
  pane_id 不要 (全体)。購読成功で `{"result":{"type":"subscription_started"}}` が返り、以降イベントが stream。
- **購読可能イベント全 20 種** (実測): `workspace.{created,updated,renamed,closed,focused}` /
  `worktree.{created,opened,removed}` / `tab.{created,closed,focused,renamed}` /
  `pane.{created,closed,focused,moved,exited,agent_detected,output_matched,agent_status_changed}`。
- 受信イベントの `event` 名はドット形 (`pane.agent_status_changed`) だが、pane ライフサイクル系は
  `data.type` がアンダースコア形 (`pane_created`) の場合がある (両表記が混在。パース時は data を見る)。
- 実測: `w3:p4` の working→idle→blocked→idle を注入 → 4 遷移すべて push 受信。
- **0109 への含意**: 1 pane の完了/ブロック待ちは `wait agent-status` (CLI) で十分。`list --watch` のような
  全セッション監視は `events.subscribe` に `pane.created`/`closed` で pane 集合を追い、各 pane に
  `agent_status_changed` を per-pane 購読する構成になる (socket 直叩きが必要)。

### blocked の実発火 (要検証#1) — 部分確認
- manifest の blocked ルール (`bash_permission_prompt`="do you want to proceed?" 等) は 0105 で確認済み。
  working スピナー (OSC タイトル) 稼働中は working が優先され、スピナーが消える承認待ちで blocked ルールが
  勝つ設計。**push 経路 (blocked への遷移を wait/subscribe で拾えること) は 0107 で実測済み**。
- **未実施**: 実際の Claude 許可プロンプトを発生させた end-to-end の blocked 発火。確実に起こすには
  テスト用 Claude セッションを spawn して非許可コマンドを走らせる必要があり、対話・トークンコストを伴う。
  → **opportunistic に確認**する (実運用で承認待ちが出たとき `agent get`/`wait` で blocked を確認)。
  0109 は push 機構が確定しているので、この点を残したまま着手してよい (blocked 判定自体は manifest 依存で
  herdr 側の責務)。

## worktime の herdr プラグイン化 (0114)

worktime (実稼働時間) の記録源を Claude の `session-hook` から **herdr プラグインの event hook** に
移した。これで状態ソースが herdr に一本化され、Claude 固有 hook への依存が worktime からも外れる
(SESSION 状態は 0109 で herdr 化済み)。**常駐デーモンは不要** — herdr が状態遷移ごとに短命コマンドを
起動する ephemeral モデル (session-hook と同型だが発火は herdr が管理)。

### プラグイン (repo 同梱)

- プラグイン root = **このリポジトリ自身**。manifest は root 直下の `herdr-plugin.toml`
  (`id`/`name`/`version`/`min_herdr_version` + `[[events]]`)。
- event hook: `on = "pane.agent_status_changed"` → `command = ["agent-tasks-herdr", "worktime-record"]`。
- 0117 (tui overlay) が同じ manifest に `[[panes]]` + `[[actions]]` を追記済み (1 プラグインに同居。下記)。

### イベント JSON の形 (実地確認済み)

env `HERDR_PLUGIN_EVENT_JSON` で渡り、フィールドは **`data` 配下にネスト**する:

```json
{"event":"pane_agent_status_changed",
 "data":{"type":"pane_agent_status_changed","pane_id":"w3:p8",
         "workspace_id":"w3","agent_status":"working","agent":"claude"}}
```

⚠️ **session_id は含まれない**ので、`worktime-record` は `data.pane_id` から `herdr agent get <pane>`
して `agent_session.value` を解決する。`agent_status` は `working`/`idle`/`blocked`/`done` を観測。
`working` で区間を開き、それ以外で閉じる (worktime.go の `workingIntervals` は変更不要)。

### 導入

```sh
herdr plugin link <このリポジトリ (or worktree) のパス>   # ローカル開発時
# 導入用スニペットは `agent-tasks-herdr worktime-record --print-plugin` でも出る
```

記録先は progName 由来の隔離 state dir (`~/.local/state/agent-tasks-herdr/sessions/worktime/`)。
稼働中の本体版 (`agent-tasks`) の worktime ログは壊さない (0113 の共存制約)。

### session-hook との関係

`session-hook` はもう worktime を書かない (0114 で追記を撤去。二重記録の解消)。SESSION 状態の
マーカー・フォールバック (0109) としては当面存続する。**session-hook 自体の完全撤去は別タスク**
(プラグインをドッグフードで十分検証してから)。

## tui を herdr overlay pane で開く (0117)

tmux では `agent-tasks tui` を `display-popup` で前面表示していた。これを herdr の **overlay pane**
で置換する。同一の `agent-tasks` プラグイン (0114 の manifest) に相乗りさせる (導入 = `plugin link` 1 回)。

### manifest ([[panes]] + [[actions]])

```toml
[[panes]]                       # tui を開く pane エントリポイント
id = "tui"
title = "agent-tasks"
placement = "overlay"           # アクティブ pane の上に一時 zoomed 表示、閉じると元へ戻る
command = ["agent-tasks-herdr", "tui"]

[[actions]]                     # 上記 pane を開く action (キーバインドの間接層。下記理由)
id = "open-tui"
title = "agent-tasks tui (overlay)"
contexts = ["workspace"]
command = ["agent-tasks-herdr", "tui-overlay"]   # アクティブ pane の cwd で開く (0124。下記)
```

### キーバインドが action 経由になる理由 (実地確認)

herdr の custom keybinding (`[[keys.command]]`) が受け付ける `type` は **`pane` / `shell` /
`plugin_action` の 3 つだけ** (docs 確認)。plugin の *pane entrypoint* を直接開く keybinding 型は
無い (`open_plugin_overlay_pane` は内部 API メソッドで、config からは叩けない)。そこで
「overlay pane を開く」ことを **plugin action** として公開し、キーはその action に割り当てる:

```toml
# ~/.config/herdr/config.toml
[[keys.command]]
key = "prefix+a"
type = "plugin_action"
command = "agent-tasks.open-tui"
description = "agent-tasks tui (overlay)"
```

action の command は herdr CLI をそのまま呼べる (`HERDR_*` env が注入され、`herdr plugin pane open`
に socket 越しで届く)。→ キー → action → overlay pane 起動、が成立する。

### 検証 (実 herdr)

`herdr plugin pane open --plugin agent-tasks --entrypoint tui --placement overlay` と
`herdr plugin action invoke agent-tasks.open-tui` (= キーが叩く経路) の両方で overlay が開き、tui が
一覧を描画、`plugin pane close` で元に戻ることを確認。`[[keys.command]]` の実キー押下だけは
クライアント側の手動確認 (config 追加 + reload)。Go 側の変更は無し (`tui` は既存コマンド)。

## overlay の tui を「アクティブ pane のプロジェクト」で開く (0124)

0117 時点では overlay pane の cwd がアクティブ pane と無関係 (プラグイン root) で、tui は常に
プラグイン root の project を出していた。0124 で「開いた時点でアクティブだった pane のプロジェクト」を
出すようにした。

### なぜ action にラッパーが要るか

- tui の現在 project は **cwd の git root basename** (`currentProject` → `mainRepoOf`)。よって overlay pane を
  **アクティブ pane の cwd で起動**すれば、tui は無変更でその project を出す (worktree 内でもメイン repo 名に解決)。
- 開かれる pane 自身はアクティブ pane を知らない (自 pane の `HERDR_PANE_ID` のみ)。アクティブ pane の
  情報は **action の呼び出しコンテキスト** = env `HERDR_PLUGIN_CONTEXT_JSON` にしか無い。static な manifest
  argv では展開できないので、この env を読むラッパー (`agent-tasks-herdr tui-overlay`) を action の command にする。

### HERDR_PLUGIN_CONTEXT_JSON の形 (実地確認済み)

```json
{"workspace_id":"wB","workspace_cwd":"…/workforce","tab_id":"wB:t1",
 "focused_pane_id":"wB:p1","focused_pane_cwd":"…/workforce",
 "focused_pane_agent":"claude","focused_pane_status":"working","invocation_source":"cli"}
```

`tui-overlay` は `focused_pane_cwd` (無ければ `workspace_cwd`) を取り出し、
`herdr plugin pane open … --cwd <それ>` を実行する。取れなければ `--cwd` を付けず開く
(tui はプラグイン root の project / 横断にフォールバック)。

### 検証 (実 herdr)

`workforce` の cwd を持つコンテキストを注入して `agent-tasks-herdr tui-overlay` を実行 → overlay の tui が
**workforce** の一覧を描画することを確認。コンテキスト無し (env 未設定) → `--cwd` 無しで開き
プラグイン root の project を出す (exit 0) ことも確認。

## 残課題・未検証

- **要検証#6 の一部未実施**: リモート SSH (`herdr --remote`)・スマホ attach の実用性。
- **blocked の実発火 end-to-end 未実施**: 上記のとおり push 経路は実測済み。実 Claude 承認プロンプトでの
  発火は opportunistic 確認に回す。
- **`TMUX` 併存の意味**: herdr が tmux をどう使っているか (共存/内包) を確認し、移行後に tmux 由来の
  前提が残るかを詰める。
- **claude.ai URL 突合**: ローカル UUID ↔ `session_<base62>` URL の対応は herdr でも解決しない。
  必要なら別経路 (transcript 内の情報等) を検討。

## ドッグフード準備: env による確実な振り分け (0118)

0113 で「別名バイナリ + 隔離 state dir」により**併存**はできたが、「**どちらの版が呼ばれるか**」は
まだ環境非依存だった (skill は関連度選択、CLI 名は symlink の付け替え次第)。0118 でこれを確実にした。

### 調査結果: skill 選択に env 連動の公式機構は無い

`claude-code-guide` で確認: skill は description との関連度でモデルが選ぶのみで、環境変数や
session 条件で自動有効/無効化する公式機構は無い (hook でも skill 呼び出し自体は拒否できない)。
同目的の skill を2つ (`agent-tasks` / `agent-tasks-herdr`) 入れると選択が不確実になる
→ **skill は常に1つに集約し、内部で env 分岐**する方針を採用 (「候補アプローチ」の 1+2)。

### 実装: CLI はルーター、skill は1つに統合

- **CLI の振り分け = PATH 解決によるルーター**。`$HOME/.local/agent-tasks-router/bin/agent-tasks`
  (`scripts/agent-tasks-router.sh`) が `HERDR_ENV=1` なら `~/.local/bin/agent-tasks-herdr`、
  そうでなければ `~/.local/bin/agent-tasks` (本体版) を絶対パスで `exec` する。ルーターのディレクトリを
  `~/.local/bin` より前に PATH へ通す (`~/.zshrc` に1行追加。一度だけの手動セットアップ)。
  絶対パス起動なので、本体版・herdr 版どちらの `make install` (symlink 張り替え) が何度走っても
  ルーター自体は上書きされない。
- **skill は常に `agent-tasks` の1つ**。herdr 版の Makefile を `SKILL_NAME := agent-tasks` に固定し
  (バイナリ名 `agent-tasks-herdr` とは独立)、`~/.claude/skills/agent-tasks` を herdr worktree の
  `skills/agent-tasks/SKILL.md` (env 分岐を内蔵した統合版) へ向ける。
- **SKILL.md 本文の env 分岐が要るのは spawn だけ**。state 表示 (`list` の SESSION 列) と
  session-rename は herdr 版 CLI 自体が `HERDR_ENV` を見て内部で herdr 経路/tmux フォールバックを
  選ぶ (0109/0111 で実装済み) ので、ルーターが herdr 版へ振り向けた時点で自動的に正しく動く。
  一方 **spawn は本体版に相当コマンドが無い** (tmux の raw コマンドが SKILL.md にしかない) ため、
  SKILL.md 側で `$HERDR_ENV`/`$TMUX` を見て手順そのものを分岐させている。

### 既知の制約: skill の canonical 名は「最後に install した方」が勝つ

CLI ルーターと違い、skill 名の重複を避ける PATH 相当の機構は無い。**main worktree で
`make install` を再実行すると `~/.claude/skills/agent-tasks` が本体版 (tmux 専用の旧内容) に
巻き戻る** (main 側の Makefile は変更していない — herdr → main 未マージのため)。
ドッグフード中に main 側で機能追加して `make install` した場合は、**herdr 永続 worktree
(`agent-tasks--herdr`) で `make install` を再実行**すれば統合版に戻る (完全に自動化はしていない。
将来 `doctor` 相当の検知を足すのは未着手の改善余地)。

### herdr 用の永続 worktree

タスクごとの worktree (`agent-tasks--NNNN`) は完了時に撤去されるため、herdr 版を継続ビルド/
install する母艦として `../agent-tasks--herdr` (branch `herdr` を直接チェックアウト) を新設した。
main の `../agent-tasks` (origin チェックアウト) に相当する、herdr 版の恒久的な作業ツリー。
以後の herdr ブランチの `make install` はここから実行する。
