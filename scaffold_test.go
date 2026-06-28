package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAvailableStacks(t *testing.T) {
	stacks, err := availableStacks()
	if err != nil {
		t.Fatal(err)
	}
	// 同梱テンプレに firebase / rails があること。
	for _, want := range []string{"firebase", "rails"} {
		if !slices.Contains(stacks, want) {
			t.Errorf("stacks %v に %q が無い", stacks, want)
		}
	}
}

func TestDetectStack(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		want  string
	}{
		{"firebase.json", []string{"firebase.json"}, "firebase"},
		{".firebaserc", []string{".firebaserc"}, "firebase"},
		{"bin/rails", []string{"bin/rails"}, "rails"},
		{"config/environment.rb", []string{"config/environment.rb"}, "rails"},
		{"判定不能", []string{"README.md"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				p := filepath.Join(dir, f)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := detectStack(dir); got != tc.want {
				t.Errorf("detectStack = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestScaffoldInto(t *testing.T) {
	dir := t.TempDir()
	written, skipped, err := scaffoldInto("firebase", dir, false)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(written)
	want := []string{".worktree-post-create", ".worktreeinclude"}
	if !slices.Equal(written, want) {
		t.Fatalf("written = %v, want %v", written, want)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none", skipped)
	}

	// post-create に実行ビットが立っていること。
	fi, err := os.Stat(filepath.Join(dir, ".worktree-post-create"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o100 == 0 {
		t.Errorf(".worktree-post-create に実行ビットが無い: %v", fi.Mode())
	}
	// 中身が同梱テンプレ由来であること (firebase の目印)。
	b, _ := os.ReadFile(filepath.Join(dir, ".worktree-post-create"))
	if !strings.Contains(string(b), "EMULATOR_PORT_OFFSET") {
		t.Errorf("firebase post-create の内容が想定と違う:\n%s", b)
	}
}

func TestScaffoldIntoSkipAndForce(t *testing.T) {
	dir := t.TempDir()
	// 既存ファイルを置く。
	existing := filepath.Join(dir, ".worktreeinclude")
	if err := os.WriteFile(existing, []byte("KEEP ME"), 0o644); err != nil {
		t.Fatal(err)
	}

	// force なし → .worktreeinclude はスキップ、.worktree-post-create は書かれる。
	written, skipped, err := scaffoldInto("rails", dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(skipped, ".worktreeinclude") {
		t.Errorf("既存 .worktreeinclude はスキップされるべき: skipped=%v", skipped)
	}
	if !slices.Contains(written, ".worktree-post-create") {
		t.Errorf(".worktree-post-create は書かれるべき: written=%v", written)
	}
	if b, _ := os.ReadFile(existing); string(b) != "KEEP ME" {
		t.Errorf("既存ファイルが上書きされた: %q", b)
	}

	// force → 上書きされる。
	written, _, err = scaffoldInto("rails", dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(written, ".worktreeinclude") {
		t.Errorf("--force で .worktreeinclude を上書きするべき: written=%v", written)
	}
	if b, _ := os.ReadFile(existing); string(b) == "KEEP ME" {
		t.Error("--force なのに上書きされていない")
	}
}
