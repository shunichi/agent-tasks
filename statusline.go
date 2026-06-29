package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

// statusline は Claude Code の status line (settings.json の statusLine, type: command) から
// 呼ばれ、stdin の JSON を読んで「この pane (セッション) が今どのタスクを実行中か」を 1 行で返す。
// list の SESSION 列 (0020/0027) が「俯瞰」(どの pane が waiting か) なのに対し、これは逆に
// その pane 自身に「自分が何をやっているか」を常時表示する。
//
// 現在タスクの特定は 2 経路 (resolveCurrentTask):
//   (1) cwd 経路: cwd の git root basename が <project>--<NNNN> 形式ならそこから直接 (worktree 内起動)。
//   (2) session_id 経路: 通常フローではセッションの cwd はメインリポなので、JSON の session_id を
//       session-link の <key>.link.json で逆引きしてタスクを引く (0027 の成果を再利用)。
// status line はパイプ出力 (stdout が非 TTY) だが端末に表示されるので、色は既定で出す
// (statusLineColors)。タスクを特定できなければ何も出力しない (空の status line)。

// statusLineInput は statusLine コマンドに stdin で渡される JSON のうち本コマンドが使う分だけ拾う。
type statusLineInput struct {
	Cwd       string `json:"cwd"`
	SessionID string `json:"session_id"`
	Workspace struct {
		CurrentDir string `json:"current_dir"`
	} `json:"workspace"`
}

// cmdStatusline は stdin の JSON を読み、現在タスクを 1 行表示する。
// status line を壊さない方針: 入力エラー/解析失敗は黙って空表示で抜ける (非ゼロ終了しない)。
func cmdStatusline(args []string) error {
	if slices.Contains(args, "--print-config") {
		fmt.Print(statusLineConfig())
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		return nil
	}
	var in statusLineInput
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &in) // 解析失敗でも空表示で続行
	}
	cwd := in.Cwd
	if cwd == "" {
		cwd = in.Workspace.CurrentDir
	}
	if line := statusLine(cwd, in.SessionID, statusLineColors()); line != "" {
		fmt.Println(line)
	}
	return nil
}

// statusLine は cwd / session_id から現在タスクを引き、1 行に整形する。無ければ空文字。
func statusLine(cwd, sessionID string, c colors) string {
	t, ok := resolveCurrentTask(cwd, sessionID)
	if !ok {
		return ""
	}
	return formatStatusLine(t, c)
}

// resolveCurrentTask は status line に渡された cwd / session_id から
// 「このセッションが実行中のタスク」を引く。cwd 経路 → session_id 経路の順に試す。
func resolveCurrentTask(cwd, sessionID string) (Task, bool) {
	// (1) cwd 経路: worktree 内で起動したセッション (cwd の git root が <project>--<NNNN>)。
	if key := worktreeKey(cwd); key != "" {
		if t, ok := lookupTaskByWorktreeKey(key); ok {
			return t, true
		}
	}
	// (2) session_id 経路: 通常フロー (cwd はメインリポ)。link を逆引きして worktree キーを得る。
	if sessionID != "" {
		if key, ok := worktreeKeyForSession(sessionID); ok {
			if t, ok := lookupTaskByWorktreeKey(key); ok {
				return t, true
			}
		}
	}
	return Task{}, false
}

// lookupTaskByWorktreeKey は worktree キー (<project>--<NNNN>) からストアのタスクを引く。
func lookupTaskByWorktreeKey(key string) (Task, bool) {
	project, id, ok := parseWorktreeKey(key)
	if !ok {
		return Task{}, false
	}
	path, err := resolveTaskPath(project, id)
	if err != nil {
		return Task{}, false
	}
	t, err := parseTask(path)
	if err != nil {
		return Task{}, false
	}
	return t, true
}

// parseWorktreeKey は worktree の basename "<project>--<NNNN>" を project と id に分ける。
// start が作る worktree 名 (../<project>--<NNNN>) の規約に従う。末尾の "--<数字>" だけを id とみなし、
// project 名自体に "--" が含まれても末尾分割で正しく取れる。数字でなければ ok=false (worktree でない)。
func parseWorktreeKey(key string) (project, id string, ok bool) {
	idx := strings.LastIndex(key, "--")
	if idx < 0 {
		return "", "", false
	}
	project, id = key[:idx], key[idx+2:]
	if project == "" || id == "" || !isAllDigits(id) {
		return "", "", false
	}
	return project, id, true
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// formatStatusLine は現在タスクを 1 行に整形する。
// 例: agent-tasks #0037 claude の表示を…カスタマイズ [in-progress]
func formatStatusLine(t Task, c colors) string {
	title := truncateDisp(t.Title, statusLineTitleWidth)
	return fmt.Sprintf("%s%s%s %s#%s%s %s %s[%s]%s",
		c.dim, t.Project, c.reset,
		c.bold, t.ID, c.reset,
		title,
		c.status(t.Status), t.Status, c.reset)
}

// statusLineTitleWidth は title の最大表示幅 (列)。status line は他情報と並ぶので長すぎない値に。
const statusLineTitleWidth = 40

// statusLineColors は status line 用の色を返す。status line は端末に表示されるので
// stdout が TTY でなくても既定で色を出す (auto)。NO_COLOR / --color never は尊重する。
func statusLineColors() colors {
	switch colorMode {
	case "never":
		return colors{}
	case "always":
		return palette()
	}
	if v, ok := os.LookupEnv("NO_COLOR"); ok && v != "" {
		return colors{}
	}
	return palette()
}

// statusLineConfig は ~/.claude/settings.json に貼る statusLine スニペットを返す。
func statusLineConfig() string {
	return `# ~/.claude/settings.json に設定してください (agent-tasks が PATH にある前提):
{
  "statusLine": {
    "type": "command",
    "command": "agent-tasks statusline"
  }
}
`
}
