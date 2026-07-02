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
- → **herdr は transcript_path を内部的に把握している**。タスク 0101 (per-task コスト計測、現状は
  transcript JSONL を自前探索) は、herdr 経由で transcript パスを引ける余地がある (現状 CLI の
  `agent get` は path を surface していないが、内部保持はしている = 将来の統合点)。
- **working/idle 検出は OSC タイトル駆動が最優先** (`osc_title_working` 優先度 1100 = 点字スピナー
  `⠂`、`osc_title_idle` = `✳`)。tmux `capture-pane` が alt-screen で空になる問題 (0067) と異なり、
  herdr は OSC タイトル + 画面領域を alt-screen でも読める。
- agent-tasks 自身の `session-hook` と herdr の hook は**現在も並存**して動いている (settings.json に両方)。

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
| per-task コスト計測 API 無い (0101) | transcript JSONL 自前集計・揮発・目安表記 | herdr が **transcript_path を内部保持** (hook 経由)。CLI で surface されれば探索が楽に | **改善余地** (現状 CLI 未 surface。将来の統合点) |

## 設計方針の選択肢

再プラットフォーム化の是非が最大の論点。agent-tasks の「tmux + hook マーカー」層
(spawn の split-window/send-keys、session-hook のマーカー、`tmux capture-pane` 回避策) を
herdr socket API に載せ替えるかどうか。

### 案 A: 全面移行 (herdr 前提に一本化)
- **利点**: 妥協点の多く (0027/0079/0067/0020/0080/0067/0089/0092) が素直に解消。コードが簡潔になる。
  状態検出・pane 送出・待機が一級 API になる。
- **欠点**: herdr 非利用環境で動かなくなる。herdr への依存 (バイナリ・server・manifest 更新) が前提化。
  スマホ/リモートの実用性が未検証。

### 案 B: 両対応 (tmux/herdr のアダプタ層を挟む)
- **利点**: 段階移行できる。herdr 無し環境でも従来どおり。リスク分散。
- **欠点**: 抽象層のメンテコスト。両系統のテストが要る。「herdr があれば正規経路、無ければ従来ハック」の
  分岐が各所に増える。

### 役割分担 (どちらの案でも共通の指針)
- **セッション/pane/状態の取得は herdr に委譲**、タスク管理 (frontmatter/PR/コスト/稼働時間/blocked 理由/
  複数プロジェクト) は agent-tasks が持つ。
- herdr のサイドバー (blocked/working/idle/done 俯瞰) は `list` の SESSION 列と機能が重なる。
  重複を herdr に寄せるか、agent-tasks 側で独自の付加価値 (タスク紐付け) を保つか要判断。
- **agent 中立性は保てる/むしろ向上**。herdr は複数 agent 統合 (codex/opencode 等) を持つので、
  「保管・突合は agent 中立、信号源は herdr 統合」に整理しやすい。

## 作り直しタスクの優先順位 (後続タスク案)

1. **[最有力] 空き pane へ start 送信 / pane 分配 (0079/0067)** — herdr 前提なら素直。
   `agent start`/`pane split` + `pane run` で spawn を正規化。tmux split-window/send-keys ハックを置換。
2. **状態検出の herdr 移行 (0020/0080)** — `session-hook` のマーカー間接推定を、
   `wait agent-status` / `events.subscribe` (`pane.agent_status_changed`) の push に置換。
   `list` の SESSION 列を herdr 由来の状態で更新。
3. **自 session_id / pane 特定の herdr 化 (0027)** — スクラッチパッド裏技を `agent get` +
   `HERDR_PANE_ID` に置換。session-link/statusline の特定経路を簡素化。
4. **session-rename の herdr 化 (0085/0089/0092)** — send-keys `/rename` ハックを `herdr agent rename`
   に置換 (herdr 内ラベル)。ただし claude.ai 側名称の穴は別問題として残す。
5. **[検証先行] blocked 検出の取りこぼし調査** — 実際に許可プロンプトを出して `agent_status=blocked` に
   なるか、非標準プロンプトを拾うかを確認してから 2 を確定する。
6. **[要調査] コスト計測の herdr 連携 (0101)** — herdr が保持する transcript_path を CLI で引けるか
   (herdr 側の機能要望 or 現状の内部保持を利用する術) を調べる。

## 残課題・未検証

- **要検証#6 の一部未実施**: リモート SSH (`herdr --remote`)・スマホ attach の実用性。
- **blocked の実発火テスト未実施**: 本メモは `agent explain` のルール定義から判定。実際に承認待ちを
  発生させて `wait agent-status --status blocked` が返るかは後続 (優先順位 5)。
- **`events.subscribe` の実挙動未確認**: push 購読の CLI/socket 経路を実測していない。
- **`TMUX` 併存の意味**: herdr が tmux をどう使っているか (共存/内包) を確認し、移行後に tmux 由来の
  前提が残るかを詰める。
- **claude.ai URL 突合**: ローカル UUID ↔ `session_<base62>` URL の対応は herdr でも解決しない。
  必要なら別経路 (transcript 内の情報等) を検討。
