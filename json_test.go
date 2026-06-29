package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestToTaskJSONComputedFields(t *testing.T) {
	// session マーカーを確実に空にして in-progress を "unknown" にする。
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)

	// in-progress: session_state が付く (マーカー無し → unknown)。blocked_for は付かない。
	ip := toTaskJSON(Task{ID: "0001", Status: "in-progress", Worktree: "../p--0001"}, now)
	if ip.SessionState != "unknown" {
		t.Errorf("in-progress の session_state = %q, want unknown", ip.SessionState)
	}
	if ip.BlockedFor != "" {
		t.Errorf("in-progress に blocked_for が付いた: %q", ip.BlockedFor)
	}

	// blocked: blocked_for が付く。session_state は付かない。
	bl := toTaskJSON(Task{ID: "0002", Status: "blocked", BlockedAt: "2026-06-20T00:00:00+00:00"}, now)
	if bl.BlockedFor == "" {
		t.Error("blocked に blocked_for が付かない")
	}
	if bl.SessionState != "" {
		t.Errorf("blocked に session_state が付いた: %q", bl.SessionState)
	}

	// todo: どちらも付かない。
	td := toTaskJSON(Task{ID: "0003", Status: "todo"}, now)
	if td.SessionState != "" || td.BlockedFor != "" {
		t.Errorf("todo に計算済みフィールドが付いた: %+v", td)
	}
}

func TestWriteTasksJSONRoundTrip(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	rows := []Task{
		{ID: "0001", Project: "webapp", Title: "最初", Status: "todo", Created: "2026-06-01T00:00:00+09:00", Updated: "2026-06-01T00:00:00+09:00"},
		{ID: "0002", Project: "webapp", Title: "PR テスト", Status: "done", PRs: []string{"https://x/pull/1"}, Created: "c", Updated: "u"},
	}
	var buf bytes.Buffer
	if err := writeTasksJSON(&buf, rows, now); err != nil {
		t.Fatal(err)
	}
	var got []taskJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("出力が JSON 配列としてパースできない: %v\n%s", err, buf.String())
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != "0001" || got[1].PRs[0] != "https://x/pull/1" {
		t.Errorf("内容がずれている: %+v", got)
	}
}

func TestWriteTasksJSONEmptyIsArray(t *testing.T) {
	var buf bytes.Buffer
	if err := writeTasksJSON(&buf, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != "[]" {
		t.Errorf("空は [] であるべき, got %q", got)
	}
}

func TestWriteTaskJSONNoHTMLEscape(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	var buf bytes.Buffer
	if err := writeTaskJSON(&buf, Task{ID: "0001", Title: "a < b & c", Status: "todo"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	// < > & はエスケープされず生で出る。
	if !strings.Contains(buf.String(), "a < b & c") {
		t.Errorf("title が HTML エスケープされている: %s", buf.String())
	}
}
