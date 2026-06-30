package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

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
