package main

import (
	"bufio"
	"cmp"
	"fmt"
	"os"
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

	// このタスクに紐づく PR の URL。1 タスクで複数 PR (分割 PR / 追従修正) になり得るので
	// リストで持つ。frontmatter では prs: の YAML ブロックリストで表す。session (着手した
	// エージェントのセッション URL) とは別フィールドに分け、PR はここに集約する。
	PRs []string

	// このタスクを共有するために起票した GitHub issue の URL (任意)。ストアは private で
	// 人に見せづらいので、共有したいタスクだけ `agent-tasks issue` で起票し、その URL をここに
	// 記録する (store → issue の一方向)。1 タスク 1 issue (共有用途には単一で足りる)。
	Issue string

	// このタスクに関連する外部 issue tracker / 課題管理の URL (任意、複数可)。prs: (PR 専用) とは
	// 別枠で、任意のホストの関連 URL を汎用に保持する (特定サービス非依存)。frontmatter では
	// tracker: の YAML ブロックリストで表す。
	Tracker []string

	// 着手・完了の日時 (ISO8601)。created/updated と違い「いつ始めて終わったか」を
	// 正確に追う/所要期間 (リードタイム) を出すための専用フィールド。
	StartedAt   string // status を in-progress にした日時。初回着手を保持 (再 start で上書きしない)
	CompletedAt string // status を done にした日時。done→in-progress で再オープンするとクリア

	// blocked のときだけ埋まる (start/done で blocked を抜けるとクリアされる)。
	BlockedAt     string // 保留にした日時 (ISO8601。経過算出の基点。updated とは別)
	BlockedReason string // 保留理由 (一覧表示用の構造化フィールド。履歴は進捗ログ)

	// Archived はこのタスクが <project>/archive/ に退避されているか。通常の走査
	// (loadTasksReport) はアーカイブを読まないので、ここを読むのは loadArchivedTasks 経由
	// (list --archived / show --archived / doctor の重複検査) のときだけ true になる。
	Archived bool

	// Incomplete は作成途中の中途半端なファイル (frontmatter に title が無い) を示す。
	// alloc-id が採番時に作る空の予約ファイル、または create の書き込み途中で観測した状態。
	// 一覧表示 (list/json/recent/tui) からは除外するが、doctor では隠さない (未記入予約の検出用)。
	Incomplete bool

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
	// git root の解決は mainRepoOf に集約 (worktree.go)。現在 project はその root の basename。
	// git 外などで解決できないときは "" を返し、呼び出し側が横断にフォールバックする契約。
	root, err := mainRepoOf(".")
	if err != nil {
		return ""
	}
	return filepath.Base(root)
}

// resolveListScope は list の project フィルタ対象 (集合) と横断フラグを決める。
// 優先順位: --project 明示 (1 つ以上) > --all-projects > 既定 (現在 project)。
// 現在 project が空 (git 外で判定不能) のときは横断にフォールバックする。
// 返す projects が空 (len 0) のときは横断 (全 project)、1 つ以上ならその集合だけを表示する。
// filterProjects に複数 project を渡せば、その部分集合だけを横断表示する (--all-projects の部分版)。
func resolveListScope(filterProjects []string, allProjects bool, current string) (projects []string, cross bool) {
	switch {
	case len(filterProjects) > 0:
		return filterProjects, false // 明示指定 (単一/複数とも)。別 project も許す
	case allProjects:
		return nil, true
	case current == "":
		return nil, true // git 外: 判定不能なので横断
	default:
		return []string{current}, false // 既定: 現在 project のみ
	}
}

// matchProjects は project がスコープ集合 effProjects に含まれるかを返す。
// effProjects が空 (nil/len 0) は横断 (全 project) を意味し、常に true。
func matchProjects(project string, effProjects []string) bool {
	return len(effProjects) == 0 || slices.Contains(effProjects, project)
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

// archiveDirName は project ディレクトリ内でアーカイブ済みタスクを退避するサブディレクトリ名。
// 通常の走査 (loadTasksReport / loadTasks) はここを読まないので、退避したタスクは
// list / -a / doctor の通常表示から外れる。明示的に見たいときだけ loadArchivedTasks で読む。
const archiveDirName = "archive"

// loadTasks は store 配下の <project>/*.md (アーカイブ除く) を全て読み、project / id 順で返す。
func loadTasks(dir string) ([]Task, error) {
	tasks, _, err := loadTasksReport(dir)
	return tasks, err
}

// LoadFailure は走査中に読めなかった (parse 失敗 / ディレクトリ読み取り失敗) ファイル。
// loadTasks はこれらを黙って一覧から落とすため、doctor が「無言で消えたタスク」を
// 可視化するのに使う (長大な1行・権限などで起きうる)。
type LoadFailure struct {
	Path string
	Err  error
}

// loadTasksReport は loadTasks 本体。読めたタスク (アクティブのみ) に加え、読めなかった
// ファイルも返す。アーカイブ (<project>/archive/) は対象外 (loadArchivedTasks で読む)。
func loadTasksReport(dir string) ([]Task, []LoadFailure, error) {
	return collectTasks(dir, true, false)
}

// loadArchivedTasks はアーカイブ済みタスク (<project>/archive/*.md) だけを読む。
// list --archived / show --archived / doctor の重複検査 (アクティブと番号が被らないか) で使う。
func loadArchivedTasks(dir string) ([]Task, []LoadFailure, error) {
	return collectTasks(dir, false, true)
}

// collectTasks は store 配下の各 project から、active (<project>/*.md) と
// archive (<project>/archive/*.md) を選択的に読み、project / id 順 (同 id は active 優先) で返す。
func collectTasks(dir string, includeActive, includeArchive bool) ([]Task, []LoadFailure, error) {
	var tasks []Task
	var failures []LoadFailure
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	for _, projEntry := range entries {
		if !projEntry.IsDir() {
			continue // トップレベルの README.md などは無視
		}
		project := projEntry.Name()
		projDir := filepath.Join(dir, project)
		if includeActive {
			readTasksInDir(projDir, project, false, &tasks, &failures)
		}
		if includeArchive {
			readTasksInDir(filepath.Join(projDir, archiveDirName), project, true, &tasks, &failures)
		}
	}
	slices.SortFunc(tasks, func(a, b Task) int {
		return cmp.Or(
			cmp.Compare(a.Project, b.Project),
			compareID(a.ID, b.ID),
			boolCompare(a.Archived, b.Archived), // 同 id (active+archive 同時取得時) は active を先に
		)
	})
	return tasks, failures, nil
}

// readTasksInDir は d 直下の *.md を読み tasks/failures に積む。archived はそのディレクトリの
// タスクに付ける印 (active=false / archive=true)。d が存在しない (アーカイブ未作成など) ときは
// 失敗にせず黙って何もしない。サブディレクトリ (active 走査時の archive/ など) は読み飛ばす。
func readTasksInDir(d, project string, archived bool, tasks *[]Task, failures *[]LoadFailure) {
	files, err := os.ReadDir(d)
	if err != nil {
		if !os.IsNotExist(err) {
			*failures = append(*failures, LoadFailure{d, err})
		}
		return
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
			continue
		}
		path := filepath.Join(d, f.Name())
		t, err := parseTask(path)
		if err != nil {
			*failures = append(*failures, LoadFailure{path, err})
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
		// 空ファイル (0 バイト) = alloc-id の未記入予約 = 作成途中。印を付け、表示側で除外する
		// (書き込み後は size>0 になる。frontmatter を持つ既存タスクは title 欠けでも除外しない)。
		if info, e := f.Info(); e == nil && info.Size() == 0 {
			t.Incomplete = true
		}
		t.Archived = archived
		*tasks = append(*tasks, t)
	}
}

// boolCompare は false < true で比較する (アクティブを先、アーカイブを後に並べる用)。
func boolCompare(a, b bool) int {
	if a == b {
		return 0
	}
	if a {
		return 1
	}
	return -1
}

// compareID は ID を数値順で比較する。両方が非負整数としてパースできれば数値比較を
// 第1キーにし (例: "9999" < "10000"。4桁を超えても順序が破綻しない)、同値または
// パースできない側があれば文字列比較にフォールバックする。
func compareID(a, b string) int {
	na, ea := strconv.Atoi(a)
	nb, eb := strconv.Atoi(b)
	if ea == nil && eb == nil {
		return cmp.Or(cmp.Compare(na, nb), cmp.Compare(a, b))
	}
	return cmp.Compare(a, b)
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

// SyncStatus はストア (git repo) の同期状況。CLI の status / sync で使う。
type SyncStatus struct {
	NotGit     bool   // git 管理されていない (rev-parse 失敗)
	Dirty      int    // 未コミットの変更エントリ数 (working tree + index)
	Branch     string // 現在のブランチ名
	NoUpstream bool   // upstream (@{u}) 未設定
	Upstream   string // upstream の参照名 (例 origin/main)
	Ahead      int    // upstream に対して未 push のコミット数
	Behind     int    // upstream より遅れているコミット数
}

// Clean は「同期済み (未コミットも未 push も無い)」かを返す。
// git 管理外・upstream 未設定は同期判断ができないので Clean=false 扱い。
func (s SyncStatus) Clean() bool {
	return !s.NotGit && !s.NoUpstream && s.Dirty == 0 && s.Ahead == 0 && s.Behind == 0
}

// loadSyncStatus はストア dir の git 状態を集計する。git は main.go の同名ヘルパを使う
// (同一 package)。git 管理外・upstream 未設定はエラーにせず status のフラグで返す。
func loadSyncStatus(dir string) (SyncStatus, error) {
	var st SyncStatus
	if out, err := git(dir, "rev-parse", "--is-inside-work-tree"); err != nil || out != "true" {
		st.NotGit = true
		return st, nil
	}
	porcelain, err := git(dir, "status", "--porcelain")
	if err != nil {
		return st, fmt.Errorf("git status に失敗しました: %w", err)
	}
	if porcelain != "" {
		st.Dirty = len(strings.Split(porcelain, "\n"))
	}
	st.Branch, _ = git(dir, "rev-parse", "--abbrev-ref", "HEAD")

	up, err := git(dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		st.NoUpstream = true
		return st, nil
	}
	st.Upstream = up
	// rev-list --count --left-right @{u}...HEAD => "<behind>\t<ahead>"
	if counts, err := git(dir, "rev-list", "--count", "--left-right", "@{u}...HEAD"); err == nil {
		if behind, ahead, ok := strings.Cut(counts, "\t"); ok {
			st.Behind, _ = strconv.Atoi(strings.TrimSpace(behind))
			st.Ahead, _ = strconv.Atoi(strings.TrimSpace(ahead))
		}
	}
	return st, nil
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
	// 直前に出た「空値のリストキー」(prs / tracker)。後続のインデント項目 ("- value") を
	// ここへ集約する。新しいキー行が来たらリセットする。
	listKey := ""
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			// 先頭行に UTF-8 BOM が付いていると "---" 判定が外れて frontmatter を
			// 丸ごと取りこぼす (タスクが一覧から消える)。BOM を剥がしてから判定する。
			line = strings.TrimPrefix(line, "\ufeff")
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = true
				continue
			}
			break // frontmatter なし
		}
		if !inFrontmatter {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			break // frontmatter 終端
		}
		// YAML ブロックリストの項目 ("  - value")。直前のキーが空値リストのときだけ拾う。
		// URL は ":" を含むため、key:value 分割より前に処理する必要がある。
		if listKey != "" && strings.HasPrefix(trimmed, "- ") {
			if item := unquote(strings.TrimSpace(trimmed[len("- "):])); item != "" {
				switch listKey {
				case "prs":
					t.PRs = append(t.PRs, item)
				case "tracker":
					t.Tracker = append(t.Tracker, item)
				}
			}
			continue
		}
		listKey = "" // リスト項目でない行に来たら収集を終える
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
		case "issue":
			t.Issue = val
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
		case "prs":
			// 値が空ならブロックリスト形式 (後続の "- url" を集約)。
			// 同一行に値があれば 1 行カンマ区切りも一応許容する。
			if val == "" {
				listKey = "prs"
			} else {
				for _, p := range strings.Split(val, ",") {
					if p = strings.TrimSpace(p); p != "" {
						t.PRs = append(t.PRs, p)
					}
				}
			}
		case "tracker":
			// prs: と同じ扱い (ブロックリスト or 1 行カンマ区切り)。任意ホストの関連 URL。
			if val == "" {
				listKey = "tracker"
			} else {
				for _, p := range strings.Split(val, ",") {
					if p = strings.TrimSpace(p); p != "" {
						t.Tracker = append(t.Tracker, p)
					}
				}
			}
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
