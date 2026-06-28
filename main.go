// Command agent-tasks は ~/agent-tasks-store を横断してエージェント開発タスクの
// 進捗を表示する CLI。エージェント (claude / codex / ...) を起動せずに進捗を見る。
//
// データの場所は AGENT_TASKS_STORE で上書きできる (既定: ~/agent-tasks-store)。
package main

import (
	"bytes"
	"errors"
	"fmt"
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
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
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

	switch cmd {
	case "list":
		return cmdList(args)
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
	case "scaffold-worktree":
		return cmdScaffoldWorktree(args)
	case "doctor":
		return cmdDoctor(args)
	case "session-hook":
		return cmdSessionHook(args)
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

func usage(w *os.File) {
	fmt.Fprint(w, `agent-tasks — エージェント開発タスクの横断ビュー

USAGE:
  agent-tasks [list]                 現在 project の未完了タスクを一覧 (既定。done は非表示)
  agent-tasks --all-projects         全 project を横断して一覧
  agent-tasks --all | -a             done も含めて表示
  agent-tasks --status <status>      status で絞り込み (todo/in-progress/blocked/review/done)
  agent-tasks --project <name>       project を指定 (別 project も可)
  agent-tasks --watch | -w           一覧を一定間隔で自動更新表示 (--interval <秒>、既定 2。Ctrl-C で終了)
  agent-tasks show [<project>] <id>  1タスクの全文を表示 (project 省略時は現在 project)
  agent-tasks edit [[<project>] <id>] ストア (引数なし) か1タスクをエディタで開く
  agent-tasks status                 ストアの未同期状態 (未コミット/未push) を1行表示
                                     (未同期があれば exit 1。sync が要るかの確認に使う)
  agent-tasks sync [--no-push]       ストアを add/commit/push して同期 (--no-push で commit まで)
  agent-tasks worktree-init <dir>    worktree 作成後フック: .worktreeinclude をコピーし
                                     .worktree-post-create を実行 (start/spawn が呼ぶ。--force で再実行)
  agent-tasks scaffold-worktree [stack]  worktree 設定 (.worktreeinclude/.worktree-post-create) を
                                     プロジェクトに展開 (stack 省略で自動検出。--list/--dir/--force)
  agent-tasks doctor [--project <name>] id 重複と id/ファイル名の不一致を点検 (既定は全 project 横断。
                                     問題があれば exit 1。CI / 着手前チェックに使う)
  agent-tasks session-hook [--print-config]  Claude Code の hook から呼ぶ。stdin の JSON を読んで
                                     セッションの working/waiting を記録する (--print-config で設定例を出力)
  agent-tasks where                  データディレクトリのパスを表示
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
	var filterStatus, filterProject string
	showAll := false
	allProjects := false
	watch := false
	interval := 2 * time.Second
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--status":
			if i+1 >= len(args) {
				return usagef("--status requires a value")
			}
			i++
			filterStatus = args[i]
		case "--project":
			if i+1 >= len(args) {
				return usagef("--project requires a value")
			}
			i++
			filterProject = args[i]
		case "--all", "-a":
			showAll = true
		case "--all-projects":
			allProjects = true
		case "--watch", "-w":
			watch = true
		case "--interval":
			if i+1 >= len(args) {
				return usagef("--interval requires a value (秒)")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				return usagef("--interval must be a positive integer (秒): %q", args[i])
			}
			interval = time.Duration(n) * time.Second
		case "--active":
			// 既定が「done 以外」になったので no-op。互換のため受け付ける。
		default:
			return usagef("unknown option: %s", args[i])
		}
	}

	// watch は端末のときだけループ表示。パイプ等では誤用防止のため 1 回出力して終える。
	if watch && isTTY(os.Stdout) {
		return watchList(filterStatus, filterProject, showAll, allProjects, interval)
	}
	return runList(os.Stdout, filterStatus, filterProject, showAll, allProjects)
}

// runList は一覧を 1 回読み込み・絞り込み・w に描画する。watch はバッファに描かせて
// ちらつかない上書き再描画に使う。
func runList(w io.Writer, filterStatus, filterProject string, showAll, allProjects bool) error {
	// project スコープを決める。既定は現在 project (cwd の git root basename) のみ。
	// --project 明示 / --all-projects / git 外 では横断 (effProject == "")。
	current := currentProject()
	effProject, _ := resolveListScope(filterProject, allProjects, current)
	// 既定 (明示なし) で現在 project に絞れたか。フッターの案内に使う。
	defaulted := filterProject == "" && !allProjects && current != ""
	// 既定で横断にフォールバックしたか (git 外)。
	fellBack := filterProject == "" && !allProjects && current == ""

	dir := storeDir()
	tasks, err := loadTasks(dir)
	if err != nil {
		return fmt.Errorf("タスクディレクトリを読めません: %s (%w)", dir, err)
	}

	// 既定では done を隠す。--all 指定時、または --status で明示的に絞り込んだ時は隠さない。
	hideDone := !showAll && filterStatus == ""

	var rows []Task
	counts := map[string]int{}
	for _, t := range tasks {
		if filterStatus != "" && t.Status != filterStatus {
			continue
		}
		if effProject != "" && t.Project != effProject {
			continue
		}
		if hideDone && t.Status == "done" {
			continue
		}
		rows = append(rows, t)
		counts[t.Status]++
	}

	if len(rows) == 0 {
		if effProject != "" {
			fmt.Fprintf(w, "該当タスクなし (project: %s, dir: %s)\n", effProject, dir)
			if defaulted {
				fmt.Fprintln(w, "横断するには --all-projects、別 project は --project <name>")
			}
		} else {
			fmt.Fprintf(w, "該当タスクなし (dir: %s)\n", dir)
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

	tbl := newTable(headers...)
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

	// スコープの注記: 既定で現在 project に絞った / git 外で横断にフォールバックした旨を伝える。
	switch {
	case defaulted:
		fmt.Fprintf(w, "%s(project: %s のみ。横断は --all-projects)%s\n", c.dim, current, c.reset)
	case fellBack:
		fmt.Fprintf(w, "%s(git リポジトリ外のため全 project を表示)%s\n", c.dim, c.reset)
	}
	return nil
}

// watchList は interval ごとに runList を再描画する。Ctrl-C (SIGINT) で抜ける。
// 端末前提 (cmdList が TTY を確認済み)。別セッションのタスク更新を開きっぱなしで監視する用途。
//
// ちらつき対策: 画面全消去 (\033[2J) はせず、1 フレームをバッファに組み立ててから
// writeFrame で「カーソルを左上へ戻して上書き」する。全消去だと消去〜再描画の間に空白
// フレームが見えてチカチカするため。
func watchList(filterStatus, filterProject string, showAll, allProjects bool, interval time.Duration) error {
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
		if err := runList(&buf, filterStatus, filterProject, showAll, allProjects); err != nil {
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
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project":
			if i+1 >= len(args) {
				return usagef("--project requires a value")
			}
			i++
			filterProject = args[i]
		default:
			return usagef("unknown option: %s", args[i])
		}
	}

	dir := storeDir()
	tasks, failures, err := loadTasksReport(dir)
	if err != nil {
		return fmt.Errorf("タスクディレクトリを読めません: %s (%w)", dir, err)
	}
	if filterProject != "" {
		tasks = slices.DeleteFunc(tasks, func(t Task) bool { return t.Project != filterProject })
		failures = slices.DeleteFunc(failures, func(f LoadFailure) bool {
			return filepath.Base(filepath.Dir(f.Path)) != filterProject
		})
	}

	dups := findDuplicateIDs(tasks)
	mismatches := findIDMismatches(tasks)
	tsIssues := findTimestampIssues(tasks)
	blockedIssues := findBlockedIssues(tasks)

	c := newColors()
	scope := "全 project"
	if filterProject != "" {
		scope = fmt.Sprintf("project: %s", filterProject)
	}

	total := len(dups) + len(mismatches) + len(tsIssues) + len(blockedIssues) + len(failures)
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
	if len(failures) > 0 {
		if len(dups) > 0 || len(mismatches) > 0 || len(tsIssues) > 0 || len(blockedIssues) > 0 {
			fmt.Println()
		}
		fmt.Printf("%s読めなかったファイル (一覧から無言で落ちる):%s\n", c.bold, c.reset)
		for _, f := range failures {
			fmt.Printf("  %s%s%s  %v\n", c.block, f.Path, c.reset, f.Err)
		}
	}

	fmt.Printf("\n%s%d 件の問題%s (重複 %d / 不一致 %d / 日時矛盾 %d / blocked %d / 読込失敗 %d)\n",
		c.block, total, c.reset, len(dups), len(mismatches), len(tsIssues), len(blockedIssues), len(failures))
	return &silentExit{code: 1}
}

func cmdShow(args []string) error {
	project, id, err := resolveProjectID(args)
	if err != nil {
		return err
	}
	path, err := resolveTaskPath(project, id)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	c := newColors()
	fmt.Printf("%s# %s%s\n", c.dim, path, c.reset)
	os.Stdout.Write(data)
	// 着手/完了が記録されていれば、所要時間 (または経過) の要約を末尾に添える。
	if t, err := parseTask(path); err == nil {
		if footer := timestampSummary(t, time.Now(), c); footer != "" {
			if len(data) > 0 && data[len(data)-1] != '\n' {
				fmt.Println()
			}
			fmt.Println(footer)
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
	id = normalizeID(id)
	projDir := filepath.Join(storeDir(), project)
	matches, _ := filepath.Glob(filepath.Join(projDir, id+"-*.md"))
	if len(matches) == 0 {
		matches, _ = filepath.Glob(filepath.Join(projDir, id+".md"))
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("見つかりません: %s / %s", project, id)
	}
	return matches[0], nil
}

// cmdEdit はストア (引数なし) か1タスク (<id> / <project> <id>) をエディタで開く。
func cmdEdit(args []string) error {
	target := storeDir()
	switch len(args) {
	case 0:
		// ストアのルートを開く
	default:
		project, id, err := resolveProjectID(args)
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
// 別マシンの更新を取り込む (依存ゼロ方針のため git は os/exec で呼ぶ)。
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

func cmdSync(args []string) error {
	push := true
	for _, a := range args {
		switch a {
		case "--no-push":
			push = false
		case "--push":
			push = true // 既定だが明示用に受け付ける
		default:
			return usagef("unknown option: %s", a)
		}
	}

	dir := storeDir()
	if out, err := git(dir, "rev-parse", "--is-inside-work-tree"); err != nil || out != "true" {
		return fmt.Errorf("%s は git リポジトリではありません (git init とリモート設定が必要)", dir)
	}

	if _, err := git(dir, "add", "-A"); err != nil {
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

	// 別マシンの更新を取り込んでから push する。コンフリクト時は rebase を畳んで通知。
	if out, err := git(dir, "pull", "--rebase"); err != nil {
		git(dir, "rebase", "--abort")
		return fmt.Errorf("pull --rebase に失敗しました。ストアで手動解決してください (%s):\n%s", dir, out)
	}
	if out, err := git(dir, "push"); err != nil {
		return fmt.Errorf("push に失敗しました:\n%s", out)
	}
	fmt.Printf("push 完了 (%s)\n", branch)
	return nil
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
