package main

import (
	"bufio"
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Task は 1 つのタスクファイル (~/agent-tasks-store/<project>/<NNNN>-<slug>.md) を表す。
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

	// 着手・完了の日時 (ISO8601)。created/updated と違い「いつ始めて終わったか」を
	// 正確に追う/所要期間 (リードタイム) を出すための専用フィールド。
	StartedAt   string // status を in-progress にした日時。初回着手を保持 (再 start で上書きしない)
	CompletedAt string // status を done にした日時。done→in-progress で再オープンするとクリア

	// blocked のときだけ埋まる (start/done で blocked を抜けるとクリアされる)。
	BlockedAt     string // 保留にした日時 (ISO8601。経過算出の基点。updated とは別)
	BlockedReason string // 保留理由 (一覧表示用の構造化フィールド。履歴は進捗ログ)

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

// currentProject は cwd の git リポジトリから project キーを返す。git 外なら空文字。
// list の既定フィルタ (現在 project のみ表示) のための seam。
//
// project キーは「メイン作業ツリー root の basename」。show-toplevel ではなく
// git-common-dir の親を使うのは、リンク worktree (start が作る ../<project>--<NNNN>)
// の中で実行されても、その basename (<project>--<NNNN>) ではなくメイン repo 名
// (<project>) を返すため。タスク登録時に記録される project キーと一致させる。
func currentProject() string {
	out, err := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	commonDir := strings.TrimSpace(string(out))
	if commonDir == "" {
		return ""
	}
	if !filepath.IsAbs(commonDir) {
		if wd, err := os.Getwd(); err == nil {
			commonDir = filepath.Join(wd, commonDir)
		}
	}
	// commonDir はメイン作業ツリーの .git を指す。その親 = メイン repo root。
	return filepath.Base(filepath.Dir(commonDir))
}

// resolveListScope は list の project フィルタ対象と横断フラグを決める。
// 優先順位: --project 明示 > --all-projects > 既定 (現在 project)。
// 現在 project が空 (git 外で判定不能) のときは横断にフォールバックする。
// project が "" を返したときは横断 (全 project) を意味する。
func resolveListScope(filterProject string, allProjects bool, current string) (project string, cross bool) {
	switch {
	case filterProject != "":
		return filterProject, false // 別 project の明示指定も許す
	case allProjects:
		return "", true
	case current == "":
		return "", true // git 外: 判定不能なので横断
	default:
		return current, false // 既定: 現在 project のみ
	}
}

// normalizeID は入力 ID を照合用に正規化する。数値なら4桁ゼロ埋めにそろえ
// (5 -> 0005, 05 -> 0005, 12345 -> 12345)、数値でなければそのまま返す。
// 保存される ID 自体は4桁ゼロ埋めのままで、入力だけ緩く受けるための関数。
// start/done/block を CLI 化する場合も ID 解決でこれを共有する想定。
func normalizeID(id string) string {
	if n, err := strconv.Atoi(id); err == nil && n >= 0 {
		return fmt.Sprintf("%04d", n)
	}
	return id
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

// Duplicate は同一 (project, id) を共有する複数ファイルを表す検出結果。
// 並行 create の採番競合 (TOCTOU) で同じ id のファイルが複数できた状態を拾う。
type Duplicate struct {
	Project string
	ID      string
	Paths   []string // 該当ファイルの絶対パス (loadTasks 由来なので project/id 昇順)
}

// Mismatch は frontmatter の id とファイル名先頭の連番 (NNNN) がずれているタスク。
// 例: ファイル名 0016-foo.md だが frontmatter が id: "0015"。
type Mismatch struct {
	Project string
	FileID  string // ファイル名先頭の連番
	MetaID  string // frontmatter の id (loadTasks の補完後)
	Path    string
}

// findDuplicateIDs は loadTasks の結果を (project, id) で集計し、同一キーが
// 2 件以上あるものを返す。tasks は project/id 昇順前提なので結果も昇順になる。
func findDuplicateIDs(tasks []Task) []Duplicate {
	type key struct{ project, id string }
	groups := map[key][]string{}
	var order []key
	for _, t := range tasks {
		k := key{t.Project, t.ID}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], t.Path)
	}
	var dups []Duplicate
	for _, k := range order {
		if paths := groups[k]; len(paths) > 1 {
			dups = append(dups, Duplicate{Project: k.project, ID: k.id, Paths: paths})
		}
	}
	return dups
}

// findIDMismatches は frontmatter の id とファイル名先頭の連番が一致しないタスクを返す。
// ファイル名に連番が無い (leadingID が空) ファイルは対象外。
func findIDMismatches(tasks []Task) []Mismatch {
	var out []Mismatch
	for _, t := range tasks {
		fileID := leadingID(filepath.Base(t.Path))
		if fileID == "" {
			continue
		}
		if t.ID != fileID {
			out = append(out, Mismatch{Project: t.Project, FileID: fileID, MetaID: t.ID, Path: t.Path})
		}
	}
	return out
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
		case "started_at":
			t.StartedAt = val
		case "completed_at":
			t.CompletedAt = val
		case "blocked_at":
			t.BlockedAt = val
		case "blocked_reason":
			t.BlockedReason = val
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
