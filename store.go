package main

import (
	"bufio"
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Task は 1 つのタスクファイル (~/agent-tasks-store/<project>/<NNN>-<slug>.md) を表す。
type Task struct {
	ID       string
	Project  string
	Title    string
	Status   string
	Agent    string
	Session  string
	Branch   string
	Worktree string
	Created  string
	Updated  string

	Path string // ファイルの絶対パス
}

// storeDir はタスクデータの置き場を返す。AGENT_TASKS_STORE、未設定なら ~/agent-tasks-store。
func storeDir() string {
	if v := os.Getenv("AGENT_TASKS_STORE"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "agent-tasks-store"
	}
	return filepath.Join(home, "agent-tasks-store")
}

// loadTasks は store 配下の <project>/*.md を全て読み、project / id 順で返す。
func loadTasks(dir string) ([]Task, error) {
	var tasks []Task
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, projEntry := range entries {
		if !projEntry.IsDir() {
			continue // トップレベルの README.md などは無視
		}
		project := projEntry.Name()
		projDir := filepath.Join(dir, project)
		files, err := os.ReadDir(projDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			path := filepath.Join(projDir, f.Name())
			t, err := parseTask(path)
			if err != nil {
				continue
			}
			if t.Project == "" {
				t.Project = project
			}
			if t.ID == "" {
				t.ID = strings.TrimSuffix(f.Name(), ".md")
			}
			if t.Status == "" {
				t.Status = "todo"
			}
			if t.Title == "" {
				t.Title = "(no title)"
			}
			tasks = append(tasks, t)
		}
	}
	slices.SortFunc(tasks, func(a, b Task) int {
		return cmp.Or(
			cmp.Compare(a.Project, b.Project),
			cmp.Compare(a.ID, b.ID),
		)
	})
	return tasks, nil
}

// parseTask は Markdown ファイル先頭の YAML frontmatter (フラットな key: value) を読む。
func parseTask(path string) (Task, error) {
	t := Task{Path: path}
	f, err := os.Open(path)
	if err != nil {
		return t, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	first := true
	inFrontmatter := false
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = true
				continue
			}
			break // frontmatter なし
		}
		if !inFrontmatter {
			break
		}
		if strings.TrimSpace(line) == "---" {
			break // frontmatter 終端
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = unquote(strings.TrimSpace(val))
		switch key {
		case "id":
			t.ID = val
		case "project":
			t.Project = val
		case "title":
			t.Title = val
		case "status":
			t.Status = val
		case "agent":
			t.Agent = val
		case "session":
			t.Session = val
		case "branch":
			t.Branch = val
		case "worktree":
			t.Worktree = val
		case "created":
			t.Created = val
		case "updated":
			t.Updated = val
		}
	}
	return t, sc.Err()
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
