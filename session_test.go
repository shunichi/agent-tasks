package main

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionStateFor(t *testing.T) {
	tests := []struct {
		event, notif, want string
	}{
		{"UserPromptSubmit", "", sessWorking},
		{"PreToolUse", "", sessWorking},
		{"PostToolUse", "", sessWorking},
		{"SessionStart", "", sessWorking},
		{"Stop", "", sessWaiting},
		{"Notification", "permission_prompt", sessWaiting},
		{"Notification", "idle_prompt", sessWaiting},
		{"Notification", "auth_success", ""}, // 状態に影響しない
		{"Notification", "", ""},
		{"SessionEnd", "", sessEnded},
		{"StopFailure", "", sessEnded},
		{"PreCompact", "", ""}, // 未対応イベントは無視
	}
	for _, tt := range tests {
		if got := sessionStateFor(tt.event, tt.notif); got != tt.want {
			t.Errorf("sessionStateFor(%q,%q) = %q, want %q", tt.event, tt.notif, got, tt.want)
		}
	}
}

func TestTaskSessionKey(t *testing.T) {
	tests := []struct {
		worktree, want string
	}{
		{"../agent-tasks--0020", "agent-tasks--0020"},
		{"/abs/path/family-app2--0001", "family-app2--0001"},
		{"", ""},
	}
	for _, tt := range tests {
		got := taskSessionKey(Task{Worktree: tt.worktree})
		if got != tt.want {
			t.Errorf("taskSessionKey(%q) = %q, want %q", tt.worktree, got, tt.want)
		}
	}
}

func TestWorktreeKey(t *testing.T) {
	// git 管理外は ""。
	nonGit := t.TempDir()
	if got := worktreeKey(nonGit); got != "" {
		t.Errorf("git 外で worktreeKey = %q, want \"\"", got)
	}

	// git repo の root basename を返す。
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Skipf("git init 不可のためスキップ: %v (%s)", err, out)
	}
	// git は realpath を返すので、比較側も symlink 解決した basename にそろえる。
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		real = dir
	}
	want := filepath.Base(real)
	if got := worktreeKey(dir); got != want {
		t.Errorf("worktreeKey(%q) = %q, want %q", dir, got, want)
	}
}

func TestWriteReadSessionState(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)

	if _, ok := readSessionState("missing"); ok {
		t.Fatal("存在しないマーカーが ok=true")
	}
	if err := writeSessionState("agent-tasks--0020", sessWaiting, "/wt/agent-tasks--0020", now); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, ok := readSessionState("agent-tasks--0020")
	if !ok {
		t.Fatal("書いたマーカーが読めない")
	}
	if got.State != sessWaiting {
		t.Errorf("state = %q, want %q", got.State, sessWaiting)
	}
	if got.Updated != "2026-06-29T12:00:00Z" {
		t.Errorf("updated = %q", got.Updated)
	}
	if got.Cwd != "/wt/agent-tasks--0020" {
		t.Errorf("cwd = %q", got.Cwd)
	}
	// 上書き
	if err := writeSessionState("agent-tasks--0020", sessWorking, "", now); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	if got, _ := readSessionState("agent-tasks--0020"); got.State != sessWorking {
		t.Errorf("上書き後 state = %q, want %q", got.State, sessWorking)
	}
}

func TestWriteSessionStateRejectsBadKey(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	for _, key := range []string{"", "a/b", `a\b`} {
		if err := writeSessionState(key, sessWaiting, "", now); err == nil {
			t.Errorf("不正な key %q を受理した", key)
		}
	}
}
