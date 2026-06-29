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
	// TTY 判定は isStdoutTTY を差し替えて決定的にする (実 stdout の状態に依存させない。
	// ターミナルで直接 go test しても CI/パイプ経由でも同じ結果になるように)。各ケースの
	// tty フィールドを isStdoutTTY の戻り値として与え、auto が env 無しのとき TTY 判定へ
	// 落ちることを true/false 両方で検証する。
	tests := []struct {
		name     string
		mode     string
		noColor  string // "" は未設定
		forceCol string
		tty      bool // isStdoutTTY の戻り値 (auto + env 無しのときだけ効く)
		want     bool
	}{
		{"always は env/TTY を無視して true", "always", "1", "", false, true},
		{"never は env/TTY を無視して false", "never", "", "1", true, false},
		{"auto + NO_COLOR で false", "auto", "1", "", true, false},
		{"auto + FORCE_COLOR で true", "auto", "", "1", false, true},
		{"NO_COLOR が FORCE_COLOR より優先", "auto", "1", "1", true, false},
		{"auto + 空の NO_COLOR は効かず FORCE_COLOR で true", "auto", "", "1", false, true},
		{"auto + env なし + TTY なら true", "auto", "", "", true, true},
		{"auto + env なし + 非 TTY なら false", "auto", "", "", false, false},
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
			orig := isStdoutTTY
			isStdoutTTY = func() bool { return tt.tty }
			defer func() { isStdoutTTY = orig }()
			if got := colorEnabled(tt.mode); got != tt.want {
				t.Errorf("colorEnabled(%q) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}
