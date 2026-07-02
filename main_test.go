package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain はテストを herdr の内外どちらで実行しても決定的にする。テストプロセスが herdr 内
// (HERDR_ENV=1) で走ると taskSessionState 等が実 herdr を叩いて非決定的になるため、既定を
// 「herdr 外」(旧マーカー経路) にリセットする。herdr 経路を検証するテストは t.Setenv で opt-in する。
func TestMain(m *testing.M) {
	os.Unsetenv("HERDR_ENV")
	os.Unsetenv("HERDR_PANE_ID")
	os.Unsetenv("HERDR_SOCKET_PATH")
	os.Unsetenv("HERDR_WORKSPACE_ID")
	os.Unsetenv("HERDR_TAB_ID")
	os.Unsetenv("CLAUDE_CODE_SESSION_ID")
	os.Exit(m.Run())
}

// usage が io.Writer を受けること (バッファに書ける) を担保する。*os.File 固定だと
// この呼び出し自体がコンパイルできない。
func TestUsageWritesToWriter(t *testing.T) {
	var b bytes.Buffer
	usage(&b)
	if b.Len() == 0 {
		t.Fatal("usage が何も書き込んでいない")
	}
	if !strings.Contains(b.String(), "agent-tasks") {
		t.Errorf("usage 出力に 'agent-tasks' が無い:\n%s", b.String())
	}
}

// currentProject が mainRepoOf に委譲され、その root の basename になっていることを担保する
// (git root ロジックの重複統合の回帰テスト)。
func TestCurrentProjectDelegatesToMainRepoOf(t *testing.T) {
	root, err := mainRepoOf(".")
	if err != nil {
		t.Skip("git リポジトリ外のためスキップ")
	}
	if got, want := currentProject(), filepath.Base(root); got != want {
		t.Errorf("currentProject() = %q, want %q (mainRepoOf(\".\") の basename)", got, want)
	}
}
