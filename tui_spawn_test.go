package main

import "testing"

// spawnAction は S キー (選択タスクを別 pane で spawn) の事前条件を判定する。
func TestSpawnAction(t *testing.T) {
	tests := []struct {
		name    string
		task    Task
		current string
		want    bool
	}{
		{"todo は spawn 可", Task{Project: "webapp", Status: "todo"}, "webapp", true},
		{"blocked も可", Task{Project: "webapp", Status: "blocked"}, "webapp", true},
		{"review も可", Task{Project: "webapp", Status: "review"}, "webapp", true},
		{"in-progress も可 (二重着手ガードは spawnTask 側)", Task{Project: "webapp", Status: "in-progress"}, "webapp", true},
		{"done は不可", Task{Project: "webapp", Status: "done"}, "webapp", false},
		{"別 project は不可", Task{Project: "other", Status: "todo"}, "webapp", false},
		{"現在 project 不明は不可", Task{Project: "webapp", Status: "todo"}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proceed, msg := spawnAction(tt.task, tt.current)
			if proceed != tt.want {
				t.Errorf("proceed = %v, want %v", proceed, tt.want)
			}
			if !proceed && msg == "" {
				t.Error("中止時は理由 (msg) を返すべき")
			}
			if proceed && msg != "" {
				t.Errorf("続行時に msg が出た: %q", msg)
			}
		})
	}
}

// spawnTask は herdr 外なら requireHerdr で止まり、herdr を叩かない。
func TestSpawnTaskRequiresHerdr(t *testing.T) {
	t.Setenv("HERDR_ENV", "0")
	calls := stubHerdrRun(t, nil, nil)
	if _, err := spawnTask(Task{ID: "0001", Project: "webapp", Status: "todo"}, "down", false, false); err == nil {
		t.Error("herdr 外では止まるはず")
	}
	if len(*calls) != 0 {
		t.Errorf("herdr 外で herdr を叩いた: %v", *calls)
	}
}

// 二重着手ガード: in-progress + session あり + force=false はエラーで、herdr を叩かない。
func TestSpawnTaskDoubleStartGuard(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	calls := stubHerdrRun(t, nil, nil)
	task := Task{ID: "0001", Project: "webapp", Status: "in-progress", Session: "https://claude.ai/code/session_x"}
	if _, err := spawnTask(task, "down", false, false); err == nil {
		t.Error("in-progress + session + force=false はガードで止まるはず")
	}
	if len(*calls) != 0 {
		t.Errorf("ガードで止まる前に herdr を叩いた: %v", *calls)
	}
	// --force なら通す (herdr が呼ばれる)。
	stubSpawnHerdr(t, "w1:p2")
	if _, err := spawnTask(task, "down", false, true); err != nil {
		t.Errorf("--force では通すはず: %v", err)
	}
}

// stubSpawnHerdr は spawn の 2 段起動 (pane split → agent start) に応える最小スタブ。
func stubSpawnHerdr(t *testing.T, paneID string) *[][]string {
	t.Helper()
	return stubHerdrRunWith(t, func(args []string) ([]byte, error) {
		if args[1] == "split" {
			return []byte(`{"result":{"pane":{"pane_id":"` + paneID + `"}}}`), nil
		}
		return []byte(`{"result":{"agent":{"pane_id":"` + paneID + `"}}}`), nil
	})
}

// 成功パス: spawnTask は背面に pane を割ってから、その pane で agent をスラッグ名で起動する。
func TestSpawnTaskInvokesHerdr(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	t.Setenv("AGENT_TASKS_AGENT", "claude")
	calls := stubSpawnHerdr(t, "w1:p3")

	task := Task{ID: "0007", Project: "webapp", Title: "サンプル", Status: "todo"}
	pane, err := spawnTask(task, "down", false, false)
	if err != nil {
		t.Fatalf("spawnTask: %v", err)
	}
	if pane.PaneID != "w1:p3" {
		t.Errorf("pane_id = %q, want w1:p3", pane.PaneID)
	}
	if len(*calls) != 2 {
		t.Fatalf("herdr 呼び出し回数 = %d, want 2 (split + start): %v", len(*calls), *calls)
	}
	split, start := (*calls)[0], (*calls)[1]
	// pane は下方向・背面 (親のフォーカスを奪わない)。
	if !containsPair(split, "--direction", "down") || !containsArg(split, "--no-focus") {
		t.Errorf("pane split が想定と違う: %v", split)
	}
	// agent 名は herdr のスラッグ制約に合わせた別物で、人向けラベルは -n 側に載る。
	if len(start) < 3 || start[0] != "agent" || start[1] != "start" || start[2] != "task-webapp-0007" {
		t.Errorf("agent start の先頭引数が想定と違う: %v", start)
	}
	if !containsPair(start, "--kind", "claude") || !containsPair(start, "--pane", "w1:p3") {
		t.Errorf("--kind / --pane が想定と違う: %v", start)
	}
	if !containsPair(start, "-n", "task 0007: サンプル") {
		t.Errorf("表示ラベルが -n で渡っていない: %v", start)
	}
}

// ヘルプに S (spawn) の項目がある。
func TestHelpHasSpawnKey(t *testing.T) {
	for _, e := range helpEntries() {
		if e[0] == "S" {
			return
		}
	}
	t.Error("helpEntries に S (spawn) が無い")
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func containsPair(args []string, flag, val string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == val {
			return true
		}
	}
	return false
}
