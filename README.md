# agent-tasks

エージェント (Claude / Codex / ...) に開発させるタスクを管理するための一式。
**操作 (skill)** と **閲覧 (CLI)** を1リポジトリにまとめている。

タスクデータは各コードリポジトリの**外**、`~/agent-tasks-store/` に置く
(repo `agent-tasks` = ツール、`agent-tasks-store` = タスクの中身、という役割分担)。
リポジトリ内に置かないのは、ブランチごとにタスク状態がずれるのを避けるため。

## 構成

```
agent-tasks/
  skills/agent-tasks/SKILL.md   # 操作 (agent 用): /agent-tasks。登録/一覧/着手/spawn/完了/保留
  main.go store.go render.go    # 閲覧 (人間用) CLI 本体 (Go, 依存ゼロ)
  store_test.go                 # テスト
  Makefile                      # build / install / test
  bin/agent-tasks               # ビルド成果物 (gitignore)
```

- 閲覧 CLI は Go 製。機能追加していく前提で、frontmatter パース (`store.go`) /
  表示 (`render.go`) / コマンド振り分け (`main.go`) に分けている。
- `skills/` 直下に置くのは、Claude 以外のエージェント (Codex / Cursor / Gemini) からも
  標準位置として見つけやすくするため。

## データの場所

```
~/agent-tasks-store/
  <project>/            # コードリポジトリ root の basename
    <NNNN>-<slug>.md    # 1 タスク = 1 Markdown ファイル
```

`AGENT_TASKS_STORE` で場所を変更可。データ形式の詳細は `~/agent-tasks-store/README.md` を参照。

## インストール

`make install` で CLI をビルドし、CLI (`~/.local/bin`) と skill (`~/.claude/skills`)
を symlink する (`~/.local/bin` が PATH にある前提):

```sh
make install
```

中身は次と等価:

```sh
go build -o bin/agent-tasks .                            # CLI をビルド
ln -sf  "$(pwd)/bin/agent-tasks"    ~/.local/bin/agent-tasks
ln -sfn "$(pwd)/skills/agent-tasks" ~/.claude/skills/agent-tasks
```

- skill は symlink なので編集すれば即反映。
- CLI は Go バイナリなので、ソースを変えたら `make build` で再ビルドする
  (symlink 先のバイナリが更新される)。

## 使い方

### 操作 (エージェントに対して)

| 操作 | 言い方の例 |
| --- | --- |
| 登録 | 「〜というタスクを作って」「/agent-tasks create」 |
| 一覧 | 「タスク一覧」「/agent-tasks list」 |
| おすすめ | 「次に何をやるべき?」「おすすめは?」「/agent-tasks recommend」(衝突回避・価値で着手候補を提示。着手はしない) |
| 着手 | 「タスク 0001 に着手」「/agent-tasks start 0001」(git worktree で並行開発) |
| 別 pane で着手 | 「別 pane で 0001 をやって」「/agent-tasks spawn 0001」(tmux の別 pane に新セッション) |
| 完了 | 「0001 を完了」「/agent-tasks done 0001」 |
| 保留 | 「0001 を保留」「/agent-tasks block 0001」 |
| worktree 設定の展開 | 「worktree 設定を入れて」「/agent-tasks scaffold」(firebase/rails を検出して雛形生成) |

**並行開発**: 別々のエージェントセッションでそれぞれ別タスクを `start` すると、
タスクごとに git worktree + ブランチが切られ、衝突なく同時に開発できる。
`spawn` は tmux の別 pane を**メインリポ root で**開き、新セッションに `start` を指示するだけ
(fire-and-forget。worktree 作成・session 紐づけ・着手は子の `start` が担当)。親はポーリングせず、
起動確認は `agent-tasks --watch` の SESSION 列で行う。tmux 外では貼り付け用コマンドを表示するだけ。

**worktree 作成後フック**: start は worktree を作ったあと `agent-tasks worktree-init` を呼び、
環境の干渉 (worktree に `.env` も `node_modules` も無い問題) を解消する。コードリポジトリ root に
置いた 2 ファイルを参照する (両方任意・無ければ no-op):

- `.worktreeinclude` — コピーする gitignored ファイル (`.gitignore` 構文。Claude Code の `--worktree` と
  同名・互換)。tracked ファイルは複製しない。
- `.worktree-post-create` — worktree 内で実行するスクリプト (依存インストール / ポート分離 / DB 準備など)。
  `AGENT_TASKS_WORKTREE` / `AGENT_TASKS_MAIN` / `AGENT_TASKS_PROJECT` が渡る。

コピーは既存を上書きせず、post-create は worktree ごとに一度だけ実行する (冪等。`--force` で再実行)。

この 2 ファイルの雛形は **`agent-tasks scaffold-worktree [stack]`** でスタック別に生成できる
(firebase / rails 同梱。stack 省略で自動検出、`--list` で一覧、`--print`/`--dry-run` で書き出さず
stdout にプレビュー)。テンプレはバイナリに同梱しており、スタックを増やすには
`templates/<stack>/{worktreeinclude,post-create}` を足すだけ。例えば firebase なら emulator ポートを
worktree ごとに一意化する post-create が入る。rails テンプレは実運用を想定した CONFIG 切り替え式
(`DOTENV_TARGET`: `.env.local` か `.env` 追記 / `DB_MODE`: 空スキーマか dev DB 複製 /
`SERVER_MODE`: PORT env か puma-dev) で、pnpm 検出・Redis DB 分離・マルチテナント host のレシピを含む。

### 閲覧 (ターミナルから)

```sh
agent-tasks                      # 現在 project の未完了タスク一覧 (既定。done は非表示)
agent-tasks --all-projects       # 全 project を横断して一覧
agent-tasks --all                # done も含めて表示 (-a も可)
agent-tasks --status in-progress # status で絞り込み (既定どおり現在 project に絞られる)
agent-tasks --project webapp     # 別 project を指定
agent-tasks --watch              # 一覧を自動更新表示 (-w。--interval <秒> で間隔、既定 2。Ctrl-C で終了)
agent-tasks -w --status in-progress # in-progress を常時モニタ (他の絞り込みと併用可)
agent-tasks show webapp 0001     # 1 タスクの全文
agent-tasks show 0001            # project 省略時は現在 project のタスク
agent-tasks edit                 # ストアをエディタで開く (既定 code)
agent-tasks edit webapp 0001     # 1 タスクをエディタで開く
agent-tasks edit 0001            # project 省略時は現在 project のタスク
agent-tasks show webapp 1        # ID は短縮形でも可 (1 -> 0001)
agent-tasks status               # ストアの未同期状態 (未コミット/未push) を1行表示
agent-tasks sync                 # ストアを add/commit/push して同期
agent-tasks sync --no-push       # commit まで (push しない)
agent-tasks worktree-init ../foo--0001 # worktree 作成後フック (start/spawn が自動で呼ぶ)
agent-tasks scaffold-worktree    # worktree 設定の雛形を生成 (stack 自動検出。--list/--dir/--force)
agent-tasks scaffold-worktree rails --print # 書き出さず stdout にプレビュー (--dry-run も可)
agent-tasks doctor               # id 重複と id/ファイル名不一致を点検 (既定は全 project 横断)
agent-tasks doctor --project webapp # 1 project だけ点検
agent-tasks session-hook --print-config # セッション状態表示用 hook の設定例を出力
agent-tasks statusline --print-config # 実行中タスクを出す status line の設定例を出力
agent-tasks completion bash      # bash 補完スクリプトを stdout 出力 (zsh も可)
agent-tasks where                # データディレクトリのパス
```

既定では**現在のコードリポジトリ (project) のタスクだけ**を表示する。横断したいときは
`--all-projects`、別 project を見たいときは `--project <name>` を使う。現在 project は cwd の
git リポジトリから判定し (リンク worktree 内でもメイン repo 名に解決する)、git 外なら自動で横断にフォールバックする。

`show` / `edit` は `<project>` を省略すると現在 project のタスクとして解決する (`show 0001`)。
git 外などで現在 project を判定できないときは `<project> <id>` の明示指定を促す。
`<id>` は数値なら4桁ゼロ埋めに正規化して照合する。`1` でも `0001` でも同じタスクを指せる。

`status` は `~/agent-tasks-store` (git repo) の未コミットファイル数と upstream に対する
ahead/behind を1行で表示する (例: `未コミット 3 ファイル / 未 push 2 コミット (origin/main)`)。
同期済みなら `クリーン (同期済み)` を出す。未同期があれば exit 1 を返すので、prompt や
スクリプトから「sync が必要か」を終了コードで判定できる。

`sync` は `~/agent-tasks-store` (git repo) を `add -A` → コミット → `pull --rebase` → `push` する。
コミットメッセージは変更されたタスクから自動生成する (例: `tasks: agent-tasks/0005 (in-progress)`)。
リモート未設定なら push をスキップ、upstream 未設定なら初回 `push -u` で追跡を設定する。

`edit` のエディタは `AGENT_TASKS_EDITOR` > `VISUAL` > `EDITOR` の順、未設定なら `code`。

`doctor` はストアを点検し、(1) 同一 project 内で同じ id を持つファイルが複数ある状態 (並行 create の
採番競合で起きる)、(2) frontmatter の `id:` がファイル名先頭の連番とずれている状態、(3) 着手/完了
日時の矛盾 (`completed_at` があるのに `started_at` が無い / `completed_at` が `started_at` より前 /
`done` なのに `completed_at` が無い記録漏れ)、(4) blocked の記録/クリア漏れ (blocked でないのに
`blocked_at`/`blocked_reason` が残存 / `blocked` なのに `blocked_at` が無い)、(5) parse 失敗で一覧から
無言で落ちるファイル (長大な1行・権限など) を報告する。(3)(4) は記録・クリアを skill が行うため、
CLI 側の検査が漏れの防御線になる。問題があれば終了コード 1 を返すので、CI や着手前チェックに使える。
既定は全 project を横断し、`--project <name>` で 1 project に絞れる。

### セッション状態 (working / waiting) の表示

並行で複数 pane を回しているとき、「どのセッションが自分の入力待ちで止まっているか」を一覧で
分かるようにできる。`list` は in-progress のタスクがあると `SESSION` 列を出し、各セッションが
**working** (処理中) / **waiting** (入力・許可待ち) / **ended** (終了) を表示する (`?` はマーカー未取得)。

仕組みは Claude Code の **hook**。各セッションが状態変化のたびに `agent-tasks session-hook` を呼び、
マーカー (`~/.local/state/agent-tasks/sessions/`、`AGENT_TASKS_STATE_DIR` で変更可) を更新する。
hook が受け取る `session_id` はローカル CLI の UUID で frontmatter の `session:` (claude.ai の URL) とは
一致しない。突合の**主経路は `session-link` (session_id ベース)** で、spawn かどうかに依存しない:

- **session 経路 (主)**: hook が `session_id` キーのマーカー (cwd 付き) を常に書き、start 時に
  `agent-tasks session-link <id>` がセッションを `<project>--<NNNN>.link.json` に記録する。直接 start でも
  spawn 経由でも、セッションの cwd はメインリポ (worktree の外) なのでこの経路で紐づく。session_id の
  特定方法は 2 通り:
  - `--session <id>` で**明示**する (推奨)。Claude Code は自分のローカル session_id を知り得るので
    これを渡す。cwd 逆引きの曖昧性を完全に回避できる。
  - 省略時は hook が書いた sess マーカーを**現在 cwd で逆引き**する (agent 中立のフォールバック)。
- **worktree 経路 (補助)**: cwd が worktree 内のセッションがあれば、その git root basename
  (`<project>--<NNNN>`) のマーカーでも突合する。現行フロー (cwd=メインリポ) では通常使われない。

`list` は session 経路と worktree 経路のマーカーの新しい方を採用する。マーカー・link はいずれも
マシンローカルな揮発情報なので、git 同期されるストアには置かない (state dir に置く)。

**層の切り分け (agent 中立 / agent 固有)**: マーカー・link の保管と突合は CLI 側で **agent 中立**。
一方、状態の信号源 (hook の発火) と「自分の session_id をどう知るか」は本質的に **agent 固有**なので、
SKILL の start 手順に agent ごとに記す。Claude Code は hook (working/waiting) + `--session` で
self-id を明示。他 agent は同形式のマーカーを書ければ載るが、現状は未整備。

> 制限: `--session` を使わず cwd 逆引きに頼る場合、複数のセッションが**同じ cwd** (同じメインリポ) で
> 同時に走っていると最後に活動したセッションを拾うため取り違える可能性がある。現行フローでは spawn 子も
> メインリポ root で動く (cwd が共通) ため、並行時は **`--session` での明示を推奨**する。

導入は `~/.claude/settings.json` に hook を 1 度追加するだけ:

```sh
agent-tasks session-hook --print-config   # 貼り付け用スニペットを表示
```

`UserPromptSubmit`→working、`Stop`→waiting、`Notification`(権限/アイドル待ち)→waiting、`SessionEnd`→ended
にマッピングされる。内蔵の watch モードと組み合わせると「どの pane が応答待ちか」を常時モニタできる
(`agent-tasks -w --all-projects --status in-progress`)。

### 実行中タスクの表示 (status line)

`list` の `SESSION` 列が「俯瞰」(どの pane が応答待ちか) なのに対し、各 pane **自身**に
「このセッションが今どのタスク (NNNN) を実行中か」を常時表示できる。Claude Code の
**status line** (プロンプト下部) に出すので、複数 pane を並行で回していても各 pane で
今やっているタスクがぱっと分かる。

```sh
agent-tasks statusline --print-config   # settings.json 用スニペットを表示
```

`~/.claude/settings.json` の `statusLine` に `agent-tasks statusline` を登録すると、Claude Code が
セッション情報を JSON で stdin に渡して呼び、`agent-tasks #0037 タイトル… [in-progress]` のような
1 行を返す。現在タスクの特定は 2 経路:

- **session_id 経路 (主)**: 通常フローではセッションの cwd はメインリポ (worktree の外) なので、
  JSON の `session_id` を `session-link` の `<project>--<NNNN>.link.json` で逆引きしてタスクを引く
  (上記「セッション状態」の link をそのまま再利用する)。
- **cwd 経路 (補助)**: cwd の git root basename が `<project>--<NNNN>` 形式 (worktree 内で起動した
  セッション) ならそこから直接引く。

どちらでも特定できなければ何も表示しない (空の status line)。status line はパイプ出力 (非 TTY) だが
端末に表示されるので色は既定で出す (`NO_COLOR` / `--color never` は尊重)。
status line を壊さないよう、入力エラーや解析失敗は黙って空表示で抜ける。

> 主経路が `session-link` なので、start 時に `session-link` がセッションを紐づけていることが前提
> (start 手順に含まれる)。hook + session-link を導入していれば、`SESSION` 列 (俯瞰) と status line
> (各 pane 自身) が同じ情報源で揃う。

### シェル補完 (bash / zsh)

サブコマンド (`list` / `show` / `doctor` / ...) と列挙できるフラグ値 (`--status` の
`todo|in-progress|blocked|review|done`、`--color` の `always|auto|never` など) をタブ補完できる。
補完スクリプトは `agent-tasks completion <shell>` が stdout に出力する (依存ゼロ・外部フレームワーク不使用)。

```sh
# bash: そのセッションで有効化
source <(agent-tasks completion bash)
# bash: 恒久化 (bash-completion 導入済みなら)
agent-tasks completion bash > ~/.local/share/bash-completion/completions/agent-tasks

# zsh: そのセッションで有効化 (.zshrc に書く)
source <(agent-tasks completion zsh)
# zsh: 恒久化 (fpath のディレクトリに置いて再ログイン)
agent-tasks completion zsh > "${fpath[1]}/_agent_tasks"
```

`make install-completions` でも配置できる (bash は `~/.local/share/bash-completion/completions/`、
zsh は `~/.local/share/zsh/site-functions/` に書き出す。`PREFIX` でルートを変更可)。

> 補完は**静的のみ** (サブコマンド名と列挙できるフラグ値)。`--project` の候補や id 引数の
> **動的補完**は `agent-tasks` を呼んで列挙する必要があり、別タスクに切り出している。

### blocked タスクの可視化 (理由・経過)

`list` は `blocked` のタスクがあると `BLOCKED` 列を出し、**保留からの経過時間** (`3d` / `5h` /
`12m`) を表示する。長く放置された blocked (既定 7 日超) は警告色で目立たせるので、止まったまま
忘れられたタスクに気づける。保留理由は `TITLE` に括弧書きで添える (長い理由は折り返さず丸める)。

経過は `updated` ではなく専用の **`blocked_at`** (保留にした日時) から測る。`updated` はあらゆる
status 更新で動くため、「保留してからの経過」とはずれるから。理由は **`blocked_reason`** に持つ。
どちらも `block` 操作で frontmatter に記録され、`start` / `done` で blocked を抜けるとクリアされる
(`blocked_at` 未記録の古い blocked は `?` 表示)。`agent-tasks --status blocked` で一覧できる。

### 色出力

既定 (`--color=auto`) は stdout が端末のときだけ色を付ける。`watch` などパイプ経由では
端末でなくなるため色が消える。色を制御したいときは:

```sh
agent-tasks --color=always …   # 端末でなくても色を出す (パイプ向け)
agent-tasks --color=never …    # 常に無色
watch --color agent-tasks --color=always   # 外部 watch で色付き監視 (内蔵 --watch を使うなら不要)
```

常時モニタは内蔵の `agent-tasks --watch` (端末なので色も自動で出る) が手軽。外部 `watch` を使う
場合のみ上記の `--color=always` が要る。

環境変数も尊重する: `NO_COLOR` (非空) で無効化、`FORCE_COLOR` (非空) で強制。
優先順位は `--color` フラグ > `NO_COLOR` > `FORCE_COLOR` > 端末判定。

### 日時フィールド

frontmatter の時刻系 (`created` / `updated` / `started_at` / `completed_at` / `blocked_at`) は
**ISO8601 日時** (ローカルオフセット込み。例 `2026-06-28T14:30:00+09:00`) で持つ。skill は
`date --iso-8601=seconds` で記録する。一覧の `UPDATED` 列は情報過多にならないよう**日付だけ**に
丸めて表示し、時刻まで見たいときは `show` で全文を見る。パーサは日付のみ (`2026-06-28`) の旧データも
読めるので、移行は後方互換。

`started_at` (start で記録・初回着手を保持) と `completed_at` (done で記録) で**着手〜完了の所要期間
(リードタイム)** を追える。`show` は記録があれば末尾に「着手 / 完了 / リードタイム」(進行中なら経過) を
要約表示する。done→in-progress で再オープンすると `completed_at` はクリアされる。

## 今後

- Web ダッシュボード化 (このリポジトリ内で発展させる)
- blocked の経過に加え、相対表示の一覧オプションなど表示の作り込み
