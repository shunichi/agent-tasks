// Command agent-tasks は ~/agent-tasks-store を横断してエージェント開発タスクの
// 進捗を表示する CLI。エージェント (claude / codex / ...) を起動せずに進捗を見る。
//
// データの場所は AGENT_TASKS_STORE で上書きできる (既定: ~/agent-tasks-store)。
package main

import (
	"bytes"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// usageError は使い方の誤り (未知オプション/引数不足など) を表す。
// main がこれを受け取ると、メッセージに続けて usage を表示し exit 2 する。
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error {
	return &usageError{msg: fmt.Sprintf(format, a...)}
}

// silentExit はメッセージを出さずに指定コードで終了したいときに返す。
// doctor がレポートを自前で stdout に出したあと、問題ありを終了コードで
// 伝える (CI / prompt から使えるように) ために使う。
type silentExit struct{ code int }

func (e *silentExit) Error() string { return "" }

func main() {
	// --color はサブコマンド共通のグローバルフラグなので、コマンド判定より先に抜き取る。
	mode, args, err := extractColorFlag(os.Args[1:])
	if err == nil {
		colorMode = mode
		err = dispatch(args)
	}
	if err != nil {
		var se *silentExit
		if errors.As(err, &se) {
			os.Exit(se.code)
		}
		var ue *usageError
		if errors.As(err, &ue) {
			fmt.Fprintf(os.Stderr, "%s\n\n", err)
			usage(os.Stderr)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// extractColorFlag は引数列から --color を取り除き、その値 (always|auto|never) を返す。
// 形式は --color=always と --color always の両対応。未指定なら auto。
func extractColorFlag(args []string) (mode string, rest []string, err error) {
	mode = "auto"
loop:
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			// オプション終端。以降は `--` ごとサブコマンドへそのまま渡す (サブコマンド側の
			// パーサが終端を解釈し、`--color` 等を奪わせない)。
			rest = append(rest, args[i:]...)
			break loop
		case a == "--color":
			if i+1 >= len(args) {
				return "", nil, usagef("--color requires a value (always|auto|never)")
			}
			i++
			mode = args[i]
		case strings.HasPrefix(a, "--color="):
			mode = strings.TrimPrefix(a, "--color=")
		default:
			rest = append(rest, a)
		}
	}
	switch mode {
	case "always", "auto", "never":
		return mode, rest, nil
	default:
		return "", nil, usagef("--color must be always|auto|never (got %q)", mode)
	}
}

func dispatch(args []string) error {
	cmd := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}
	// -h / --help はサブコマンド扱いされず引数として流れてくるので先に拾う
	// (例: `agent-tasks -h`, `agent-tasks list -h`)。
	if cmd == "help" || slices.Contains(args, "-h") || slices.Contains(args, "--help") {
		usage(os.Stdout)
		return nil
	}
	// -h と同様、--version / -V もサブコマンド扱いされず引数として流れてくるので先に拾う。
	if cmd == "version" || slices.Contains(args, "--version") || slices.Contains(args, "-V") {
		return cmdVersion(nil)
	}

	switch cmd {
	case "list":
		return cmdList(args)
	case "tui":
		return cmdTUI(args)
	case "show":
		return cmdShow(args)
	case "edit":
		return cmdEdit(args)
	case "status":
		return cmdStatus(args)
	case "sync":
		return cmdSync(args)
	case "worktree-init":
		return cmdWorktreeInit(args)
	case "worktree-remove":
		return cmdWorktreeRemove(args)
	case "scaffold-worktree":
		return cmdScaffoldWorktree(args)
	case "doctor":
		return cmdDoctor(args)
	case "session-hook":
		return cmdSessionHook(args)
	case "session-link":
		return cmdSessionLink(args)
	case "session-rename":
		return cmdSessionRename(args)
	case "statusline":
		return cmdStatusline(args)
	case "completion":
		return cmdCompletion(args)
	case "completion-values":
		return cmdCompletionValues(args)
	case "alloc-id":
		return cmdAllocID(args)
	case "archive":
		return cmdArchive(args)
	case "auto-archive":
		return cmdAutoArchive(args)
	case "unarchive":
		return cmdUnarchive(args)
	case "issue":
		return cmdIssue(args)
	case "report":
		return cmdReport(args)
	case "open":
		return cmdOpen(args)
	case "where":
		fmt.Println(storeDir())
		return nil
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		usage(os.Stderr)
		os.Exit(2)
		return nil
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `agent-tasks — エージェント開発タスクの横断ビュー

USAGE:
  agent-tasks [list]                 現在 project の未完了タスクを一覧 (既定。done は非表示)
  agent-tasks --all-projects         全 project を横断して一覧
  agent-tasks --all | -a             done も含めて表示
  agent-tasks --status <status>      status で絞り込み (todo/in-progress/blocked/review/done)
  agent-tasks --search <q>            タイトル部分一致で検索 (大小無視。--grep も可)。--content で本文も対象
  agent-tasks --project <name>       project を指定 (別 project も可。繰り返し指定でその集合だけを横断)
  agent-tasks --projects a,b,c       複数 project をカンマ区切りでまとめて横断 (--project の集合版)
  agent-tasks --watch | -w           一覧を一定間隔で自動更新表示 (--interval <秒>、既定 2。Ctrl-C で終了)
  agent-tasks --recent [N]           最近完了したタスクを completed_at 降順で上位 N 件 (既定 10)
  agent-tasks --archived             アーカイブ済みタスクだけを一覧 (通常は非表示。--recent と排他)
  agent-tasks tui                    一覧+詳細をインタラクティブに閲覧する常駐ビューワー (自動更新)。
                                     ↑↓で選択し右に詳細。a:done切替 s:status p:project r:更新 q:終了。
                                     --status/--project/--all-projects/--all/--interval を受ける (端末専用)
  agent-tasks --json                 一覧を JSON 配列で出力 (機械可読。既存フィルタと併用可)
  agent-tasks show [<project>] <id> [--archived] [--json]  1タスクの全文を表示 (--json で機械可読
                                     オブジェクト。--archived でアーカイブ済みタスクを開く)
  agent-tasks edit [[<project>] <id>] ストア (引数なし) か1タスクをエディタで開く
  agent-tasks open [<project>] <id>  タスクの worktree (作業ツリー) をエディタで開く
                                     (worktree: を解決。エディタは edit と同じ。撤去済みならエラー)
  agent-tasks status                 ストアの未同期状態 (未コミット/未push) を1行表示
                                     (未同期があれば exit 1。sync が要るかの確認に使う)
  agent-tasks sync [[<project>] <id>] [--path <p>]... [--no-push]
                                     ストアを add/commit/push して同期 (--no-push で commit まで)。
                                     <id> / --path 指定でそのタスクだけを stage (scoped。並列セッションの
                                     書きかけを巻き込まない)。指定なしは全体 (add -A)。並列 sync は
                                     ストアロックで直列化し、push 競合は pull --rebase で自動リトライ
  agent-tasks worktree-init <dir>    worktree 作成後フック: .worktreeinclude をコピーし
                                     .worktree-post-create を実行 (start/spawn が呼ぶ。--force で再実行)
  agent-tasks worktree-remove <dir>  worktree 撤去フック: .worktree-post-remove を実行してから
                                     git worktree remove (done が呼ぶ。cwd が中なら中止。未コミット/
                                     フック失敗で中止、捨てるなら --force。--hook-only でフックだけ)
  agent-tasks scaffold-worktree [stack]  worktree 設定 (.worktreeinclude/.worktree-post-create/
                                     .worktree-post-remove) をプロジェクトに展開 (stack 省略で自動検出。
                                     --list/--dir/--force。--print/--dry-run で書き出さず stdout にプレビュー)
  agent-tasks doctor [--project <name>] id 重複と id/ファイル名の不一致を点検 (既定は全 project 横断。
                                     問題があれば exit 1。CI / 着手前チェックに使う)
  agent-tasks session-hook [--print-config]  Claude Code の hook から呼ぶ。stdin の JSON を読んで
                                     セッションの working/waiting を記録する (--print-config で設定例を出力)
  agent-tasks session-link [<project>] <id> [--session <id>]  現在のセッションをタスクに紐づける
                                     (start 手順が呼ぶ)。同一セッション start でも SESSION 状態が出る。
                                     --session で自分の session_id を明示 (省略時は cwd 逆引き)
  agent-tasks statusline [--print-config]  Claude Code の status line から呼ぶ。stdin の JSON を読んで
                                     この pane が実行中のタスクを 1 行表示 (--print-config で設定例を出力)
  agent-tasks completion bash|zsh    シェル補完スクリプトを stdout に出力
                                     (例: source <(agent-tasks completion bash))
  agent-tasks completion-values projects|ids [--project <name>] [--with-title] [--archived]
                                     動的補完の候補を1行ずつ出力 (補完スクリプトが内部で呼ぶ)。
                                     projects=ストアの project 一覧 / ids=その project の id 一覧。
                                     ids に --with-title で "<id>\t<title>" 形式 (zsh の説明付き補完用)、
                                     --archived でアーカイブ済みの id を列挙 (unarchive 補完用)
  agent-tasks alloc-id --slug <slug> [--project <name>] [--pull]
                                     タスク id を原子的に採番し予約ファイルを作成、その絶対パスを
                                     stdout に出力 (skill の create が中身を書き込む)。project 省略時は
                                     現在 project。--pull で採番前にストアを pull --rebase
  agent-tasks archive [<project>] <id>   タスクを <project>/archive/ へ退避 (削除しない)。通常の
                                     list / -a / doctor に出なくなる。閲覧は list/show の --archived
  agent-tasks auto-archive [--older-than <days>] [--project <name>|--all-projects] [--dry-run]
                                     完了後に一定日数 (既定 30) を過ぎた done タスクを一括で
                                     <project>/archive/ へ退避 (古い完了タスクを一覧から片付ける)。
                                     completed_at 無しや review/in-progress は対象外。スコープは
                                     list と同じ。--dry-run で対象一覧のみ表示 (移動しない)
  agent-tasks unarchive [<project>] <id> アーカイブ済みタスクを通常ディレクトリへ戻す
  agent-tasks report [--month [YYYY-MM]] [--week [YYYY-MM-DD]] [--since <d> --until <d>]
                                     一定期間に完了したタスク (done かつ completed_at が期間内) を
                                     markdown で出力 (既定は今月)。所要時間と合計/平均サマリ付き。
                                     スコープは list と同じ (--project / --all-projects で横断はセクション分け)
  agent-tasks issue [<project>] <id> [--repo owner/repo]  タスクを GitHub issue として共有
                                     (起票し URL を frontmatter issue: に記録。連携済みなら本文を更新)。
                                     --repo 省略時は cwd のコード repo を gh で推論。gh CLI が必要
  agent-tasks where                  データディレクトリのパスを表示
  agent-tasks version | --version | -V  ビルド元の commit + CalVer を表示 (タグ運用なし)
  agent-tasks help | -h | --help     このヘルプ

OPTIONS:
  --color always|auto|never          色出力の制御 (既定 auto = TTY のときだけ色)。
                                      watch 経由で色を出すなら: watch --color agent-tasks --color=always

ENV:
  AGENT_TASKS_STORE    タスクデータの場所 (既定: ~/agent-tasks-store)
  AGENT_TASKS_EDITOR   edit で使うエディタ (既定: code。VISUAL/EDITOR も参照)
  NO_COLOR             設定 (非空) で色を無効化 (https://no-color.org/)
  FORCE_COLOR          設定 (非空) で色を強制 (auto 時。--color=never が優先)
  AGENT_TASKS_STATE_DIR  session マーカーの置き場 (既定: ~/.local/state/agent-tasks/sessions)
`)
}

func cmdList(args []string) error {
	var filterStatus string
	var filterProjects []string
	var searchQuery string
	searchContent := false
	showAll := false
	allProjects := false
	archived := false
	watch := false
	jsonOut := false
	recent := false
	recentN := recentDefaultN
	interval := 2 * time.Second
	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch a {
		case "--status":
			v, err := s.value("--status")
			if err != nil {
				return err
			}
			filterStatus = v
		case "--project":
			// 繰り返し可。複数指定でその集合だけを横断表示する (後方互換: 単一指定は従来どおり)。
			v, err := s.value("--project")
			if err != nil {
				return err
			}
			filterProjects = append(filterProjects, v)
		case "--projects":
			// カンマ区切りでまとめて指定する糖衣 (--project a --project b と同じ集合)。
			v, err := s.value("--projects")
			if err != nil {
				return err
			}
			filterProjects = append(filterProjects, splitProjects(v)...)
		case "--search", "--grep":
			v, err := s.value(a)
			if err != nil {
				return err
			}
			searchQuery = v
		case "--content", "--full":
			searchContent = true
		case "--all", "-a":
			showAll = true
		case "--all-projects":
			allProjects = true
		case "--archived":
			archived = true
		case "--watch", "-w":
			watch = true
		case "--interval":
			v, err := s.value("--interval")
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return usagef("--interval must be a positive integer (秒): %q", v)
			}
			interval = time.Duration(n) * time.Second
		case "--active":
			// 既定が「done 以外」になったので no-op。互換のため受け付ける。
		case "--json":
			jsonOut = true
		case "--recent":
			// 最近完了 N 件。N は任意の数値引数 (省略時は既定)。次が正の整数なら N として取る。
			recent = true
			if v, ok := s.peek(); ok {
				if n, err := strconv.Atoi(v); err == nil && n > 0 {
					s.skip()
					recentN = n
				}
			}
		default:
			return usagef("unknown option: %s", a)
		}
	}
	if pos := s.rest(); len(pos) > 0 {
		return usagef("unexpected argument: %s", pos[0])
	}
	// --recent は「最近完了したアクティブタスク」専用なので --archived とは併用しない。
	if recent && archived {
		return usagef("--recent と --archived は併用できません")
	}

	// --recent は done を completed_at 降順で上位 N 件。--json と併用可 (status フィルタは無視し done)。
	if recent {
		rows, err := selectRecent(filterProjects, allProjects, recentN, searchQuery, searchContent)
		if err != nil {
			return err
		}
		if jsonOut {
			return writeTasksJSON(os.Stdout, rows, time.Now())
		}
		runRecentTable(os.Stdout, rows)
		return nil
	}

	// --json は機械可読出力。watch/色付けより優先し、フィルタは共通の selectTasks で適用する。
	if jsonOut {
		rows, _, _, err := selectTasks(filterStatus, filterProjects, showAll, allProjects, archived, searchQuery, searchContent)
		if err != nil {
			return err
		}
		return writeTasksJSON(os.Stdout, rows, time.Now())
	}

	// watch は端末のときだけループ表示。パイプ等では誤用防止のため 1 回出力して終える。
	if watch && isTTY(os.Stdout) {
		return watchList(filterStatus, filterProjects, showAll, allProjects, archived, searchQuery, searchContent, interval)
	}
	return runList(os.Stdout, filterStatus, filterProjects, showAll, allProjects, archived, searchQuery, searchContent)
}

// splitProjects は "--projects a,b, c" のカンマ区切りを trim して非空要素だけの集合にする。
func splitProjects(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// selectTasks はストアを読み、list のスコープ/フラグで絞り込んだ行を返す
// (テーブル出力と JSON 出力で共有)。effProjects は実効 project スコープ集合
// (空 = 横断、1 つ = 単一 project、複数 = 部分集合横断)、current は現在 project
// (フッター注記の判定に使う)。filterProjects に複数渡すと部分集合横断になる。
func selectTasks(filterStatus string, filterProjects []string, showAll, allProjects, archived bool, query string, searchContent bool) (rows []Task, effProjects []string, current string, err error) {
	current = currentProject()
	effProjects, _ = resolveListScope(filterProjects, allProjects, current)

	dir := storeDir()
	var tasks []Task
	if archived {
		// アーカイブ表示: <project>/archive/ だけを読む (通常の一覧とは排他の専用ビュー)。
		tasks, _, err = loadArchivedTasks(dir)
	} else {
		tasks, err = loadTasks(dir)
	}
	if err != nil {
		return nil, effProjects, current, fmt.Errorf("タスクディレクトリを読めません: %s (%w)", dir, err)
	}

	// 既定では done を隠す。--all 指定時、--status で絞った時、--archived (完全なアーカイブ閲覧) では隠さない。
	hideDone := !archived && !showAll && filterStatus == ""
	for _, t := range tasks {
		if t.Incomplete {
			continue // 作成途中 (title 未記入) の予約は一覧に出さない
		}
		if filterStatus != "" && t.Status != filterStatus {
			continue
		}
		if !matchProjects(t.Project, effProjects) {
			continue
		}
		if !matchQuery(t, query, searchContent) {
			continue
		}
		if hideDone && t.Status == "done" {
			continue
		}
		rows = append(rows, t)
	}
	return rows, effProjects, current, nil
}

// scopeLabel は実効 project スコープ集合を注記/エラー用の文字列にする
// (単一は "project: X"、複数は "projects: a, b")。空集合 (横断) では呼ばない前提。
func scopeLabel(effProjects []string) string {
	if len(effProjects) == 1 {
		return "project: " + effProjects[0]
	}
	return "projects: " + strings.Join(effProjects, ", ")
}

// runList は一覧を 1 回読み込み・絞り込み・w に描画する。watch はバッファに描かせて
// ちらつかない上書き再描画に使う。
func runList(w io.Writer, filterStatus string, filterProjects []string, showAll, allProjects, archived bool, query string, searchContent bool) error {
	rows, effProjects, current, err := selectTasks(filterStatus, filterProjects, showAll, allProjects, archived, query, searchContent)
	if err != nil {
		return err
	}
	dir := storeDir()
	// 既定 (明示なし) で現在 project に絞れたか。フッターの案内に使う。
	defaulted := len(filterProjects) == 0 && !allProjects && current != ""
	// 既定で横断にフォールバックしたか (git 外)。
	fellBack := len(filterProjects) == 0 && !allProjects && current == ""

	counts := map[string]int{}
	for _, t := range rows {
		counts[t.Status]++
	}

	if len(rows) == 0 {
		scopeNote := ""
		if archived {
			scopeNote = "アーカイブに"
		}
		if len(effProjects) > 0 {
			fmt.Fprintf(w, "%s該当タスクなし (%s, dir: %s)\n", scopeNote, scopeLabel(effProjects), dir)
			if defaulted {
				fmt.Fprintln(w, "横断するには --all-projects、別 project は --project <name>")
			}
		} else {
			fmt.Fprintf(w, "%s該当タスクなし (dir: %s)\n", scopeNote, dir)
		}
		return nil
	}

	c := newColors()
	now := time.Now()
	// 任意カラムは該当 status の行があるときだけ STATUS の右に出す:
	//   SESSION (in-progress: working/waiting/ended。hook 未導入だと "?")
	//   BLOCKED (blocked: 保留からの経過。長期放置は警告色。blocked_at 未記録だと "?")
	showSession := slices.ContainsFunc(rows, func(t Task) bool { return t.Status == "in-progress" })
	showBlocked := slices.ContainsFunc(rows, func(t Task) bool { return t.Status == "blocked" })

	headers := []string{"PROJECT", "ID", "STATUS"}
	if showSession {
		headers = append(headers, "SESSION")
	}
	if showBlocked {
		headers = append(headers, "BLOCKED")
	}
	headers = append(headers, "TITLE", "UPDATED")
	titleCol := len(headers) - 2 // TITLE は末尾 UPDATED の 1 つ手前

	tbl := newTable(headers...).truncatable(titleCol)
	for _, t := range rows {
		cells := []cell{
			{t.Project, c.dim},
			{t.ID, ""},
			{t.Status, c.status(t.Status)},
		}
		if showSession {
			cells = append(cells, sessionCell(t, c))
		}
		if showBlocked {
			cells = append(cells, blockedCell(t, c, now))
		}
		cells = append(cells, cell{blockedTitle(t), ""}, cell{displayDate(t.Updated), c.dim})
		tbl.add(cells...)
	}
	tbl.render(w, c)

	// サマリ
	var parts []string
	for _, s := range []string{"todo", "in-progress", "review", "blocked", "done"} {
		if n := counts[s]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s%s:%d%s", c.status(s), s, n, c.reset))
		}
	}
	fmt.Fprintf(w, "\n%stotal %d%s  %s\n", c.dim, len(rows), c.reset, strings.Join(parts, "  "))

	// スコープの注記: 既定で現在 project に絞った / 複数 project 指定 / git 外で横断にフォールバック。
	switch {
	case defaulted:
		fmt.Fprintf(w, "%s(project: %s のみ。横断は --all-projects)%s\n", c.dim, current, c.reset)
	case len(effProjects) > 1:
		fmt.Fprintf(w, "%s(%s のみ。全 project は --all-projects)%s\n", c.dim, scopeLabel(effProjects), c.reset)
	case fellBack:
		fmt.Fprintf(w, "%s(git リポジトリ外のため全 project を表示)%s\n", c.dim, c.reset)
	}
	// アーカイブ閲覧中であることを明示する (通常の一覧と取り違えないように)。
	if archived {
		fmt.Fprintf(w, "%s(アーカイブ表示。通常の一覧には出ません。戻すには unarchive <id>)%s\n", c.dim, c.reset)
	}
	// 検索でフィルタしているときはクエリと対象 (タイトル/本文) を注記する。
	if query != "" {
		target := "タイトル"
		if searchContent {
			target = "タイトル+本文"
		}
		fmt.Fprintf(w, "%s(検索: %q [%s])%s\n", c.dim, query, target, c.reset)
	}
	return nil
}

// watchList は interval ごとに runList を再描画する。Ctrl-C (SIGINT) で抜ける。
// 端末前提 (cmdList が TTY を確認済み)。別セッションのタスク更新を開きっぱなしで監視する用途。
//
// ちらつき対策: 画面全消去 (\033[2J) はせず、1 フレームをバッファに組み立ててから
// writeFrame で「カーソルを左上へ戻して上書き」する。全消去だと消去〜再描画の間に空白
// フレームが見えてチカチカするため。
func watchList(filterStatus string, filterProjects []string, showAll, allProjects, archived bool, query string, searchContent bool, interval time.Duration) error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sig)

	fmt.Print("\033[?25l")               // カーソルを隠す (ちらつき低減)
	defer fmt.Print("\033[?25h\033[?7h") // 抜けるとき: カーソルを戻し、行折り返しも戻す

	// draw はエラーで抜けない。読み取り失敗 (ストアが一時的に読めない等) もフレーム内に
	// 出して監視を続ける。watchList を err で抜けさせると main が os.Exit(1) し、defer の
	// カーソル/折り返し復帰が走らず端末が壊れたまま残るため (os.Exit は defer を実行しない)。
	draw := func() {
		c := newColors()
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "%sagent-tasks --watch  %s  間隔 %s  (Ctrl-C で終了)%s\n\n",
			c.dim, time.Now().Format("15:04:05"), interval, c.reset)
		if err := runList(&buf, filterStatus, filterProjects, showAll, allProjects, archived, query, searchContent); err != nil {
			fmt.Fprintf(&buf, "%serror: %v%s\n", c.block, err, c.reset)
		}
		writeFrame(buf.String())
	}

	draw()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-sig:
			fmt.Println()
			return nil
		case <-ticker.C:
			draw()
		}
	}
}

// writeFrame は overwriteFrame の結果を端末へ書く。
func writeFrame(frame string) {
	os.Stdout.WriteString(overwriteFrame(frame))
}

// overwriteFrame は frame を画面全消去せずに上書き描画するための制御シーケンス付き文字列に
// 変換する。カーソルを左上へ戻し、各行末で \033[K (行末までクリア) して前フレームの残りを消し、
// 最後に \033[J (カーソル以下をクリア) で行数が減ったときの古い行を消す。全消去 (\033[2J) を
// しないので消去〜再描画の間の空白フレームが出ず、ちらつかない。行折り返しを無効化 (\033[?7l)
// して、幅を超える行が次行に回り込んで桁ずれするのを防ぐ。
func overwriteFrame(frame string) string {
	var b strings.Builder
	b.WriteString("\033[?7l") // 自動折り返し OFF (長い行は端で切る)
	b.WriteString("\033[H")   // カーソルを左上へ (クリアはしない)
	for i, line := range strings.Split(strings.TrimRight(frame, "\n"), "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
		b.WriteString("\033[K") // この行の右側に残る前フレームの文字を消す
	}
	b.WriteString("\033[J") // カーソル以下 (行数が減った分) を消す
	return b.String()
}

// cmdDoctor は id 重複と id/ファイル名不一致を点検する。既定は全 project 横断
// (重複は project 単位の問題だが、まとめて点検したい)。--project で 1 project に絞れる。
// 問題が 1 件でもあれば silentExit{1} を返し、CI / prompt から検出できるようにする。
func cmdDoctor(args []string) error {
	var filterProject string
	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch a {
		case "--project":
			v, err := s.value("--project")
			if err != nil {
				return err
			}
			filterProject = v
		default:
			return usagef("unknown option: %s", a)
		}
	}
	if pos := s.rest(); len(pos) > 0 {
		return usagef("unexpected argument: %s", pos[0])
	}

	dir := storeDir()
	tasks, failures, err := loadTasksReport(dir)
	if err != nil {
		return fmt.Errorf("タスクディレクトリを読めません: %s (%w)", dir, err)
	}
	// アーカイブ済みも読み、重複 id 検査の対象に含める。番号は再利用しない方針なので、
	// アクティブと退避済みで id が被っていないか (戻すときに衝突しないか) を点検する。
	// 他の検査 (不一致 / 日時 / blocked / PR) は通常運用の対象であるアクティブのみに絞る。
	archivedTasks, _, _ := loadArchivedTasks(dir)
	if filterProject != "" {
		tasks = slices.DeleteFunc(tasks, func(t Task) bool { return t.Project != filterProject })
		archivedTasks = slices.DeleteFunc(archivedTasks, func(t Task) bool { return t.Project != filterProject })
		failures = slices.DeleteFunc(failures, func(f LoadFailure) bool {
			return filepath.Base(filepath.Dir(f.Path)) != filterProject
		})
	}

	// 重複検査だけはアクティブ + アーカイブを合わせて見る (active を先に並べて Paths 順を安定させる)。
	dups := findDuplicateIDs(append(slices.Clone(tasks), archivedTasks...))
	mismatches := findIDMismatches(tasks)
	tsIssues := findTimestampIssues(tasks)
	blockedIssues := findBlockedIssues(tasks)
	prIssues := findPRIssues(tasks)
	issueProbs := findIssueProblems(tasks)
	trackerProbs := findTrackerProblems(tasks)
	// 作成途中 (title 未記入) の予約ファイル。一覧からは隠れるので、doctor で可視化する
	// (放置された空予約の検出用。create 実行中のものが一時的に出ることはある)。
	incompletes := slices.DeleteFunc(slices.Clone(tasks), func(t Task) bool { return !t.Incomplete })

	c := newColors()
	scope := "全 project"
	if filterProject != "" {
		scope = fmt.Sprintf("project: %s", filterProject)
	}

	total := len(dups) + len(mismatches) + len(tsIssues) + len(blockedIssues) + len(prIssues) + len(issueProbs) + len(trackerProbs) + len(incompletes) + len(failures)
	if total == 0 {
		fmt.Printf("%s問題なし%s (%s, %d タスクを点検, dir: %s)\n", c.done, c.reset, scope, len(tasks), dir)
		return nil
	}

	if len(dups) > 0 {
		fmt.Printf("%s重複 id (同じ project に同一 id のファイルが複数):%s\n", c.bold, c.reset)
		for _, d := range dups {
			fmt.Printf("  %s%s/%s%s\n", c.block, d.Project, d.ID, c.reset)
			for _, p := range d.Paths {
				fmt.Printf("    - %s\n", p)
			}
		}
	}
	if len(mismatches) > 0 {
		if len(dups) > 0 {
			fmt.Println()
		}
		fmt.Printf("%sid とファイル名の不一致 (frontmatter id ≠ ファイル名先頭の連番):%s\n", c.bold, c.reset)
		for _, m := range mismatches {
			fmt.Printf("  %s%s%s  file=%s meta=%s  %s\n", c.block, m.Project, c.reset, m.FileID, m.MetaID, m.Path)
		}
	}
	if len(tsIssues) > 0 {
		if len(dups) > 0 || len(mismatches) > 0 {
			fmt.Println()
		}
		fmt.Printf("%s着手/完了日時の矛盾 (started_at / completed_at):%s\n", c.bold, c.reset)
		for _, ts := range tsIssues {
			fmt.Printf("  %s%s/%s%s  %s  %s\n", c.block, ts.Project, ts.ID, c.reset, ts.Detail, ts.Path)
		}
	}
	if len(blockedIssues) > 0 {
		if len(dups) > 0 || len(mismatches) > 0 || len(tsIssues) > 0 {
			fmt.Println()
		}
		fmt.Printf("%sblocked の記録/クリア漏れ (blocked_at / blocked_reason):%s\n", c.bold, c.reset)
		for _, bi := range blockedIssues {
			fmt.Printf("  %s%s/%s%s  %s  %s\n", c.block, bi.Project, bi.ID, c.reset, bi.Detail, bi.Path)
		}
	}
	if len(prIssues) > 0 {
		if len(dups) > 0 || len(mismatches) > 0 || len(tsIssues) > 0 || len(blockedIssues) > 0 {
			fmt.Println()
		}
		fmt.Printf("%sPR URL の形式 (prs:):%s\n", c.bold, c.reset)
		for _, pi := range prIssues {
			fmt.Printf("  %s%s/%s%s  %s  %s\n", c.block, pi.Project, pi.ID, c.reset, pi.Detail, pi.Path)
		}
	}
	if len(issueProbs) > 0 {
		if len(dups) > 0 || len(mismatches) > 0 || len(tsIssues) > 0 || len(blockedIssues) > 0 || len(prIssues) > 0 {
			fmt.Println()
		}
		fmt.Printf("%sissue URL の形式 (issue:):%s\n", c.bold, c.reset)
		for _, ip := range issueProbs {
			fmt.Printf("  %s%s/%s%s  %s  %s\n", c.block, ip.Project, ip.ID, c.reset, ip.Detail, ip.Path)
		}
	}
	if len(trackerProbs) > 0 {
		if len(dups) > 0 || len(mismatches) > 0 || len(tsIssues) > 0 || len(blockedIssues) > 0 || len(prIssues) > 0 || len(issueProbs) > 0 {
			fmt.Println()
		}
		fmt.Printf("%stracker URL の形式 (tracker:):%s\n", c.bold, c.reset)
		for _, tp := range trackerProbs {
			fmt.Printf("  %s%s/%s%s  %s  %s\n", c.block, tp.Project, tp.ID, c.reset, tp.Detail, tp.Path)
		}
	}
	if len(incompletes) > 0 {
		if len(dups) > 0 || len(mismatches) > 0 || len(tsIssues) > 0 || len(blockedIssues) > 0 || len(prIssues) > 0 || len(issueProbs) > 0 || len(trackerProbs) > 0 {
			fmt.Println()
		}
		fmt.Printf("%s作成途中/空の予約ファイル (title 未記入。一覧には出ない。放置なら削除を検討):%s\n", c.bold, c.reset)
		for _, t := range incompletes {
			fmt.Printf("  %s%s/%s%s  %s\n", c.block, t.Project, t.ID, c.reset, t.Path)
		}
	}
	if len(failures) > 0 {
		if len(dups) > 0 || len(mismatches) > 0 || len(tsIssues) > 0 || len(blockedIssues) > 0 || len(prIssues) > 0 || len(issueProbs) > 0 || len(trackerProbs) > 0 || len(incompletes) > 0 {
			fmt.Println()
		}
		fmt.Printf("%s読めなかったファイル (一覧から無言で落ちる):%s\n", c.bold, c.reset)
		for _, f := range failures {
			fmt.Printf("  %s%s%s  %v\n", c.block, f.Path, c.reset, f.Err)
		}
	}

	fmt.Printf("\n%s%d 件の問題%s (重複 %d / 不一致 %d / 日時矛盾 %d / blocked %d / PR %d / issue %d / tracker %d / 作成途中 %d / 読込失敗 %d)\n",
		c.block, total, c.reset, len(dups), len(mismatches), len(tsIssues), len(blockedIssues), len(prIssues), len(issueProbs), len(trackerProbs), len(incompletes), len(failures))
	return &silentExit{code: 1}
}

func cmdShow(args []string) error {
	// --json は機械可読出力。位置引数 (project/id) と分離してから解決する。
	jsonOut := false
	archived := false
	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch a {
		case "--json":
			jsonOut = true
		case "--archived":
			archived = true
		default:
			s.positional(a)
		}
	}
	project, id, err := resolveProjectID(s.rest())
	if err != nil {
		return err
	}
	subdir := ""
	if archived {
		subdir = archiveDirName // アーカイブ済みタスクを <project>/archive/ から開く
	}
	path, err := resolveTaskPathIn(project, id, subdir)
	if err != nil {
		return err
	}
	if jsonOut {
		t, err := parseTask(path)
		if err != nil {
			return err
		}
		t.Archived = archived // parseTask は印を付けないので、開いた場所 (--archived) を反映する
		return writeTaskJSON(os.Stdout, t, time.Now())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	c := newColors()
	fmt.Printf("%s# %s%s\n", c.dim, path, c.reset)
	os.Stdout.Write(data)
	// PR 一覧と、着手/完了が記録されていれば所要時間 (または経過) の要約を末尾に添える。
	if t, err := parseTask(path); err == nil {
		var footers []string
		if s := issueSummary(t, c); s != "" {
			footers = append(footers, s)
		}
		if s := prSummary(t, c); s != "" {
			footers = append(footers, s)
		}
		if s := trackerSummary(t, c); s != "" {
			footers = append(footers, s)
		}
		if s := timestampSummary(t, time.Now(), c); s != "" {
			footers = append(footers, s)
		}
		if len(footers) > 0 {
			if len(data) > 0 && data[len(data)-1] != '\n' {
				fmt.Println()
			}
			for _, f := range footers {
				fmt.Println(f)
			}
		}
	}
	return nil
}

// resolveProjectID は show / edit の引数を (project, id) に解決する。
//   - 1 引数: id とみなし、project は現在 project (cwd の git root basename) で補う。
//   - 2 引数: <project> <id> の明示指定 (別 project も可)。
//
// git 外などで現在 project を判定できないときは、明示指定を促すエラーにする
// (横断推測はしない)。
func resolveProjectID(args []string) (project, id string, err error) {
	switch len(args) {
	case 1:
		project = currentProject()
		if project == "" {
			return "", "", usagef("project を省略できるのは git リポジトリ内のみ。<project> <id> で指定してください")
		}
		return project, args[0], nil
	case 2:
		return args[0], args[1], nil
	default:
		return "", "", usagef("<id> (現在 project) か <project> <id> が必要")
	}
}

// resolveTaskPath は <project>/<id>-*.md (なければ <id>.md) を1件解決する。
// id は数値なら4桁ゼロ埋めに正規化してから照合するので `5` でも `0005` を指せる。
func resolveTaskPath(project, id string) (string, error) {
	return resolveTaskPathIn(project, id, "")
}

// resolveTaskPathIn は resolveTaskPath の subdir 指定版。subdir が空なら <project>/ 直下、
// 非空なら <project>/<subdir>/ (アーカイブ閲覧用の "archive" 等) から1件解決する。
func resolveTaskPathIn(project, id, subdir string) (string, error) {
	id = normalizeID(id)
	dir := filepath.Join(storeDir(), project)
	if subdir != "" {
		dir = filepath.Join(dir, subdir)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, id+"-*.md"))
	if len(matches) == 0 {
		matches, _ = filepath.Glob(filepath.Join(dir, id+".md"))
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("見つかりません: %s / %s", project, id)
	}
	return matches[0], nil
}

// cmdEdit はストア (引数なし) か1タスク (<id> / <project> <id>) をエディタで開く。
func cmdEdit(args []string) error {
	// フラグは無いが `--` (オプション終端) を解釈して位置引数だけを取り出す。
	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		s.positional(a)
	}
	target := storeDir()
	switch pos := s.rest(); len(pos) {
	case 0:
		// ストアのルートを開く
	default:
		project, id, err := resolveProjectID(pos)
		if err != nil {
			return err
		}
		path, err := resolveTaskPath(project, id)
		if err != nil {
			return err
		}
		target = path
	}

	argv := append(editorArgv(), target)
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return fmt.Errorf("エディタが見つかりません: %s (AGENT_TASKS_EDITOR / VISUAL / EDITOR で指定可)", argv[0])
	}
	cmd := exec.Command(bin, argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// editorArgv は使うエディタを AGENT_TASKS_EDITOR > VISUAL > EDITOR の順で決め、
// いずれも未設定なら code を使う。値は空白区切りで引数も解釈する (例: "code -w")。
func editorArgv() []string {
	for _, env := range []string{"AGENT_TASKS_EDITOR", "VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return strings.Fields(v)
		}
	}
	return []string{"code"}
}

// cmdSync はストア (storeDir) を git で add/commit/push してマシン間同期する。
// 既定は push まで。--no-push なら commit で止める。push 前に pull --rebase して
// 別マシンの更新を取り込む (git ライブラリは増やさず os/exec で呼ぶ)。
// cmdStatus はストアの未 sync 状態 (uncommitted / unpushed) を 1 行で表示する。
// 未同期があれば exit 1 (prompt / スクリプトで「sync が要るか」を終了コードで判別できる)。
func cmdStatus(args []string) error {
	if len(args) > 0 {
		return usagef("status は引数を取りません: %s", args[0])
	}
	dir := storeDir()
	st, err := loadSyncStatus(dir)
	if err != nil {
		return err
	}
	fmt.Println(formatSyncStatus(st, newColors()))
	if !st.Clean() {
		return &silentExit{code: 1}
	}
	return nil
}

// sync のロック/リトライのパラメータ。sync は network push を含むので、採番ロックより
// 長めに待つ・残骸判定も長め (途中で死んだ sync の置き土産を一定後に奪う)。
const (
	syncLockWait     = 30 * time.Second
	syncLockStale    = 2 * time.Minute
	syncPushAttempts = 3 // push 競合 (non-fast-forward) 時に pull --rebase して再試行する回数
)

func cmdSync(args []string) error {
	push := true
	var paths []string // 指定があれば scoped add (そのファイルだけ stage)
	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch a {
		case "--no-push":
			push = false
		case "--push":
			push = true // 既定だが明示用に受け付ける
		case "--path":
			v, err := s.value("--path")
			if err != nil {
				return err
			}
			paths = append(paths, v)
		default:
			s.positional(a)
		}
	}
	// 位置引数 [<project>] <id> はそのタスクファイルへ解決して scoped add の対象にする。
	if pos := s.rest(); len(pos) > 0 {
		project, id, err := resolveProjectID(pos)
		if err != nil {
			return err
		}
		p, err := resolveTaskPath(project, id)
		if err != nil {
			return err
		}
		paths = append(paths, p)
	}

	dir := storeDir()
	if out, err := git(dir, "rev-parse", "--is-inside-work-tree"); err != nil || out != "true" {
		return fmt.Errorf("%s は git リポジトリではありません (git init とリモート設定が必要)", dir)
	}

	// 並列セッションの sync を直列化する (index.lock 衝突や add -A の取りこぼしを防ぐ)。
	// ロックは git 管理下に置くと add -A で混入するため、ストア外 (OS temp) にパス由来の名前で置く。
	unlock, err := lockFile(syncLockPath(dir), syncLockWait, syncLockStale)
	if err != nil {
		return err
	}
	defer unlock()

	// scoped add: 指定があればそのファイルだけ stage する (他セッションの dirty を巻き込まない)。
	// 指定なしは従来どおり全体 (add -A) = 手動の全体同期。
	if len(paths) > 0 {
		if out, err := git(dir, append([]string{"add", "--"}, paths...)...); err != nil {
			return fmt.Errorf("git add に失敗しました:\n%s", out)
		}
	} else if _, err := git(dir, "add", "-A"); err != nil {
		return fmt.Errorf("git add に失敗しました: %w", err)
	}

	staged, err := git(dir, "diff", "--cached", "--name-status")
	if err != nil {
		return fmt.Errorf("git diff に失敗しました: %w", err)
	}
	if staged == "" {
		fmt.Println("コミットする変更はありません")
	} else {
		msg := syncCommitMessage(dir, staged)
		if out, err := git(dir, "commit", "-m", msg); err != nil {
			return fmt.Errorf("git commit に失敗しました:\n%s", out)
		}
		fmt.Printf("commit: %s\n", firstLine(msg))
	}

	if !push {
		return nil
	}
	if remotes, _ := git(dir, "remote"); remotes == "" {
		fmt.Println("リモート未設定のため push をスキップしました")
		return nil
	}

	branch, _ := git(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if _, err := git(dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err != nil {
		// upstream 未設定: 初回 push で追跡を設定する。
		if out, err := git(dir, "push", "-u", "origin", branch); err != nil {
			return fmt.Errorf("push に失敗しました:\n%s", out)
		}
		fmt.Printf("push 完了 (%s, upstream を設定)\n", branch)
		return nil
	}

	// 取り込んで push。push が競合 (non-fast-forward) したら取り込み直して数回リトライする。
	// pull --rebase --autostash で、他セッションの未 stage 変更があっても rebase できる。
	for attempt := 1; ; attempt++ {
		if out, err := git(dir, "pull", "--rebase", "--autostash"); err != nil {
			git(dir, "rebase", "--abort")
			return fmt.Errorf("pull --rebase に失敗しました。ストアで手動解決してください (%s):\n%s", dir, out)
		}
		out, err := git(dir, "push")
		if err == nil {
			fmt.Printf("push 完了 (%s)\n", branch)
			return nil
		}
		if attempt < syncPushAttempts && isNonFastForward(out) {
			continue // 別 push が先着。取り込み直して再試行。
		}
		return fmt.Errorf("push に失敗しました:\n%s", out)
	}
}

// syncLockPath はストア dir 専用の sync ロックのパスを返す。git 管理下に置くと add -A で
// 混入するため、ストア外 (OS temp) にストア絶対パスのハッシュで一意な名前を作る。
func syncLockPath(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	h := fnv.New64a()
	io.WriteString(h, abs)
	return filepath.Join(os.TempDir(), fmt.Sprintf("agent-tasks-sync-%x.lock", h.Sum64()))
}

// isNonFastForward は git push の出力が「先に他の push が入った (取り込みが必要)」かを判定する。
func isNonFastForward(out string) bool {
	for _, s := range []string{"non-fast-forward", "fetch first", "[rejected]", "behind"} {
		if strings.Contains(out, s) {
			return true
		}
	}
	return false
}

// git は storeDir 等の dir で git を実行し、出力 (stdout+stderr) を trim して返す。
func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// syncCommitMessage は git diff --cached --name-status からコミットメッセージを組む。
// 例: "tasks: agent-tasks/0005 (in-progress)"、複数なら本文に列挙する。
func syncCommitMessage(dir, nameStatus string) string {
	var entries []string
	for _, line := range strings.Split(nameStatus, "\n") {
		code, path, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		project, file, ok := strings.Cut(path, "/")
		if !ok || !strings.HasSuffix(file, ".md") {
			continue // README.md などタスク以外は列挙しない
		}
		id := leadingID(file)
		if id == "" {
			continue
		}
		status := "updated"
		if strings.HasPrefix(code, "D") {
			status = "removed"
		} else if t, err := parseTask(filepath.Join(dir, path)); err == nil && t.Status != "" {
			status = t.Status
		}
		entries = append(entries, fmt.Sprintf("%s/%s (%s)", project, id, status))
	}
	switch len(entries) {
	case 0:
		return "tasks: sync store"
	case 1:
		return "tasks: " + entries[0]
	default:
		return fmt.Sprintf("tasks: update %d tasks\n\n- %s", len(entries), strings.Join(entries, "\n- "))
	}
}

// leadingID はファイル名先頭の数字列 (タスク ID) を返す ("0005-foo.md" -> "0005")。
func leadingID(name string) string {
	i := 0
	for i < len(name) && name[i] >= '0' && name[i] <= '9' {
		i++
	}
	return name[:i]
}

// firstLine は文字列の最初の行を返す (コミットメッセージの件名表示用)。
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
