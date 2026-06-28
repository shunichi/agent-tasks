package main

import (
	"testing"
	"time"
)

func TestSessionIDFromURL(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://claude.ai/code/session_01ABC", "01ABC"},
		{"session_01ABC", "01ABC"},
		{"01ABC", "01ABC"},
		{"  https://claude.ai/code/session_XYZ  ", "XYZ"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := sessionIDFromURL(tt.in); got != tt.want {
			t.Errorf("sessionIDFromURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

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

func TestWriteReadSessionState(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)

	if _, ok := readSessionState("missing"); ok {
		t.Fatal("存在しないマーカーが ok=true")
	}
	if err := writeSessionState("sess1", sessWaiting, now); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, ok := readSessionState("sess1")
	if !ok {
		t.Fatal("書いたマーカーが読めない")
	}
	if got.State != sessWaiting {
		t.Errorf("state = %q, want %q", got.State, sessWaiting)
	}
	if got.Updated != "2026-06-29T12:00:00Z" {
		t.Errorf("updated = %q", got.Updated)
	}
	// 上書き
	if err := writeSessionState("sess1", sessWorking, now); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	if got, _ := readSessionState("sess1"); got.State != sessWorking {
		t.Errorf("上書き後 state = %q, want %q", got.State, sessWorking)
	}
}

func TestWriteSessionStateRejectsBadID(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"", "a/b", `a\b`} {
		if err := writeSessionState(id, sessWaiting, now); err == nil {
			t.Errorf("不正な id %q を受理した", id)
		}
	}
}
