package main

import (
	"slices"
	"testing"
)

// argScan の基本: フラグ/値/位置引数の振り分けと `--` 終端。
func TestArgScanTerminator(t *testing.T) {
	// `--project p -- --foo bar`: `--` 以降 (--foo bar) は位置引数になる。
	s := newArgScan([]string{"--project", "p", "--", "--foo", "bar"})
	var project string
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch a {
		case "--project":
			v, err := s.value("--project")
			if err != nil {
				t.Fatalf("value: %v", err)
			}
			project = v
		default:
			t.Fatalf("`--` 以降がフラグとして返った: %q", a)
		}
	}
	if project != "p" {
		t.Errorf("project = %q, want p", project)
	}
	if got := s.rest(); !slices.Equal(got, []string{"--foo", "bar"}) {
		t.Errorf("rest = %v, want [--foo bar]", got)
	}
}

func TestArgScanValueMissing(t *testing.T) {
	s := newArgScan([]string{"--project"})
	a, _ := s.token()
	if a != "--project" {
		t.Fatalf("token = %q", a)
	}
	if _, err := s.value("--project"); err == nil {
		t.Error("値欠落でエラーになっていない")
	}
}

func TestArgScanPositionalCollect(t *testing.T) {
	s := newArgScan([]string{"webapp", "0005"})
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		s.positional(a)
	}
	if got := s.rest(); !slices.Equal(got, []string{"webapp", "0005"}) {
		t.Errorf("rest = %v, want [webapp 0005]", got)
	}
}

// peek/skip: 「次が条件を満たすときだけ取る」任意引数。
func TestArgScanPeekSkip(t *testing.T) {
	s := newArgScan([]string{"--recent", "7"})
	a, _ := s.token()
	if a != "--recent" {
		t.Fatalf("token = %q", a)
	}
	if v, ok := s.peek(); !ok || v != "7" {
		t.Fatalf("peek = %q,%v", v, ok)
	}
	s.skip()
	if _, ok := s.token(); ok {
		t.Error("skip 後にまだトークンが残っている")
	}
}

// extractColorFlag は `--` 以降を (終端マーカーごと) そのまま rest へ素通しし、
// `--` の後ろにある --color は奪わない。
func TestExtractColorFlagTerminator(t *testing.T) {
	mode, rest, err := extractColorFlag([]string{"show", "--", "--color", "always"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if mode != "auto" {
		t.Errorf("mode = %q, want auto (`--` 以降の --color は奪わない)", mode)
	}
	if !slices.Equal(rest, []string{"show", "--", "--color", "always"}) {
		t.Errorf("rest = %v, want [show -- --color always]", rest)
	}
}

// cmdList: `--` 以降は位置引数とみなされ、list は位置引数を取らないので拒否する。
func TestCmdListRejectsPositionalAfterTerminator(t *testing.T) {
	if err := cmdList([]string{"--", "foo"}); err == nil {
		t.Error("`-- foo` を受理してしまった (unexpected argument を期待)")
	}
}
