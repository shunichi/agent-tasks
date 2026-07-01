package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseTask(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0001-foo.md")
	content := `---
id: "0001"
project: webapp
title: ブックマークのドラッグ並び替え
status: in-progress
agent: claude
---

# 要件
本文
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseTask(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "0001" {
		t.Errorf("ID = %q, want 0001", got.ID)
	}
	if got.Project != "webapp" {
		t.Errorf("Project = %q, want webapp", got.Project)
	}
	if got.Title != "ブックマークのドラッグ並び替え" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.Status != "in-progress" {
		t.Errorf("Status = %q, want in-progress", got.Status)
	}
}

func TestParseTaskNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.md")
	if err := os.WriteFile(path, []byte("# just markdown\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseTask(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "" || got.Title != "" {
		t.Errorf("expected empty task, got %+v", got)
	}
}

func TestParseTaskBOM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.md")
	// 先頭に UTF-8 BOM が付いた frontmatter。BOM を剥がして正しく読めること。
	content := "\ufeff---\nid: \"0007\"\ntitle: BOM テスト\n---\n# 本文\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseTask(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "0007" || got.Title != "BOM テスト" {
		t.Errorf("BOM 付き frontmatter を取りこぼした: %+v", got)
	}
}

func TestLoadTasksSorted(t *testing.T) {
	dir := t.TempDir()
	write := func(proj, name, body string) {
		d := filepath.Join(dir, proj)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, name), []byte(body), 0o644)
	}
	write("zeta", "0002-b.md", "---\nid: \"0002\"\nstatus: todo\n---\n")
	write("zeta", "0001-a.md", "---\nid: \"0001\"\nstatus: todo\n---\n")
	write("alpha", "0001-a.md", "---\nid: \"0001\"\nstatus: done\n---\n")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("ignore me"), 0o644)

	tasks, err := loadTasks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("len = %d, want 3 (README.md must be ignored)", len(tasks))
	}
	// project 昇順 -> id 昇順
	if tasks[0].Project != "alpha" || tasks[1].Project != "zeta" || tasks[1].ID != "0001" {
		t.Errorf("sort order wrong: %+v", tasks)
	}
}

// TestLoadTasksNumericIDSort は ID が4桁を超えても数値順になることを確認する。
// 文字列比較だと "10000" < "9999" で逆転するため、数値比較を第1キーにする (0035)。
func TestLoadTasksNumericIDSort(t *testing.T) {
	dir := t.TempDir()
	write := func(name, id string) {
		d := filepath.Join(dir, "big")
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, name), []byte("---\nid: \""+id+"\"\nstatus: todo\n---\n"), 0o644)
	}
	write("9999-a.md", "9999")
	write("10000-b.md", "10000")
	write("0001-c.md", "0001")

	tasks, err := loadTasks(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, tk := range tasks {
		ids = append(ids, tk.ID)
	}
	want := []string{"0001", "9999", "10000"}
	if !slices.Equal(ids, want) {
		t.Errorf("id order = %v, want %v (numeric, not lexical)", ids, want)
	}
}

func TestCompareID(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"9999", "10000", -1}, // 数値: 9999 < 10000 (文字列だと逆)
		{"10000", "9999", 1},
		{"0001", "0002", -1},
		{"0005", "0005", 0},
		{"abc", "9999", 1},  // 非数値はフォールバック (文字列比較)
		{"9999", "abc", -1}, // 数値 vs 非数値もフォールバック
		{"foo", "bar", 1},   // 両方非数値: 文字列比較
	}
	for _, c := range cases {
		if got := compareID(c.a, c.b); got != c.want {
			t.Errorf("compareID(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestEditorArgv(t *testing.T) {
	// 既定は code。
	t.Setenv("AGENT_TASKS_EDITOR", "")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if got := editorArgv(); len(got) != 1 || got[0] != "code" {
		t.Errorf("default = %v, want [code]", got)
	}

	// 優先順位 AGENT_TASKS_EDITOR > VISUAL > EDITOR、かつ引数も分割する。
	t.Setenv("EDITOR", "vi")
	t.Setenv("VISUAL", "nano")
	t.Setenv("AGENT_TASKS_EDITOR", "code -w")
	if got := editorArgv(); !slices.Equal(got, []string{"code", "-w"}) {
		t.Errorf("got %v, want [code -w]", got)
	}

	t.Setenv("AGENT_TASKS_EDITOR", "")
	if got := editorArgv(); !slices.Equal(got, []string{"nano"}) {
		t.Errorf("VISUAL precedence: got %v, want [nano]", got)
	}
}

func TestNormalizeID(t *testing.T) {
	cases := map[string]string{
		"5":     "0005", // 短縮形を4桁ゼロ埋めに
		"05":    "0005", // 中途半端なゼロ埋めも揃える
		"0005":  "0005", // 既存形式はそのまま
		"0":     "0000",
		"12345": "12345", // 4桁を超えても切り詰めない
		"foo":   "foo",   // 非数値はそのまま
		"":      "",      // 空も素通し
		"-5":    "-5",    // 負数は照合しない (そのまま返す)
	}
	for in, want := range cases {
		if got := normalizeID(in); got != want {
			t.Errorf("normalizeID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveTaskPathShortID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", dir)
	proj := filepath.Join(dir, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(proj, "0005-flexible.md")
	if err := os.WriteFile(want, []byte("---\nid: \"0005\"\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"5", "05", "0005"} {
		got, err := resolveTaskPath("demo", id)
		if err != nil {
			t.Errorf("resolveTaskPath(demo, %q) error: %v", id, err)
			continue
		}
		if got != want {
			t.Errorf("resolveTaskPath(demo, %q) = %q, want %q", id, got, want)
		}
	}

	if _, err := resolveTaskPath("demo", "9"); err == nil {
		t.Error("存在しない id 9 はエラーになるべき")
	}
}

func TestResolveListScope(t *testing.T) {
	cases := []struct {
		name        string
		filterProj  []string
		allProjects bool
		current     string
		wantProj    []string
		wantCross   bool
	}{
		{"既定は現在 project", nil, false, "webapp", []string{"webapp"}, false},
		{"--all-projects で横断", nil, true, "webapp", nil, true},
		{"--project 明示は別 project でも従う", []string{"other"}, false, "webapp", []string{"other"}, false},
		{"--project は --all-projects より優先", []string{"other"}, true, "webapp", []string{"other"}, false},
		{"複数 project は部分集合横断", []string{"a", "b"}, false, "webapp", []string{"a", "b"}, false},
		{"複数 project は --all-projects より優先", []string{"a", "b"}, true, "webapp", []string{"a", "b"}, false},
		{"git 外は横断にフォールバック", nil, false, "", nil, true},
		{"git 外でも --project は効く", []string{"other"}, false, "", []string{"other"}, false},
	}
	for _, tc := range cases {
		gotProj, gotCross := resolveListScope(tc.filterProj, tc.allProjects, tc.current)
		if !slices.Equal(gotProj, tc.wantProj) || gotCross != tc.wantCross {
			t.Errorf("%s: resolveListScope(%v, %v, %q) = (%v, %v), want (%v, %v)",
				tc.name, tc.filterProj, tc.allProjects, tc.current,
				gotProj, gotCross, tc.wantProj, tc.wantCross)
		}
	}
}

func TestDispWidth(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"abc", 3},
		{"あ", 2},   // 全角
		{"aあ", 3},  // 半角 + 全角
		{"✅", 2},   // 絵文字 (自前実装では幅1に誤計算していた)
		{"💡a", 3},  // 絵文字 + 半角
		{"ｱ", 1},   // 半角カタカナ
		{"á", 1},  // 結合文字 (アクセント) は幅0
		{"a​b", 2}, // ゼロ幅スペースは幅0
	}
	for _, c := range cases {
		if got := dispWidth(c.in); got != c.want {
			t.Errorf("dispWidth(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestTruncateDispEmoji(t *testing.T) {
	// 絵文字 (幅2) を含む文字列の truncate。max=4 なら "💡" (2) + "…" (1) で収まる範囲まで。
	if got := truncateDisp("💡💡💡", 4); dispWidth(got) > 4 {
		t.Errorf("truncateDisp(💡💡💡, 4) = %q (width %d > 4)", got, dispWidth(got))
	}
	// max 以内ならそのまま。
	if got := truncateDisp("あい", 4); got != "あい" {
		t.Errorf("truncateDisp(あい, 4) = %q, want あい", got)
	}
}

func TestLeadingID(t *testing.T) {
	cases := map[string]string{
		"0005-store-git-sync.md": "0005",
		"12-foo.md":              "12",
		"0001.md":                "0001",
		"README.md":              "", // 数字始まりでない
		"abc.md":                 "",
	}
	for in, want := range cases {
		if got := leadingID(in); got != want {
			t.Errorf("leadingID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFindDuplicateIDs(t *testing.T) {
	dir := t.TempDir()
	write := func(proj, name, body string) {
		d := filepath.Join(dir, proj)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, name), []byte(body), 0o644)
	}
	// webapp/0015 が 2 ファイルで重複。webapp/0001 と other/0015 は単独。
	write("webapp", "0015-foo.md", "---\nid: \"0015\"\nstatus: todo\n---\n")
	write("webapp", "0015-bar.md", "---\nid: \"0015\"\nstatus: todo\n---\n")
	write("webapp", "0001-a.md", "---\nid: \"0001\"\nstatus: todo\n---\n")
	write("other", "0015-x.md", "---\nid: \"0015\"\nstatus: todo\n---\n")

	tasks, err := loadTasks(dir)
	if err != nil {
		t.Fatal(err)
	}
	dups := findDuplicateIDs(tasks)
	if len(dups) != 1 {
		t.Fatalf("len(dups) = %d, want 1 (%+v)", len(dups), dups)
	}
	if dups[0].Project != "webapp" || dups[0].ID != "0015" {
		t.Errorf("dup = %s/%s, want webapp/0015", dups[0].Project, dups[0].ID)
	}
	if len(dups[0].Paths) != 2 {
		t.Errorf("paths = %v, want 2 files", dups[0].Paths)
	}
}

func TestFindIDMismatches(t *testing.T) {
	dir := t.TempDir()
	write := func(proj, name, body string) {
		d := filepath.Join(dir, proj)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, name), []byte(body), 0o644)
	}
	// ファイル名 0016 だが frontmatter は 0015 -> 不一致。
	write("webapp", "0016-foo.md", "---\nid: \"0015\"\nstatus: todo\n---\n")
	// 一致 -> 検出されない。
	write("webapp", "0001-ok.md", "---\nid: \"0001\"\nstatus: todo\n---\n")

	tasks, err := loadTasks(dir)
	if err != nil {
		t.Fatal(err)
	}
	ms := findIDMismatches(tasks)
	if len(ms) != 1 {
		t.Fatalf("len(mismatches) = %d, want 1 (%+v)", len(ms), ms)
	}
	if ms[0].FileID != "0016" || ms[0].MetaID != "0015" {
		t.Errorf("mismatch = file=%s meta=%s, want file=0016 meta=0015", ms[0].FileID, ms[0].MetaID)
	}
}

func TestSyncCommitMessage(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "0005-sync.md"),
		[]byte("---\nid: \"0005\"\nstatus: in-progress\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 単一の変更 -> 件名にステータス込みで1件。
	single := syncCommitMessage(dir, "M\tdemo/0005-sync.md")
	if single != "tasks: demo/0005 (in-progress)" {
		t.Errorf("single = %q", single)
	}

	// 削除はファイルを読まず removed 扱い。
	del := syncCommitMessage(dir, "D\tdemo/0009-old.md")
	if del != "tasks: demo/0009 (removed)" {
		t.Errorf("deleted = %q", del)
	}

	// 複数 -> 件数を件名に、本文に列挙。タスク以外 (README) は無視。
	multi := syncCommitMessage(dir, "M\tdemo/0005-sync.md\nD\tdemo/0009-old.md\nM\tREADME.md")
	if !strings.HasPrefix(multi, "tasks: update 2 tasks\n\n- demo/0005 (in-progress)\n- demo/0009 (removed)") {
		t.Errorf("multi = %q", multi)
	}

	// タスクファイルが無い変更のみ -> 汎用メッセージ。
	if got := syncCommitMessage(dir, "M\tREADME.md"); got != "tasks: sync store" {
		t.Errorf("generic = %q", got)
	}
}
