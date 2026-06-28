package main

import (
	"slices"
	"testing"
)

func TestExtractColorFlag(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantMode string
		wantRest []string
		wantErr  bool
	}{
		{"なし", []string{"show", "5"}, "auto", []string{"show", "5"}, false},
		{"=形式", []string{"--color=always", "list"}, "always", []string{"list"}, false},
		{"空白形式", []string{"list", "--color", "never"}, "never", []string{"list"}, false},
		{"先頭にあってもコマンドを残す", []string{"--color=never", "show", "5"}, "never", []string{"show", "5"}, false},
		{"値なし", []string{"--color"}, "", nil, true},
		{"不正な値", []string{"--color=bogus"}, "", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, rest, err := extractColorFlag(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if mode != tt.wantMode {
				t.Errorf("mode = %q, want %q", mode, tt.wantMode)
			}
			if !slices.Equal(rest, tt.wantRest) {
				t.Errorf("rest = %v, want %v", rest, tt.wantRest)
			}
		})
	}
}

func TestColorEnabled(t *testing.T) {
	// テスト中の stdout は端末でないので auto の TTY 判定は false になる。
	tests := []struct {
		name     string
		mode     string
		noColor  string // "" は未設定
		forceCol string
		want     bool
	}{
		{"always は env を無視して true", "always", "1", "", true},
		{"never は env を無視して false", "never", "", "1", false},
		{"auto + NO_COLOR で false", "auto", "1", "", false},
		{"auto + FORCE_COLOR で true", "auto", "", "1", true},
		{"NO_COLOR が FORCE_COLOR より優先", "auto", "1", "1", false},
		{"auto + 空の NO_COLOR は効かず FORCE_COLOR で true", "auto", "", "1", true},
		{"auto + env なしは TTY 判定 (テストでは false)", "auto", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.noColor != "" {
				t.Setenv("NO_COLOR", tt.noColor)
			} else {
				t.Setenv("NO_COLOR", "")
			}
			if tt.forceCol != "" {
				t.Setenv("FORCE_COLOR", tt.forceCol)
			} else {
				t.Setenv("FORCE_COLOR", "")
			}
			if got := colorEnabled(tt.mode); got != tt.want {
				t.Errorf("colorEnabled(%q) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}
