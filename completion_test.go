package main

import (
	"os/exec"
	"strings"
	"testing"
)

// 生成スクリプトには全サブコマンド名が含まれること (一覧が欠けると補完されない)。
func TestCompletionScriptsListAllSubcommands(t *testing.T) {
	bash := bashCompletionScript()
	zsh := zshCompletionScript()
	for _, s := range completionSubcommands {
		if !strings.Contains(bash, s.name) {
			t.Errorf("bash 補完にサブコマンド %q が無い", s.name)
		}
		if !strings.Contains(zsh, s.name) {
			t.Errorf("zsh 補完にサブコマンド %q が無い", s.name)
		}
	}
}

// 列挙できるフラグ値 (status/color) が両スクリプトに含まれること。
func TestCompletionScriptsContainEnumValues(t *testing.T) {
	bash := bashCompletionScript()
	zsh := zshCompletionScript()
	for _, v := range completionStatusValues {
		if !strings.Contains(bash, v) {
			t.Errorf("bash 補完に status 値 %q が無い", v)
		}
		if !strings.Contains(zsh, v) {
			t.Errorf("zsh 補完に status 値 %q が無い", v)
		}
	}
	for _, v := range completionColorValues {
		if !strings.Contains(bash, v) {
			t.Errorf("bash 補完に color 値 %q が無い", v)
		}
		if !strings.Contains(zsh, v) {
			t.Errorf("zsh 補完に color 値 %q が無い", v)
		}
	}
}

// cmdCompletion の引数検証 (shell 必須・未知 shell・余分な引数)。
func TestCmdCompletionArgs(t *testing.T) {
	if err := cmdCompletion(nil); err == nil {
		t.Error("shell 未指定はエラーになるべき")
	}
	if err := cmdCompletion([]string{"fish"}); err == nil {
		t.Error("未知の shell はエラーになるべき")
	}
	if err := cmdCompletion([]string{"bash", "extra"}); err == nil {
		t.Error("余分な引数はエラーになるべき")
	}
}

// bash / zsh があれば、生成スクリプトの構文 (-n) を検証する。無ければスキップ。
func TestCompletionScriptsSyntax(t *testing.T) {
	cases := []struct {
		shell  string
		script string
	}{
		{"bash", bashCompletionScript()},
		{"zsh", zshCompletionScript()},
	}
	for _, c := range cases {
		t.Run(c.shell, func(t *testing.T) {
			if _, err := exec.LookPath(c.shell); err != nil {
				t.Skipf("%s が無いのでスキップ", c.shell)
			}
			cmd := exec.Command(c.shell, "-n")
			cmd.Stdin = strings.NewReader(c.script)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s -n が失敗: %v\n%s", c.shell, err, out)
			}
		})
	}
}
