package main

import (
	"reflect"
	"testing"
)

func TestSpawnArgv(t *testing.T) {
	tests := []struct {
		name  string
		agent string
		label string
		want  []string
	}{
		{"claude uses -n label", "claude", "task 0001: X",
			[]string{"claude", "-n", "task 0001: X", "タスク 0001 に着手して"}},
		{"other agent omits -n", "codex", "task 0001: X",
			[]string{"codex", "タスク 0001 に着手して"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := spawnArgv(tt.agent, tt.label, "タスク 0001 に着手して")
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("spawnArgv: got %v want %v", got, tt.want)
			}
		})
	}
}

func TestHerdrAgentStartArgsAndParse(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	const js = `{"result":{"agent":{"agent":"claude","agent_status":"unknown","name":"task 0001: X","pane_id":"w3:p9","tab_id":"w3:t1","workspace_id":"w3"},"type":"agent_started"}}`
	calls := stubHerdrRun(t, []byte(js), nil)

	pane, err := herdrAgentStart("task 0001: X", "/repo", "down", false,
		[]string{"claude", "-n", "task 0001: X", "着手して"})
	if err != nil {
		t.Fatalf("herdrAgentStart: %v", err)
	}
	if pane.PaneID != "w3:p9" {
		t.Errorf("pane_id: got %q want w3:p9", pane.PaneID)
	}
	want := []string{"agent", "start", "task 0001: X", "--cwd", "/repo", "--split", "down",
		"--no-focus", "--", "claude", "-n", "task 0001: X", "着手して"}
	if !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("args:\n got %v\nwant %v", (*calls)[0], want)
	}
}

func TestHerdrAgentStartFocus(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	calls := stubHerdrRun(t, []byte(`{"result":{"agent":{"pane_id":"w3:p9"}}}`), nil)
	if _, err := herdrAgentStart("L", "", "", true, []string{"bash"}); err != nil {
		t.Fatalf("herdrAgentStart: %v", err)
	}
	// cwd/split 空なら該当フラグを付けず、focus=true なら --focus。
	want := []string{"agent", "start", "L", "--focus", "--", "bash"}
	if !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("args: got %v want %v", (*calls)[0], want)
	}
}

func TestHerdrAgentStartRequiresHerdr(t *testing.T) {
	t.Setenv("HERDR_ENV", "0")
	if _, err := herdrAgentStart("L", "/repo", "down", false, []string{"claude"}); err == nil {
		t.Error("herdr 外では requireHerdr で止まるはず")
	}
}

func TestHerdrAgentStartEmptyArgv(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	if _, err := herdrAgentStart("L", "/repo", "down", false, nil); err == nil {
		t.Error("argv 空はエラーにすべき")
	}
}
