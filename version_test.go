package main

import (
	"strings"
	"testing"
)

func TestFormatVersion(t *testing.T) {
	cases := []struct {
		name string
		in   vcsInfo
		want []string // 含まれるべき部分文字列
		eq   string   // 完全一致を見たい場合 (空なら want のみ)
	}{
		{
			name: "clean with calver",
			in:   vcsInfo{revision: "904ff2b7cca86527c4f3c9226cab6db667acf2b9", time: "2026-06-29T16:46:24+09:00", modified: false},
			want: []string{"agent-tasks 2026.06.29+g904ff2b7cca8", "built from 904ff2b7cca8", "2026-06-29T16:46:24+09:00", "clean"},
		},
		{
			name: "dirty",
			in:   vcsInfo{revision: "abc123def456789", time: "2026-01-02T03:04:05Z", modified: true},
			want: []string{"2026.01.02+gabc123def456", "dirty"},
		},
		{
			name: "no time falls back",
			in:   vcsInfo{revision: "abc123def456789", time: "", modified: false},
			want: []string{"0.0.0+gabc123def456", "clean"},
		},
		{
			name: "no vcs is devel",
			in:   vcsInfo{},
			eq:   "agent-tasks (devel)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := formatVersion(c.in)
			if c.eq != "" && got != c.eq {
				t.Fatalf("formatVersion = %q, want %q", got, c.eq)
			}
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("formatVersion = %q, missing %q", got, w)
				}
			}
		})
	}
}

func TestFormatVersionShortenSHA(t *testing.T) {
	got := formatVersion(vcsInfo{revision: strings.Repeat("a", 40), time: "2026-06-29T00:00:00Z"})
	// SHA は shortSHALen 桁に丸める。
	if !strings.Contains(got, "+g"+strings.Repeat("a", shortSHALen)+" ") {
		t.Errorf("SHA が %d 桁に丸められていない: %q", shortSHALen, got)
	}
	if strings.Contains(got, strings.Repeat("a", shortSHALen+1)) {
		t.Errorf("SHA が長すぎる: %q", got)
	}
}

func TestCmdVersionRejectsArgs(t *testing.T) {
	if err := cmdVersion([]string{"extra"}); err == nil {
		t.Error("version に余分な引数はエラーになるべき")
	}
}
