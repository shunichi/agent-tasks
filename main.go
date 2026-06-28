// Command agent-tasks は ~/agent-tasks-store を横断してエージェント開発タスクの
// 進捗を表示する CLI。エージェント (claude / codex / ...) を起動せずに進捗を見る。
//
// データの場所は AGENT_TASKS_STORE で上書きできる (既定: ~/agent-tasks-store)。
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// usageError は使い方の誤り (未知オプション/引数不足など) を表す。
// main がこれを受け取ると、メッセージに続けて usage を表示し exit 2 する。
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error {
	return &usageError{msg: fmt.Sprintf(format, a...)}
}

func main() {
	args := os.Args[1:]
	cmd := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}
	// -h / --help はサブコマンド扱いされず引数として流れてくるので先に拾う
	// (例: `agent-tasks -h`, `agent-tasks list -h`)。
	if cmd == "help" || slices.Contains(args, "-h") || slices.Contains(args, "--help") {
		usage(os.Stdout)
		return
	}

	var err error
	switch cmd {
	case "list":
		err = cmdList(args)
	case "show":
		err = cmdShow(args)
	case "where":
		fmt.Println(storeDir())
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
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

func usage(w *os.File) {
	fmt.Fprint(w, `agent-tasks — エージェント開発タスクの横断ビュー

USAGE:
  agent-tasks [list]                 未完了タスクを一覧 (既定。done は非表示)
  agent-tasks --all | -a             done も含めて全件表示
  agent-tasks --status <status>      status で絞り込み (todo/in-progress/blocked/review/done)
  agent-tasks --project <name>       project で絞り込み
  agent-tasks show <project> <id>    1タスクの全文を表示
  agent-tasks where                  データディレクトリのパスを表示
  agent-tasks help | -h | --help     このヘルプ

ENV:
  AGENT_TASKS_STORE    タスクデータの場所 (既定: ~/agent-tasks-store)
`)
}

func cmdList(args []string) error {
	var filterStatus, filterProject string
	showAll := false
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
		case "--active":
			// 既定が「done 以外」になったので no-op。互換のため受け付ける。
		default:
			return usagef("unknown option: %s", args[i])
		}
	}

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
		if filterProject != "" && t.Project != filterProject {
			continue
		}
		if hideDone && t.Status == "done" {
			continue
		}
		rows = append(rows, t)
		counts[t.Status]++
	}

	if len(rows) == 0 {
		fmt.Printf("該当タスクなし (dir: %s)\n", dir)
		return nil
	}

	c := newColors()
	tbl := newTable("PROJECT", "ID", "STATUS", "TITLE", "UPDATED")
	for _, t := range rows {
		tbl.add(
			cell{t.Project, c.dim},
			cell{t.ID, ""},
			cell{t.Status, c.status(t.Status)},
			cell{t.Title, ""},
			cell{t.Updated, c.dim},
		)
	}
	tbl.render(os.Stdout, c)

	// サマリ
	var parts []string
	for _, s := range []string{"todo", "in-progress", "review", "blocked", "done"} {
		if n := counts[s]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s%s:%d%s", c.status(s), s, n, c.reset))
		}
	}
	fmt.Printf("\n%stotal %d%s  %s\n", c.dim, len(rows), c.reset, strings.Join(parts, "  "))
	return nil
}

func cmdShow(args []string) error {
	if len(args) < 2 {
		return usagef("show は <project> と <id> が必要")
	}
	project, id := args[0], args[1]
	projDir := filepath.Join(storeDir(), project)

	matches, _ := filepath.Glob(filepath.Join(projDir, id+"-*.md"))
	if len(matches) == 0 {
		matches, _ = filepath.Glob(filepath.Join(projDir, id+".md"))
	}
	if len(matches) == 0 {
		return fmt.Errorf("見つかりません: %s / %s", project, id)
	}
	path := matches[0]
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	c := newColors()
	fmt.Printf("%s# %s%s\n", c.dim, path, c.reset)
	os.Stdout.Write(data)
	return nil
}
