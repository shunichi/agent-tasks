package main

import (
	"strings"
	"testing"
)

// レジストリの整合: 名前が一意、run が非 nil、commandByName が全件を張ること。
func TestCommandRegistryIntegrity(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range commands {
		if c.name == "" {
			t.Error("空の name を持つコマンドがある")
		}
		if seen[c.name] {
			t.Errorf("コマンド名が重複している: %q", c.name)
		}
		seen[c.name] = true
		if c.run == nil {
			t.Errorf("コマンド %q の run が nil", c.name)
		}
		if commandByName[c.name] == nil {
			t.Errorf("commandByName に %q が無い (dispatch できない)", c.name)
		}
	}
	if len(commandByName) != len(commands) {
		t.Errorf("commandByName の件数 %d が commands の件数 %d と一致しない", len(commandByName), len(commands))
	}
}

// visible な全コマンドが usage() (ヘルプ本文) に載ること。
// レジストリにコマンドを足したのにヘルプへ書き忘れた、を検出する。
func TestUsageListsAllVisibleCommands(t *testing.T) {
	var b strings.Builder
	usage(&b)
	help := b.String()
	for _, c := range commands {
		if c.hidden {
			continue
		}
		if !strings.Contains(help, c.name) {
			t.Errorf("usage() に visible コマンド %q が載っていない", c.name)
		}
	}
}

// bash 補完がレジストリの各コマンドの固有フラグを全て含むこと。
// bash 補完は本表から生成しているので、生成が表と一致していることの保証になり、
// 「コマンドにフラグを足したが補完に出ない」ドリフトを CI で検出する。
func TestBashCompletionContainsRegistryFlags(t *testing.T) {
	bash := bashCompletionScript()
	for _, c := range commands {
		if c.hidden {
			continue
		}
		for _, f := range c.flags {
			if !strings.Contains(bash, f) {
				t.Errorf("bash 補完にコマンド %q のフラグ %q が無い", c.name, f)
			}
		}
	}
}
