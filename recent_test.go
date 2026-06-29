package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSelectRecent(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "webapp")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(proj, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// done + completed_at (順不同に置く)。
	write("0001-a.md", "---\nid: \"0001\"\nstatus: done\ncompleted_at: \"2026-06-01T00:00:00+09:00\"\n---\n")
	write("0002-b.md", "---\nid: \"0002\"\nstatus: done\ncompleted_at: \"2026-06-03T00:00:00+09:00\"\n---\n")
	write("0003-c.md", "---\nid: \"0003\"\nstatus: done\ncompleted_at: \"2026-06-02T00:00:00+09:00\"\n---\n")
	// done だが completed_at 無し → 除外。
	write("0004-d.md", "---\nid: \"0004\"\nstatus: done\n---\n")
	// done 以外 → 除外。
	write("0005-e.md", "---\nid: \"0005\"\nstatus: todo\n---\n")
	t.Setenv("AGENT_TASKS_STORE", dir)

	// allProjects=true でスコープ判定 (currentProject) に依存させない。
	rows, err := selectRecent("", true, 10)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	// completed_at 降順: 0002(06-03) > 0003(06-02) > 0001(06-01)。0004/0005 は除外。
	if !slices.Equal(ids, []string{"0002", "0003", "0001"}) {
		t.Errorf("order = %v, want [0002 0003 0001]", ids)
	}
}

func TestSelectRecentLimit(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "webapp")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []struct{ id, day string }{{"0001", "01"}, {"0002", "02"}, {"0003", "03"}} {
		body := "---\nid: \"" + d.id + "\"\nstatus: done\ncompleted_at: \"2026-06-" + d.day + "T00:00:00+09:00\"\n---\n"
		if err := os.WriteFile(filepath.Join(proj, d.id+"-x.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AGENT_TASKS_STORE", dir)

	rows, err := selectRecent("", true, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "0003" || rows[1].ID != "0002" {
		t.Errorf("limit=2 の結果が想定外: %+v", rows)
	}
}
