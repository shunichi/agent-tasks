package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestValidateSlug(t *testing.T) {
	ok := []string{"a", "bookmark-dnd", "fix-0042-bug", "x1"}
	for _, s := range ok {
		if err := validateSlug(s); err != nil {
			t.Errorf("validateSlug(%q) = %v, want nil", s, err)
		}
	}
	ng := []string{"", "Foo", "with space", "slash/inside", "-lead", "trail-", "snake_case", "ドット"}
	for _, s := range ng {
		if err := validateSlug(s); err == nil {
			t.Errorf("validateSlug(%q) = nil, want error", s)
		}
	}
}

func TestMaxTaskID(t *testing.T) {
	dir := t.TempDir()
	if got := maxTaskID(dir); got != 0 {
		t.Errorf("maxTaskID(empty) = %d, want 0", got)
	}
	for _, name := range []string{"0001-a.md", "0007-b.md", "0003-c.md", "README.md", ".alloc.lock", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := maxTaskID(dir); got != 7 {
		t.Errorf("maxTaskID = %d, want 7", got)
	}
	// 4桁を超える連番も数値順で扱える。
	if err := os.WriteFile(filepath.Join(dir, "12345-big.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := maxTaskID(dir); got != 12345 {
		t.Errorf("maxTaskID = %d, want 12345", got)
	}
}

func TestAllocTaskFile(t *testing.T) {
	dir := t.TempDir()
	id, path, err := allocTaskFile(dir, "first")
	if err != nil {
		t.Fatal(err)
	}
	if id != "0001" {
		t.Errorf("id = %q, want 0001", id)
	}
	if want := filepath.Join(dir, "0001-first.md"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("reserved file not created: %v", err)
	}
	// 予約ファイルが採番に算入され、続けて採番すると番号が進む。
	id2, _, err := allocTaskFile(dir, "second")
	if err != nil {
		t.Fatal(err)
	}
	if id2 != "0002" {
		t.Errorf("id2 = %q, want 0002", id2)
	}
}

// 同名 (同 id 同 slug) が既にある場合は次の番号へ進む (O_EXCL の保険経路)。
func TestAllocTaskFileSkipsExistingName(t *testing.T) {
	dir := t.TempDir()
	// 0001-dup.md を先に置くが、maxTaskID 上の最大も 0001。採番候補 0002 になるので
	// この経路自体は通常踏まない。同番異 slug の衝突も leadingID 算入で避けられることを確認する。
	if err := os.WriteFile(filepath.Join(dir, "0002-other.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	id, _, err := allocTaskFile(dir, "mine")
	if err != nil {
		t.Fatal(err)
	}
	if id != "0003" {
		t.Errorf("id = %q, want 0003 (0002 は別 slug で埋まっている)", id)
	}
}

// 並行採番でも id が重複しないこと (ロックでローカル並行を直列化)。
func TestAllocTaskFileConcurrent(t *testing.T) {
	dir := t.TempDir()
	const n = 20
	var wg sync.WaitGroup
	ids := make([]string, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, _, err := allocTaskFile(dir, fmt.Sprintf("slug%d", i))
			ids[i], errs[i] = id, err
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("alloc %d failed: %v", i, errs[i])
		}
		if seen[ids[i]] {
			t.Errorf("duplicate id allocated: %s", ids[i])
		}
		seen[ids[i]] = true
	}
	// 0001..0020 が漏れなく採番されている。
	got := make([]string, 0, n)
	for id := range seen {
		got = append(got, id)
	}
	sort.Strings(got)
	for i, id := range got {
		if want := fmt.Sprintf("%04d", i+1); id != want {
			t.Errorf("got[%d] = %q, want %q", i, id, want)
		}
	}
}

func TestNormalizeKind(t *testing.T) {
	for in, want := range map[string]string{"": "", "code": "", "human": "human"} {
		got, err := normalizeKind(in)
		if err != nil {
			t.Errorf("normalizeKind(%q) err = %v", in, err)
		}
		if got != want {
			t.Errorf("normalizeKind(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := normalizeKind("bogus"); err == nil {
		t.Error("normalizeKind(bogus) = nil, want error")
	}
}

// buildNewTaskMarkdown の生成物が parseTask で読み戻せる (往復) こと。code タスク。
func TestBuildNewTaskMarkdownCode(t *testing.T) {
	now := time.Date(2026, 7, 8, 9, 30, 0, 0, time.FixedZone("JST", 9*3600))
	body := "ドラッグで並び替え。\n\n- 対象: 一覧"
	md := buildNewTaskMarkdown("0042", "webapp", "bookmark-dnd", "DnD 並び替え: 一覧", "", body, now, false)

	dir := t.TempDir()
	path := filepath.Join(dir, "0042-bookmark-dnd.md")
	if err := os.WriteFile(path, md, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseTask(path)
	if err != nil {
		t.Fatalf("parseTask: %v", err)
	}
	if got.ID != "0042" || got.Project != "webapp" || got.Status != "todo" {
		t.Errorf("id/project/status = %q/%q/%q", got.ID, got.Project, got.Status)
	}
	// title に ':' を含んでも最初の ':' 以降を丸ごと拾える。
	if want := "DnD 並び替え: 一覧"; got.Title != want {
		t.Errorf("title = %q, want %q", got.Title, want)
	}
	if got.Kind != "" {
		t.Errorf("kind = %q, want empty (code)", got.Kind)
	}
	if got.Branch != "task/0042-bookmark-dnd" || got.Worktree != "../webapp--0042" {
		t.Errorf("branch/worktree = %q/%q", got.Branch, got.Worktree)
	}
	if got.Created != "2026-07-08T09:30:00+09:00" || got.Updated != got.Created {
		t.Errorf("created/updated = %q/%q", got.Created, got.Updated)
	}
	if !strings.Contains(string(md), "## 進捗ログ\n- 2026-07-08 09:30 登録\n") {
		t.Errorf("進捗ログの登録行が期待通りでない:\n%s", md)
	}
	if !strings.Contains(string(md), "# 要件\n\nドラッグで並び替え。") {
		t.Errorf("要件セクションが期待通りでない:\n%s", md)
	}
}

// human タスクは kind: human 行を持ち、branch/worktree が空 (末尾スペースなし)。
func TestBuildNewTaskMarkdownHuman(t *testing.T) {
	now := time.Date(2026, 7, 8, 9, 30, 0, 0, time.FixedZone("JST", 9*3600))
	md := buildNewTaskMarkdown("0003", "webapp", "deploy", "デプロイ設定変更", "human", "手で変更。", now, false)
	s := string(md)
	if !strings.Contains(s, "\nkind: human\n") {
		t.Errorf("kind: human 行が無い:\n%s", s)
	}
	if !strings.Contains(s, "\nbranch:\n") || !strings.Contains(s, "\nworktree:\n") {
		t.Errorf("human は branch:/worktree: が空値 (末尾スペースなし) であるべき:\n%s", s)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "0003-deploy.md")
	if err := os.WriteFile(path, md, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseTask(path)
	if err != nil {
		t.Fatalf("parseTask: %v", err)
	}
	if got.Kind != "human" || got.Branch != "" || got.Worktree != "" {
		t.Errorf("kind/branch/worktree = %q/%q/%q", got.Kind, got.Branch, got.Worktree)
	}
}

// draft=true (TUI の簡易登録) は draft: true 行を立て、要件に詳細化の導線・進捗ログに簡易登録を残す。
// code タスクなので branch/worktree は通常どおり付く (draft は種別ではなく状態)。
func TestBuildNewTaskMarkdownDraft(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 14, 0, 0, time.FixedZone("JST", 9*3600))
	md := buildNewTaskMarkdown("0149", "webapp", "task", "あとで詳細化するやつ", "", "ざっくりメモ", now, true)
	s := string(md)
	if !strings.Contains(s, "\ndraft: true\n") {
		t.Errorf("draft: true 行が無い:\n%s", s)
	}
	if !strings.Contains(s, "着手前にエージェントが詳細化する") {
		t.Errorf("詳細化の導線が本文に無い:\n%s", s)
	}
	if !strings.Contains(s, "## 進捗ログ\n- 2026-07-15 10:14 簡易登録 (TUI)\n") {
		t.Errorf("進捗ログの簡易登録行が期待通りでない:\n%s", s)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "0149-task.md")
	if err := os.WriteFile(path, md, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseTask(path)
	if err != nil {
		t.Fatalf("parseTask: %v", err)
	}
	if !got.Draft || got.Status != "todo" {
		t.Errorf("draft/status = %v/%q, want true/todo", got.Draft, got.Status)
	}
	// draft は種別ではないので code タスクの branch/worktree は通常どおり埋まる。
	if got.Branch != "task/0149-task" || got.Worktree != "../webapp--0149" {
		t.Errorf("branch/worktree = %q/%q", got.Branch, got.Worktree)
	}
}

// slugFromTitle は ASCII をケバブ化し、非 ASCII は畳み、ASCII が無ければ task にフォールバックする。
// 返り値は必ず validateSlug を満たす。
func TestSlugFromTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Add DnD sort", "add-dnd-sort"},
		{"  fix: 0042 bug!! ", "fix-0042-bug"},
		{"日本語のみのタイトル", "task"},                // ASCII 英数字なし → フォールバック
		{"TUI から quick add", "tui-quick-add"}, // 混在は ASCII 部分だけ残る
		{"", "task"},
		{"---", "task"},
	}
	for _, c := range cases {
		got := slugFromTitle(c.in)
		if got != c.want {
			t.Errorf("slugFromTitle(%q) = %q, want %q", c.in, got, c.want)
		}
		if err := validateSlug(got); err != nil {
			t.Errorf("slugFromTitle(%q) = %q が validateSlug を満たさない: %v", c.in, got, err)
		}
	}
	// 長いタイトルは頭打ちされ、それでも valid。
	long := slugFromTitle(strings.Repeat("ab-", 40))
	if len(long) > slugMaxLen {
		t.Errorf("slug 長 %d > %d (頭打ちされていない): %q", len(long), slugMaxLen, long)
	}
	if err := validateSlug(long); err != nil {
		t.Errorf("頭打ち後 slug が invalid: %v (%q)", err, long)
	}
}

// createDraftTask は draft タスクを採番して書き、parseTask で draft/title が読み戻せる (往復)。
func TestCreateDraftTask(t *testing.T) {
	dir := t.TempDir()
	id, err := createDraftTask(dir, "webapp", "日本語タイトルの簡易登録", "説明も少し")
	if err != nil {
		t.Fatal(err)
	}
	if id != "0001" {
		t.Errorf("id = %q, want 0001", id)
	}
	// 日本語のみタイトルは slug=task にフォールバックするのでファイル名は 0001-task.md。
	path := filepath.Join(dir, "webapp", "0001-task.md")
	got, err := parseTask(path)
	if err != nil {
		t.Fatalf("parseTask(%s): %v", path, err)
	}
	if !got.Draft {
		t.Errorf("Draft = false, want true")
	}
	if got.Title != "日本語タイトルの簡易登録" || got.Status != "todo" {
		t.Errorf("title/status = %q/%q", got.Title, got.Status)
	}
}

// allocReserve に write を渡すと採番と同時に中身を書き込む (フル生成モード)。
func TestAllocReserveWritesContent(t *testing.T) {
	dir := t.TempDir()
	id, path, err := allocReserve(dir, "task", func(id string) []byte {
		return []byte("id=" + id + "\n")
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "0001" {
		t.Errorf("id = %q, want 0001", id)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "id=0001\n" {
		t.Errorf("content = %q, want %q", b, "id=0001\n")
	}
}

func TestLockProjectStealsStaleLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, allocLockName)
	if err := os.WriteFile(lockPath, []byte("99999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// mtime を stale 閾値より古くする。
	past := time.Now().Add(-(allocLockStale + time.Minute))
	if err := os.Chtimes(lockPath, past, past); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockProject(dir)
	if err != nil {
		t.Fatalf("lockProject should steal stale lock: %v", err)
	}
	unlock()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock file should be removed after unlock, stat err = %v", err)
	}
}
