package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseWorktreeKey(t *testing.T) {
	tests := []struct {
		key         string
		project, id string
		ok          bool
	}{
		{"agent-tasks--0037", "agent-tasks", "0037", true},
		{"family-app2--0001", "family-app2", "0001", true},
		{"webapp--12345", "webapp", "12345", true},
		// project 名に "--" を含んでも末尾の "--<数字>" で割れる。
		{"a--b--0002", "a--b", "0002", true},
		// worktree でない / 不正なものは ok=false。
		{"agent-tasks", "", "", false},       // 区切り無し
		{"agent-tasks--", "", "", false},     // id 空
		{"--0001", "", "", false},            // project 空
		{"agent-tasks--abc", "", "", false},  // id が数字でない
		{"agent-tasks--00x1", "", "", false}, // id に非数字混入
		{"", "", "", false},
	}
	for _, tt := range tests {
		project, id, ok := parseWorktreeKey(tt.key)
		if ok != tt.ok || project != tt.project || id != tt.id {
			t.Errorf("parseWorktreeKey(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.key, project, id, ok, tt.project, tt.id, tt.ok)
		}
	}
}

func TestFormatStatusLine(t *testing.T) {
	task := Task{Project: "agent-tasks", ID: "0037", Title: "短いタイトル", Status: "in-progress"}
	got := formatStatusLine(task, colors{}) // 色なしで素の文字列を検証
	want := "agent-tasks #0037 短いタイトル [in-progress]"
	if got != want {
		t.Errorf("formatStatusLine = %q, want %q", got, want)
	}

	// 長い title は表示幅で切り詰める (statusLineTitleWidth 上限、末尾に省略記号)。
	longTitle := strings.Repeat("x", 100)
	long := Task{Project: "p", ID: "0001", Title: longTitle, Status: "todo"}
	got = formatStatusLine(long, colors{})
	if strings.Contains(got, longTitle) {
		t.Errorf("長い title が切り詰められていない: %q", got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("切り詰め時に省略記号が無い: %q", got)
	}
}

// resolveCurrentTask は session_id を link で逆引きしてタスクを引く (通常フロー: cwd はメインリポ)。
func TestResolveCurrentTaskBySession(t *testing.T) {
	store := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", store)
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	if err := os.MkdirAll(filepath.Join(store, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	task := "---\nid: \"0007\"\nproject: proj\ntitle: 例のタスク\nstatus: in-progress\nworktree: ../proj--0007\n---\n"
	if err := os.WriteFile(filepath.Join(store, "proj", "0007-example.md"), []byte(task), 0o644); err != nil {
		t.Fatal(err)
	}
	// link: proj--0007 → session sid-1。
	if err := writeSessionLink("proj--0007", "sid-1", time.Now()); err != nil {
		t.Fatal(err)
	}

	// cwd は非 git の一時ディレクトリ (cwd 経路は当たらず session_id 経路で解決される)。
	nonGit := t.TempDir()
	got, ok := resolveCurrentTask(nonGit, "sid-1")
	if !ok || got.ID != "0007" || got.Title != "例のタスク" {
		t.Fatalf("resolveCurrentTask = %+v ok=%v, want id=0007", got, ok)
	}

	// 一致しない session_id では見つからない。
	if _, ok := resolveCurrentTask(nonGit, "sid-x"); ok {
		t.Error("未知の session_id で ok=true")
	}
	// session_id 空でも (cwd 非 git なので) 見つからない。
	if _, ok := resolveCurrentTask(nonGit, ""); ok {
		t.Error("session_id 空で ok=true")
	}
}

// worktreeKeyForSession は同じ session_id を持つ link のうち最も新しいキーを返す。
func TestWorktreeKeyForSession(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	older := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)

	if err := writeSessionLink("proj--0001", "sid", older); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionLink("proj--0002", "sid", newer); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionLink("proj--0003", "other", newer); err != nil {
		t.Fatal(err)
	}

	key, ok := worktreeKeyForSession("sid")
	if !ok || key != "proj--0002" {
		t.Errorf("worktreeKeyForSession(sid) = %q ok=%v, want proj--0002", key, ok)
	}
	if _, ok := worktreeKeyForSession("none"); ok {
		t.Error("未知 session で ok=true")
	}
	if _, ok := worktreeKeyForSession(""); ok {
		t.Error("空 session で ok=true")
	}
}

// statusLine は解決できないとき空文字を返す (status line を空にする)。
func TestStatusLineEmptyWhenUnresolved(t *testing.T) {
	t.Setenv("AGENT_TASKS_STORE", t.TempDir())
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	if got := statusLine(t.TempDir(), "nope", colors{}); got != "" {
		t.Errorf("statusLine = %q, want empty", got)
	}
}

// statusLineColors は NO_COLOR を尊重しつつ既定で色を出す (status line は非 TTY でも端末に出る)。
func TestStatusLineColors(t *testing.T) {
	defer func() { colorMode = "auto" }()

	colorMode = "auto"
	t.Setenv("NO_COLOR", "")
	if c := statusLineColors(); c.reset == "" {
		t.Error("auto + NO_COLOR 未設定で色が出ない")
	}
	t.Setenv("NO_COLOR", "1")
	if c := statusLineColors(); c.reset != "" {
		t.Error("NO_COLOR=1 で色が出た")
	}
	t.Setenv("NO_COLOR", "")
	colorMode = "never"
	if c := statusLineColors(); c.reset != "" {
		t.Error("--color never で色が出た")
	}
	colorMode = "always"
	t.Setenv("NO_COLOR", "1")
	if c := statusLineColors(); c.reset == "" {
		t.Error("--color always で色が出ない (NO_COLOR より優先のはず)")
	}
}
