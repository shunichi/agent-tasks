// Command agent-tasks は ~/agent-tasks-store を横断してエージェント開発タスクの
// 進捗を表示する CLI。エージェント (claude / codex / ...) を起動せずに進捗を見る。
//
// データの場所は AGENT_TASKS_STORE で上書きできる (既定: ~/agent-tasks-store)。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	args := os.Args[1:]
	cmd := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}

	var err error
	switch cmd {
	case "list":
		err = cmdList(args)
	case "show":
		err = cmdShow(args)
	case "where":
		fmt.Println(storeDir())
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage(w *os.File) {
	fmt.Fprint(w, `agent-tasks — エージェント開発タスクの横断ビュー

USAGE:
  agent-tasks [list]                 全タスクを一覧 (既定)
  agent-tasks --status <status>      status で絞り込み (todo/in-progress/blocked/review/done)
  agent-tasks --project <name>       project で絞り込み
  agent-tasks --active               未完了のみ (done 以外)
  agent-tasks show <project> <id>    1タスクの全文を表示
  agent-tasks where                  データディレクトリのパスを表示
  agent-tasks -h | --help            このヘルプ

ENV:
  AGENT_TASKS_STORE    タスクデータの場所 (既定: ~/agent-tasks-store)
`)
}

func cmdList(args []string) error {
	var filterStatus, filterProject string
	activeOnly := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--status":
			if i+1 >= len(args) {
				return fmt.Errorf("--status requires a value")
			}
			i++
			filterStatus = args[i]
		case "--project":
			if i+1 >= len(args) {
				return fmt.Errorf("--project requires a value")
			}
			i++
			filterProject = args[i]
		case "--active":
			activeOnly = true
		default:
			return fmt.Errorf("unknown option: %s", args[i])
		}
	}

	dir := storeDir()
	tasks, err := loadTasks(dir)
	if err != nil {
		return fmt.Errorf("タスクディレクトリを読めません: %s (%w)", dir, err)
	}

	var rows []Task
	counts := map[string]int{}
	for _, t := range tasks {
		if filterStatus != "" && t.Status != filterStatus {
			continue
		}
		if filterProject != "" && t.Project != filterProject {
			continue
		}
		if activeOnly && t.Status == "done" {
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
		return fmt.Errorf("usage: agent-tasks show <project> <id>")
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
