package main

import "testing"

func TestSessionRenameName(t *testing.T) {
	got := sessionRenameName(Task{ID: "0089", Title: "セッション名をタスク名に変える"})
	want := "task 0089: セッション名をタスク名に変える"
	if got != want {
		t.Errorf("sessionRenameName = %q, want %q", got, want)
	}
	// title が空でも形式は保つ。
	if got := sessionRenameName(Task{ID: "0001", Title: ""}); got != "task 0001: " {
		t.Errorf("空 title: got %q", got)
	}
}
