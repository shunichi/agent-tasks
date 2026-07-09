package main

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// agentListJSON は herdr agent list 応答を組み立てる (session_id → pane の突合テスト用)。
func agentListJSON(pairs ...[2]string) string {
	items := ""
	for i, p := range pairs {
		if i > 0 {
			items += ","
		}
		items += `{"agent":"claude","agent_session":{"agent":"claude","kind":"id","source":"herdr:claude","value":"` +
			p[0] + `"},"agent_status":"working","pane_id":"` + p[1] + `"}`
	}
	return `{"result":{"agents":[` + items + `],"type":"agent_list"}}`
}

func TestHerdrAgentFocus(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	calls := stubHerdrRun(t, []byte(""), nil)

	if err := herdrAgentFocus("w3:p2"); err != nil {
		t.Fatalf("herdrAgentFocus: %v", err)
	}
	want := []string{"agent", "focus", "w3:p2"}
	if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("args: got %v want %v", *calls, want)
	}
}

func TestHerdrAgentFocusRequiresHerdr(t *testing.T) {
	t.Setenv("HERDR_ENV", "0")
	if err := herdrAgentFocus("w3:p1"); err == nil {
		t.Error("herdrAgentFocus: want error outside herdr")
	}
}

func TestResolveTaskPane(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)

	t.Run("found", func(t *testing.T) {
		t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
		stubHerdrRun(t, []byte(agentListJSON([2]string{"sid-A", "w3:p1"}, [2]string{"sid-B", "w3:p2"})), nil)
		if err := writeSessionLink("proj--0001", "sid-B", now); err != nil {
			t.Fatal(err)
		}
		pane, err := resolveTaskPane(Task{Project: "proj", ID: "0001", Worktree: "../proj--0001"})
		if err != nil {
			t.Fatalf("resolveTaskPane: %v", err)
		}
		if pane.PaneID != "w3:p2" {
			t.Errorf("pane = %q, want w3:p2", pane.PaneID)
		}
	})

	t.Run("latest session wins", func(t *testing.T) {
		t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
		// sid-old と sid-new の両方が pane を持つが、最新リンク (sid-new) を優先する。
		stubHerdrRun(t, []byte(agentListJSON([2]string{"sid-old", "w3:p1"}, [2]string{"sid-new", "w3:p9"})), nil)
		if err := writeSessionLink("proj--0002", "sid-old", now); err != nil {
			t.Fatal(err)
		}
		if err := writeSessionLink("proj--0002", "sid-new", now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		pane, err := resolveTaskPane(Task{Project: "proj", ID: "0002", Worktree: "../proj--0002"})
		if err != nil {
			t.Fatalf("resolveTaskPane: %v", err)
		}
		if pane.PaneID != "w3:p9" {
			t.Errorf("pane = %q, want w3:p9 (latest session)", pane.PaneID)
		}
	})

	t.Run("no link", func(t *testing.T) {
		t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
		stubHerdrRun(t, []byte(agentListJSON([2]string{"sid-A", "w3:p1"})), nil)
		if _, err := resolveTaskPane(Task{Project: "proj", ID: "0003", Worktree: "../proj--0003"}); err == nil {
			t.Error("want error when session not linked")
		}
	})

	t.Run("ended (link but no matching agent)", func(t *testing.T) {
		t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
		stubHerdrRun(t, []byte(agentListJSON([2]string{"sid-other", "w3:p1"})), nil)
		if err := writeSessionLink("proj--0004", "sid-gone", now); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveTaskPane(Task{Project: "proj", ID: "0004", Worktree: "../proj--0004"}); err == nil {
			t.Error("want error when linked pane ended")
		}
	})

	t.Run("human task", func(t *testing.T) {
		t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
		if _, err := resolveTaskPane(Task{Project: "proj", ID: "0005", Kind: kindHuman}); err == nil {
			t.Error("want error for human task (no pane)")
		}
	})

	t.Run("no worktree", func(t *testing.T) {
		t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
		if _, err := resolveTaskPane(Task{Project: "proj", ID: "0006"}); err == nil {
			t.Error("want error when worktree unrecorded")
		}
	})
}

func TestFocusTaskPaneRequiresHerdr(t *testing.T) {
	t.Setenv("HERDR_ENV", "0")
	if _, err := focusTaskPane(Task{Project: "proj", ID: "0001", Worktree: "../proj--0001"}); err == nil {
		t.Error("focusTaskPane: want error outside herdr")
	}
}

func TestFocusTaskPaneSuccess(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	if err := writeSessionLink("proj--0001", "sid-A", now); err != nil {
		t.Fatal(err)
	}
	// agent list → agent focus の 2 回呼ばれる。両方に同じスタブ出力を返す (focus は出力を見ない)。
	calls := stubHerdrRun(t, []byte(agentListJSON([2]string{"sid-A", "w3:p1"})), nil)

	pane, err := focusTaskPane(Task{Project: "proj", ID: "0001", Worktree: "../proj--0001"})
	if err != nil {
		t.Fatalf("focusTaskPane: %v", err)
	}
	if pane != "w3:p1" {
		t.Errorf("pane = %q, want w3:p1", pane)
	}
	// 最後の呼び出しが agent focus w3:p1 であること。
	last := (*calls)[len(*calls)-1]
	if !reflect.DeepEqual(last, []string{"agent", "focus", "w3:p1"}) {
		t.Errorf("last call = %v, want [agent focus w3:p1]", last)
	}
}

func TestFocusTaskPaneAgentListError(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	if err := writeSessionLink("proj--0001", "sid-A", now); err != nil {
		t.Fatal(err)
	}
	stubHerdrRun(t, nil, errors.New("boom"))
	if _, err := focusTaskPane(Task{Project: "proj", ID: "0001", Worktree: "../proj--0001"}); err == nil {
		t.Error("want error propagated from herdr agent list failure")
	}
}
