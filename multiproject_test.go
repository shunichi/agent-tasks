package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestMatchProjects(t *testing.T) {
	// 空集合 = 横断 = 常に true。
	if !matchProjects("anything", nil) {
		t.Error("空集合は横断で常に true のはず")
	}
	if !matchProjects("a", []string{"a", "b"}) {
		t.Error("集合に含まれる project は true")
	}
	if matchProjects("c", []string{"a", "b"}) {
		t.Error("集合に無い project は false")
	}
}

func TestSplitProjects(t *testing.T) {
	got := splitProjects("a, b ,,c")
	if !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("splitProjects = %v, want [a b c] (trim + 空除去)", got)
	}
	if splitProjects("") != nil {
		t.Error("空文字は nil")
	}
}

// selectTasks に複数 project を渡すとその集合だけを横断表示する (部分集合横断)。
func TestSelectTasksMultiProject(t *testing.T) {
	store := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", store)
	write := func(proj, id string) {
		d := filepath.Join(store, proj)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, id+"-x.md"),
			[]byte("---\nid: \""+id+"\"\nproject: "+proj+"\ntitle: t\nstatus: todo\n---\n"), 0o644)
	}
	write("alpha", "0001")
	write("beta", "0001")
	write("gamma", "0001")

	rows, eff, _, err := selectTasks("", []string{"alpha", "gamma"}, false, false, false, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(eff, []string{"alpha", "gamma"}) {
		t.Errorf("effProjects = %v, want [alpha gamma]", eff)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.Project] = true
	}
	if !got["alpha"] || !got["gamma"] || got["beta"] {
		t.Errorf("複数 project フィルタが効いていない: %v", got)
	}
}

func TestScopeLabel(t *testing.T) {
	if s := scopeLabel([]string{"a"}); s != "project: a" {
		t.Errorf("単一 = %q, want 'project: a'", s)
	}
	if s := scopeLabel([]string{"a", "b"}); s != "projects: a, b" {
		t.Errorf("複数 = %q, want 'projects: a, b'", s)
	}
}
