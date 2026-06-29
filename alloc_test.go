package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
