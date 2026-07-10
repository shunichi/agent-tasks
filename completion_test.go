package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 生成スクリプトには全サブコマンド名が含まれること (一覧が欠けると補完されない)。
func TestCompletionScriptsListAllSubcommands(t *testing.T) {
	bash := bashCompletionScript()
	zsh := zshCompletionScript()
	for _, s := range completionSubcommands() {
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

// 動的補完を呼び出すため、生成スクリプトに completion-values への参照があること。
func TestCompletionScriptsReferenceDynamicValues(t *testing.T) {
	for _, c := range []struct{ name, script string }{
		{"bash", bashCompletionScript()},
		{"zsh", zshCompletionScript()},
	} {
		if !strings.Contains(c.script, "completion-values projects") {
			t.Errorf("%s 補完が completion-values projects を呼んでいない", c.name)
		}
		if !strings.Contains(c.script, "completion-values ids") {
			t.Errorf("%s 補完が completion-values ids を呼んでいない", c.name)
		}
	}
}

// zsh の位置引数補完は C 言語形式の for (( )) を使わない (補完文脈で表示を壊し、
// "i=2" のようなゴミが入力に混じる回帰があった)。foreach 形式を使うこと。
func TestZshScriptAvoidsCStyleForInPositional(t *testing.T) {
	zsh := zshCompletionScript()
	if strings.Contains(zsh, "for (( i = 3") {
		t.Error("zsh の位置引数補完に C 言語形式の for (( i = 3 ...) が残っている (表示が壊れる)")
	}
	if !strings.Contains(zsh, "for w in ${words[3,CURRENT-1]}") {
		t.Error("zsh の位置引数補完が foreach (for w in ...) になっていない")
	}
}

// zsh はサブコマンド無しでも値を取る大域フラグ (--project 等) の直後で値を補完できること。
// (回帰: 以前は no-sub 経路で _describe にフォールバックし project 値が出なかった。)
func TestZshScriptCompletesTopLevelFlagValues(t *testing.T) {
	zsh := zshCompletionScript()
	for _, want := range []string{
		"--project|--projects) _agent_tasks_projects",
		"case ${words[CURRENT-1]} in",
	} {
		if !strings.Contains(zsh, want) {
			t.Errorf("zsh 補完に大域フラグ値の処理 %q が無い", want)
		}
	}
}

// printProjects はストア配下のディレクトリ名を昇順で出し、隠しディレクトリ (.git) を除く。
func TestPrintProjects(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"webapp", "api", ".git"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// ファイル (非ディレクトリ) は project ではないので無視されること。
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_TASKS_STORE", dir)
	var buf bytes.Buffer
	printProjects(&buf)
	if got, want := buf.String(), "api\nwebapp\n"; got != want {
		t.Errorf("printProjects = %q, want %q", got, want)
	}
}

// printTaskIDs は project 配下の <NNNN>-*.md の id を昇順で出し、.md 以外は無視する。
func TestPrintTaskIDs(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "webapp")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0003-c.md", "0001-a.md", "0002-b.md", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(proj, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AGENT_TASKS_STORE", dir)
	var buf bytes.Buffer
	printTaskIDs(&buf, proj)
	if got, want := buf.String(), "0001\n0002\n0003\n"; got != want {
		t.Errorf("printTaskIDs = %q, want %q", got, want)
	}
	// 存在しないディレクトリは静かに空 (補完を壊さない)。
	var empty bytes.Buffer
	printTaskIDs(&empty, filepath.Join(dir, "nope"))
	if empty.Len() != 0 {
		t.Errorf("存在しない project は空であるべき: %q", empty.String())
	}
}

// printTaskIDsWithTitle は "<id>\t<title>" 形式で id 昇順に出す (frontmatter の title を読む)。
func TestPrintTaskIDsWithTitle(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "webapp")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(proj, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("0002-b.md", "---\nid: \"0002\"\ntitle: 二番目\n---\n")
	write("0001-a.md", "---\nid: \"0001\"\ntitle: 最初のタスク\n---\n")
	t.Setenv("AGENT_TASKS_STORE", dir)
	var buf bytes.Buffer
	printTaskIDsWithTitle(&buf, proj)
	if got, want := buf.String(), "0001\t最初のタスク\n0002\t二番目\n"; got != want {
		t.Errorf("printTaskIDsWithTitle = %q, want %q", got, want)
	}
}

// 生成スクリプトが新しい位置引数補完を呼ぶこと: bash は project 名+id、zsh は --with-title。
func TestCompletionScriptsReferencePositionalValues(t *testing.T) {
	bash := bashCompletionScript()
	if !strings.Contains(bash, "completion-values ids --project") {
		t.Error("bash 補完が project 指定の id 補完 (第2引数) を呼んでいない")
	}
	zsh := zshCompletionScript()
	if !strings.Contains(zsh, "--with-title") {
		t.Error("zsh 補完が --with-title (タイトル付き id) を使っていない")
	}
}

// cmdCompletionValues の引数検証 (kind 必須・未知 kind・余分な引数)。
func TestCmdCompletionValuesArgs(t *testing.T) {
	if err := cmdCompletionValues(nil); err == nil {
		t.Error("kind 未指定はエラーになるべき")
	}
	if err := cmdCompletionValues([]string{"bogus"}); err == nil {
		t.Error("未知の kind はエラーになるべき")
	}
	if err := cmdCompletionValues([]string{"projects", "extra"}); err == nil {
		t.Error("projects に余分な引数はエラーになるべき")
	}
	if err := cmdCompletionValues([]string{"ids", "--bogus"}); err == nil {
		t.Error("ids に未知の引数はエラーになるべき")
	}
	if err := cmdCompletionValues([]string{"ids", "--project"}); err == nil {
		t.Error("--project の値欠落はエラーになるべき")
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
