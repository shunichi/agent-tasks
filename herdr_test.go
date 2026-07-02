package main

import (
	"errors"
	"reflect"
	"testing"
)

// stubHerdrRun は herdrRun を差し替え、渡された引数を記録しつつ固定の出力/エラーを返す。
// 復元は t.Cleanup で行う。
func stubHerdrRun(t *testing.T, out []byte, err error) *[][]string {
	t.Helper()
	orig := herdrRun
	var calls [][]string
	herdrRun = func(args ...string) ([]byte, error) {
		calls = append(calls, args)
		return out, err
	}
	t.Cleanup(func() { herdrRun = orig })
	return &calls
}

func TestHerdrEnvHelpers(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "w3:p1")
	t.Setenv("HERDR_WORKSPACE_ID", "w3")
	t.Setenv("HERDR_TAB_ID", "w3:t1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")

	if !herdrEnabled() {
		t.Error("herdrEnabled: want true")
	}
	if got := herdrPaneID(); got != "w3:p1" {
		t.Errorf("herdrPaneID: got %q", got)
	}
	if got := herdrWorkspaceID(); got != "w3" {
		t.Errorf("herdrWorkspaceID: got %q", got)
	}
	if got := herdrTabID(); got != "w3:t1" {
		t.Errorf("herdrTabID: got %q", got)
	}
	if got := herdrSocketPath(); got != "/tmp/h.sock" {
		t.Errorf("herdrSocketPath: got %q", got)
	}
}

func TestHerdrEnabledFalse(t *testing.T) {
	t.Setenv("HERDR_ENV", "")
	if herdrEnabled() {
		t.Error("herdrEnabled: want false when HERDR_ENV empty")
	}
}

func TestRequireHerdr(t *testing.T) {
	t.Run("outside herdr", func(t *testing.T) {
		t.Setenv("HERDR_ENV", "0")
		t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
		if err := requireHerdr(); err == nil {
			t.Error("want error when HERDR_ENV != 1")
		}
	})
	t.Run("no socket", func(t *testing.T) {
		t.Setenv("HERDR_ENV", "1")
		t.Setenv("HERDR_SOCKET_PATH", "")
		if err := requireHerdr(); err == nil {
			t.Error("want error when socket path empty")
		}
	})
	t.Run("ok", func(t *testing.T) {
		t.Setenv("HERDR_ENV", "1")
		t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
		if err := requireHerdr(); err != nil {
			t.Errorf("want nil, got %v", err)
		}
	})
}

func TestHerdrAgentGet(t *testing.T) {
	const js = `{"id":"cli:agent:get","result":{"agent":{"agent":"claude","agent_session":{"agent":"claude","kind":"id","source":"herdr:claude","value":"uuid-123"},"agent_status":"working","cwd":"/repo","focused":true,"pane_id":"w3:p1","tab_id":"w3:t1","workspace_id":"w3"},"type":"agent_info"}}`
	calls := stubHerdrRun(t, []byte(js), nil)

	a, err := herdrAgentGet("w3:p1")
	if err != nil {
		t.Fatalf("herdrAgentGet: %v", err)
	}
	if a.Agent != "claude" || a.AgentStatus != "working" || a.PaneID != "w3:p1" {
		t.Errorf("parsed agent wrong: %+v", a)
	}
	if a.AgentSession.Value != "uuid-123" {
		t.Errorf("agent_session.value: got %q", a.AgentSession.Value)
	}
	if !a.Focused {
		t.Error("focused: want true")
	}
	want := []string{"agent", "get", "w3:p1"}
	if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("args: got %v want %v", *calls, want)
	}
}

func TestHerdrAgentList(t *testing.T) {
	const js = `{"result":{"agents":[{"agent":"claude","agent_status":"working","pane_id":"w3:p1"},{"agent":"codex","agent_status":"idle","pane_id":"w3:p2"}],"type":"agent_list"}}`
	stubHerdrRun(t, []byte(js), nil)

	agents, err := herdrAgentList()
	if err != nil {
		t.Fatalf("herdrAgentList: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("want 2 agents, got %d", len(agents))
	}
	if agents[0].PaneID != "w3:p1" || agents[1].AgentStatus != "idle" {
		t.Errorf("parsed wrong: %+v", agents)
	}
}

func TestHerdrPaneList(t *testing.T) {
	const js = `{"result":{"panes":[{"agent_status":"unknown","pane_id":"w3:p3"}],"type":"pane_list"}}`
	calls := stubHerdrRun(t, []byte(js), nil)

	panes, err := herdrPaneList("w3")
	if err != nil {
		t.Fatalf("herdrPaneList: %v", err)
	}
	if len(panes) != 1 || panes[0].PaneID != "w3:p3" {
		t.Errorf("parsed wrong: %+v", panes)
	}
	want := []string{"pane", "list", "--workspace", "w3"}
	if !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("args: got %v want %v", (*calls)[0], want)
	}
}

func TestHerdrPaneListNoWorkspace(t *testing.T) {
	calls := stubHerdrRun(t, []byte(`{"result":{"panes":[]}}`), nil)
	if _, err := herdrPaneList(""); err != nil {
		t.Fatalf("herdrPaneList: %v", err)
	}
	want := []string{"pane", "list"}
	if !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("args: got %v want %v", (*calls)[0], want)
	}
}

func TestHerdrPaneReadReturnsText(t *testing.T) {
	calls := stubHerdrRun(t, []byte("line1\nline2\n"), nil)
	got, err := herdrPaneRead("w3:p1", "recent", 5)
	if err != nil {
		t.Fatalf("herdrPaneRead: %v", err)
	}
	if got != "line1\nline2\n" {
		t.Errorf("text: got %q", got)
	}
	want := []string{"pane", "read", "w3:p1", "--source", "recent", "--lines", "5"}
	if !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("args: got %v want %v", (*calls)[0], want)
	}
}

func TestHerdrActionArgs(t *testing.T) {
	tests := []struct {
		name string
		call func() error
		want []string
	}{
		{"send-text", func() error { return herdrPaneSendText("w3:p1", "hello world") },
			[]string{"pane", "send-text", "w3:p1", "hello world"}},
		{"run", func() error { return herdrPaneRun("w3:p1", "echo hi") },
			[]string{"pane", "run", "w3:p1", "echo hi"}},
		{"rename", func() error { return herdrAgentRename("w3:p1", "task 0106") },
			[]string{"agent", "rename", "w3:p1", "task 0106"}},
		{"wait", func() error { return herdrWaitAgentStatus("w3:p1", "blocked", 3000) },
			[]string{"wait", "agent-status", "w3:p1", "--status", "blocked", "--timeout", "3000"}},
		{"wait-no-timeout", func() error { return herdrWaitAgentStatus("w3:p1", "idle", 0) },
			[]string{"wait", "agent-status", "w3:p1", "--status", "idle"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := stubHerdrRun(t, []byte(""), nil)
			if err := tt.call(); err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			if !reflect.DeepEqual((*calls)[0], tt.want) {
				t.Errorf("args: got %v want %v", (*calls)[0], tt.want)
			}
		})
	}
}

func TestHerdrRunErrorPropagates(t *testing.T) {
	stubHerdrRun(t, nil, errors.New("boom"))
	if _, err := herdrAgentGet("w3:p1"); err == nil {
		t.Error("want error propagated from herdrRun")
	}
}

func TestHerdrAgentGetBadJSON(t *testing.T) {
	stubHerdrRun(t, []byte("not json"), nil)
	if _, err := herdrAgentGet("w3:p1"); err == nil {
		t.Error("want parse error on bad JSON")
	}
}

func TestHerdrSelfAgentRequiresHerdr(t *testing.T) {
	t.Setenv("HERDR_ENV", "0")
	if _, err := herdrSelfAgent(); err == nil {
		t.Error("herdrSelfAgent: want error outside herdr")
	}
}

func TestHerdrSelfAgentNoPaneID(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	t.Setenv("HERDR_PANE_ID", "")
	if _, err := herdrSelfAgent(); err == nil {
		t.Error("herdrSelfAgent: want error when HERDR_PANE_ID empty")
	}
}

func TestHerdrSelfAgentUsesPaneID(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	t.Setenv("HERDR_PANE_ID", "w3:p1")
	const js = `{"result":{"agent":{"agent":"claude","agent_status":"working","pane_id":"w3:p1"}}}`
	calls := stubHerdrRun(t, []byte(js), nil)
	a, err := herdrSelfAgent()
	if err != nil {
		t.Fatalf("herdrSelfAgent: %v", err)
	}
	if a.PaneID != "w3:p1" {
		t.Errorf("self pane: got %q", a.PaneID)
	}
	want := []string{"agent", "get", "w3:p1"}
	if !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("args: got %v want %v", (*calls)[0], want)
	}
}
