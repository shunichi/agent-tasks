package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSpawnAgentArgs(t *testing.T) {
	tests := []struct {
		name  string
		agent string
		label string
		want  []string
	}{
		{"claude uses -n label", "claude", "task 0001: X",
			[]string{"-n", "task 0001: X", "タスク 0001 に着手して"}},
		{"other agent omits -n", "codex", "task 0001: X",
			[]string{"タスク 0001 に着手して"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := spawnAgentArgs(tt.agent, tt.label, "タスク 0001 に着手して")
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("spawnAgentArgs: got %v want %v", got, tt.want)
			}
		})
	}
}

// agent 名は herdr の制約 (小文字始まり / [a-z0-9_-] / 1-32 文字) を必ず満たし、project と id を残す。
func TestSpawnAgentName(t *testing.T) {
	tests := []struct {
		name string
		task Task
		want string
	}{
		{"project と id を含む", Task{Project: "webapp", ID: "0007"}, "task-webapp-0007"},
		{"大文字と記号は落とす", Task{Project: "Rails_App.v2", ID: "0012"}, "task-rails_app-v2-0012"},
		{"日本語 project でも壊れない", Task{Project: "家計簿", ID: "0003"}, "task-0003"},
		{"長い project は詰めて id を残す",
			Task{Project: "very-long-project-name-that-overflows", ID: "0123"},
			"task-very-long-project-name-0123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := spawnAgentName(tt.task)
			if got != tt.want {
				t.Errorf("spawnAgentName = %q, want %q", got, tt.want)
			}
			assertValidHerdrAgentName(t, got)
		})
	}
}

// 候補名は第一候補 + 番号サフィックスで、いずれも herdr の制約を満たす。
func TestHerdrAgentNameCandidates(t *testing.T) {
	got := herdrAgentNameCandidates("task-webapp-0007")
	if got[0] != "task-webapp-0007" || got[1] != "task-webapp-0007-2" {
		t.Errorf("候補の先頭が想定と違う: %v", got[:2])
	}
	// 32 文字ちょうどの名前でも、サフィックス分は base を詰めて枠を守る。
	long := strings.Repeat("a", herdrAgentNameMax)
	for _, n := range herdrAgentNameCandidates(long) {
		assertValidHerdrAgentName(t, n)
	}
	// 候補はすべて異なる (衝突回避にならない重複を作らない)。
	seen := map[string]bool{}
	for _, n := range herdrAgentNameCandidates(long) {
		if seen[n] {
			t.Errorf("候補名が重複した: %q", n)
		}
		seen[n] = true
	}
}

func assertValidHerdrAgentName(t *testing.T, name string) {
	t.Helper()
	if name == "" || len(name) > herdrAgentNameMax {
		t.Errorf("agent 名の長さが不正: %q (%d)", name, len(name))
	}
	if c := name[0]; c < 'a' || c > 'z' {
		t.Errorf("agent 名は小文字始まりのはず: %q", name)
	}
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if !ok {
			t.Errorf("agent 名に使えない文字 %q: %q", r, name)
		}
	}
}

func TestHerdrPaneSplitArgsAndParse(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	calls := stubHerdrRun(t, []byte(`{"result":{"pane":{"pane_id":"w3:p9","cwd":"/repo"}}}`), nil)

	pane, err := herdrPaneSplit("/repo", "down", false)
	if err != nil {
		t.Fatalf("herdrPaneSplit: %v", err)
	}
	if pane.PaneID != "w3:p9" {
		t.Errorf("pane_id: got %q want w3:p9", pane.PaneID)
	}
	want := []string{"pane", "split", "--current", "--direction", "down", "--cwd", "/repo", "--no-focus"}
	if !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("args:\n got %v\nwant %v", (*calls)[0], want)
	}
}

func TestHerdrPaneSplitFocus(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	calls := stubHerdrRun(t, []byte(`{"result":{"pane":{"pane_id":"w3:p9"}}}`), nil)
	if _, err := herdrPaneSplit("", "", true); err != nil {
		t.Fatalf("herdrPaneSplit: %v", err)
	}
	// cwd/direction 空なら該当フラグを付けず、focus=true なら --focus。
	want := []string{"pane", "split", "--current", "--focus"}
	if !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("args: got %v want %v", (*calls)[0], want)
	}
}

// agent start は既存 pane を --pane で受け、実行ファイルは --kind で決まる (argv 先頭に入れない)。
func TestHerdrAgentStartArgsAndParse(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	const js = `{"result":{"agent":{"agent":"claude","agent_status":"working","name":"task-webapp-0001","pane_id":"w3:p9"},"type":"agent_started"}}`
	calls := stubHerdrRun(t, []byte(js), nil)

	agent, err := herdrAgentStart("task-webapp-0001", "claude", "w3:p9", 5000,
		[]string{"-n", "task 0001: X", "着手して"})
	if err != nil {
		t.Fatalf("herdrAgentStart: %v", err)
	}
	if agent.PaneID != "w3:p9" {
		t.Errorf("pane_id: got %q want w3:p9", agent.PaneID)
	}
	want := []string{"agent", "start", "task-webapp-0001", "--kind", "claude", "--pane", "w3:p9",
		"--timeout", "5000", "--", "-n", "task 0001: X", "着手して"}
	if !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("args:\n got %v\nwant %v", (*calls)[0], want)
	}
}

func TestHerdrAgentStartOmitsEmptyOptions(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	calls := stubHerdrRun(t, []byte(`{"result":{"agent":{"pane_id":"w3:p9"}}}`), nil)
	if _, err := herdrAgentStart("a1", "codex", "w3:p9", 0, nil); err != nil {
		t.Fatalf("herdrAgentStart: %v", err)
	}
	// timeout<=0 と引数なしでは --timeout / -- を付けない。
	want := []string{"agent", "start", "a1", "--kind", "codex", "--pane", "w3:p9"}
	if !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("args: got %v want %v", (*calls)[0], want)
	}
}

func TestHerdrAgentStartRequiresHerdr(t *testing.T) {
	t.Setenv("HERDR_ENV", "0")
	if _, err := herdrAgentStart("a1", "claude", "w3:p9", 0, nil); err == nil {
		t.Error("herdr 外では requireHerdr で止まるはず")
	}
}

func TestHerdrAgentStartRequiresNameKindPane(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	for _, tt := range []struct{ name, kind, pane string }{
		{"", "claude", "w3:p9"},
		{"a1", "", "w3:p9"},
		{"a1", "claude", ""},
	} {
		if _, err := herdrAgentStart(tt.name, tt.kind, tt.pane, 0, nil); err == nil {
			t.Errorf("name=%q kind=%q pane=%q はエラーにすべき", tt.name, tt.kind, tt.pane)
		}
	}
}

// 2 段起動: pane split → agent start の順に叩き、split で得た pane id を agent start に渡す。
func TestHerdrStartAgentInNewPaneTwoSteps(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	calls := stubHerdrRunWith(t, func(args []string) ([]byte, error) {
		if args[1] == "split" {
			return []byte(`{"result":{"pane":{"pane_id":"w3:pX"}}}`), nil
		}
		return []byte(`{"result":{"agent":{"pane_id":"w3:pX","agent_status":"working"}}}`), nil
	})

	agent, err := herdrStartAgentInNewPane("task-webapp-0007", "claude", "/repo", "down", false, 0, []string{"go"})
	if err != nil {
		t.Fatalf("herdrStartAgentInNewPane: %v", err)
	}
	if agent.PaneID != "w3:pX" {
		t.Errorf("pane_id = %q, want w3:pX", agent.PaneID)
	}
	if len(*calls) != 2 {
		t.Fatalf("herdr 呼び出し回数 = %d, want 2: %v", len(*calls), *calls)
	}
	if got := (*calls)[0][:3]; !reflect.DeepEqual(got, []string{"pane", "split", "--current"}) {
		t.Errorf("1 回目は pane split のはず: %v", (*calls)[0])
	}
	want := []string{"agent", "start", "task-webapp-0007", "--kind", "claude", "--pane", "w3:pX", "--", "go"}
	if !reflect.DeepEqual((*calls)[1], want) {
		t.Errorf("2 回目:\n got %v\nwant %v", (*calls)[1], want)
	}
}

// 名前衝突は同じ pane のまま代替名で試し直す (pane を作り直さない)。
func TestHerdrStartAgentInNewPaneRetriesTakenName(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	calls := stubHerdrRunWith(t, func(args []string) ([]byte, error) {
		if args[1] == "split" {
			return []byte(`{"result":{"pane":{"pane_id":"w3:pX"}}}`), nil
		}
		if args[2] == "task-webapp-0007" {
			return nil, &herdrCLIError{Sub: "agent start", Code: "agent_name_taken", Message: "taken", err: fmt.Errorf("exit 1")}
		}
		return []byte(`{"result":{"agent":{"pane_id":"w3:pX"}}}`), nil
	})

	if _, err := herdrStartAgentInNewPane("task-webapp-0007", "claude", "/repo", "down", false, 0, nil); err != nil {
		t.Fatalf("代替名で成功するはず: %v", err)
	}
	if len(*calls) != 3 {
		t.Fatalf("呼び出し回数 = %d, want 3 (split + start 2 回): %v", len(*calls), *calls)
	}
	if (*calls)[2][2] != "task-webapp-0007-2" {
		t.Errorf("代替名 = %q, want task-webapp-0007-2", (*calls)[2][2])
	}
	// pane は 1 つだけ (split をやり直していない)。
	if n := countCalls(*calls, "pane", "split"); n != 1 {
		t.Errorf("pane split 回数 = %d, want 1", n)
	}
}

// 起動前に確実に失敗したとき (名前不正) は、作った空 pane を閉じる。
func TestHerdrStartAgentInNewPaneClosesPaneOnPreLaunchFailure(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	calls := stubHerdrRunWith(t, func(args []string) ([]byte, error) {
		switch args[1] {
		case "split":
			return []byte(`{"result":{"pane":{"pane_id":"w3:pX"}}}`), nil
		case "start":
			return nil, &herdrCLIError{Sub: "agent start", Code: "invalid_agent_name", Message: "bad", err: fmt.Errorf("exit 1")}
		}
		return []byte(`{"result":{"type":"ok"}}`), nil
	})

	if _, err := herdrStartAgentInNewPane("x", "claude", "/repo", "down", false, 0, nil); err == nil {
		t.Fatal("エラーになるはず")
	}
	if n := countCalls(*calls, "pane", "close"); n != 1 {
		t.Errorf("pane close 回数 = %d, want 1: %v", n, *calls)
	}
}

// timeout は「agent が遅れて起動しているかもしれない」ので pane を閉じず、pane id を添えて返す。
func TestHerdrStartAgentInNewPaneKeepsPaneOnTimeout(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	calls := stubHerdrRunWith(t, func(args []string) ([]byte, error) {
		if args[1] == "split" {
			return []byte(`{"result":{"pane":{"pane_id":"w3:pX"}}}`), nil
		}
		return nil, &herdrCLIError{Sub: "agent start", Code: "timeout", Message: "timed out", err: fmt.Errorf("exit 1")}
	})

	_, err := herdrStartAgentInNewPane("x", "claude", "/repo", "down", false, 0, nil)
	if err == nil {
		t.Fatal("エラーになるはず")
	}
	if !strings.Contains(err.Error(), "w3:pX") {
		t.Errorf("残した pane id を伝えるはず: %v", err)
	}
	if n := countCalls(*calls, "pane", "close"); n != 0 {
		t.Errorf("timeout では pane を閉じないはず (close %d 回)", n)
	}
}

func countCalls(calls [][]string, sub ...string) int {
	n := 0
	for _, c := range calls {
		if len(c) >= len(sub) && reflect.DeepEqual(c[:len(sub)], sub) {
			n++
		}
	}
	return n
}

// split 直後の pane はまだシェル起動中で agent_pane_busy を返す。プロンプトに落ち着くまで待って
// 同じ pane で起動し直す (pane は作り直さない)。
func TestHerdrStartAgentInNewPaneWaitsForShell(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	swapPaneReadyPacing(t, time.Second, time.Millisecond)

	busy := 2
	calls := stubHerdrRunWith(t, func(args []string) ([]byte, error) {
		if args[1] == "split" {
			return []byte(`{"result":{"pane":{"pane_id":"w3:pX"}}}`), nil
		}
		if busy > 0 {
			busy--
			return nil, &herdrCLIError{Sub: "agent start", Code: "agent_pane_busy", Message: "not an available shell", err: fmt.Errorf("exit 1")}
		}
		return []byte(`{"result":{"agent":{"pane_id":"w3:pX"}}}`), nil
	})

	if _, err := herdrStartAgentInNewPane("x", "claude", "/repo", "down", false, 0, nil); err != nil {
		t.Fatalf("シェル起動待ちのあと成功するはず: %v", err)
	}
	if n := countCalls(*calls, "pane", "split"); n != 1 {
		t.Errorf("pane split 回数 = %d, want 1 (待つだけで作り直さない)", n)
	}
	// 名前は替えずに同じ第一候補で試し直す (agent_pane_busy は名前の問題ではない)。
	for _, c := range *calls {
		if c[1] == "start" && c[2] != "x" {
			t.Errorf("agent 名を替えてしまった: %v", c)
		}
	}
}

// シェルが待っても使えるようにならなければ、起動コマンドは一度も送っていないので pane を閉じる。
func TestHerdrStartAgentInNewPaneClosesPaneWhenShellNeverReady(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	swapPaneReadyPacing(t, 5*time.Millisecond, time.Millisecond)

	calls := stubHerdrRunWith(t, func(args []string) ([]byte, error) {
		switch args[1] {
		case "split":
			return []byte(`{"result":{"pane":{"pane_id":"w3:pX"}}}`), nil
		case "start":
			return nil, &herdrCLIError{Sub: "agent start", Code: "agent_pane_busy", Message: "not an available shell", err: fmt.Errorf("exit 1")}
		}
		return []byte(`{"result":{"type":"ok"}}`), nil
	})

	if _, err := herdrStartAgentInNewPane("x", "claude", "/repo", "down", false, 0, nil); err == nil {
		t.Fatal("エラーになるはず")
	}
	if n := countCalls(*calls, "pane", "close"); n != 1 {
		t.Errorf("pane close 回数 = %d, want 1: %v", n, *calls)
	}
}

func swapPaneReadyPacing(t *testing.T, timeout, interval time.Duration) {
	t.Helper()
	origT, origI := herdrPaneReadyTimeout, herdrPaneReadyInterval
	herdrPaneReadyTimeout, herdrPaneReadyInterval = timeout, interval
	t.Cleanup(func() { herdrPaneReadyTimeout, herdrPaneReadyInterval = origT, origI })
}
