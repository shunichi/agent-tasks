package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// session の状態は Claude Code の hook から書き込まれるマーカーで管理する。
// hook が `agent-tasks session-hook` を呼ぶと、stdin の JSON から状態を判定して
// マーカーを書く。list がそれを読んで in-progress のセッションが「処理中 (working) か
// 入力待ち (waiting) か」を表示する。
//
// マーカーの突合キーは **worktree** (= hook の cwd の git root basename、例 agent-tasks--0020)。
// hook が渡す session_id (ローカル CLI の UUID) は frontmatter の session URL
// (claude.ai の session_<id>) と一致しないため使えない。start/spawn が作る worktree
// `../<project>--<NNNN>` は 1 タスク = 1 worktree = 1 セッションなので、これで確実に突合できる。
//
// マーカーはマシンローカルな揮発情報なので、git 同期されるストアの外に置く。

// セッション状態の値。
const (
	sessWorking = "working" // 処理中 (入力を受け取って動いている)
	sessWaiting = "waiting" // ユーザーの入力/許可待ちで止まっている
	sessEnded   = "ended"   // セッション終了 (タスクが in-progress のままなら未 done のサイン)
)

// sessionState はマーカーファイルの中身。
type sessionState struct {
	State   string `json:"state"`
	Updated string `json:"updated"`       // RFC3339
	Cwd     string `json:"cwd,omitempty"` // 参考: hook 実行時の cwd
}

// sessionStateDir はマーカーの置き場を返す。ストアとは別 (マシンローカル)。
// AGENT_TASKS_STATE_DIR > $XDG_STATE_HOME/agent-tasks/sessions > ~/.local/state/agent-tasks/sessions。
func sessionStateDir() string {
	if v := os.Getenv("AGENT_TASKS_STATE_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "agent-tasks", "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("agent-tasks-state", "sessions")
	}
	return filepath.Join(home, ".local", "state", "agent-tasks", "sessions")
}

// worktreeKey は dir が属する git 作業ツリーの root basename を返す (例 agent-tasks--0020)。
// dir が空ならプロセスの cwd を使う。git 管理外なら "" (= 突合キーにできない)。
// リンク worktree 内では show-toplevel がその worktree 自身の root を返すので、
// メイン repo ではなく `<project>--<NNNN>` になり、frontmatter の worktree と一致する。
func worktreeKey(dir string) string {
	if dir == "" {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
		}
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	top := strings.TrimSpace(string(out))
	if top == "" {
		return ""
	}
	return filepath.Base(top)
}

// taskSessionKey は task からマーカー突合キーを返す。worktree (frontmatter) の basename。
// worktree 未記録なら "" (突合不能)。
func taskSessionKey(t Task) string {
	if t.Worktree == "" {
		return ""
	}
	return filepath.Base(t.Worktree)
}

// sessionStateFor は hook イベント名 (と通知種別) から遷移後の状態を返す。
// 状態に影響しないイベントは "" を返す (マーカーを書かない)。
func sessionStateFor(event, notifType string) string {
	switch event {
	case "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolBatch", "SessionStart", "SubagentStart":
		return sessWorking
	case "Stop":
		return sessWaiting
	case "Notification":
		// 権限プロンプト/アイドル待ちだけを「入力待ち」とみなす。
		// auth_success や MCP elicitation 等は状態に影響させない。
		switch notifType {
		case "permission_prompt", "idle_prompt":
			return sessWaiting
		}
		return ""
	case "SessionEnd", "StopFailure":
		return sessEnded
	}
	return ""
}

// writeSessionState は突合キー key のマーカーを書く。
func writeSessionState(key, state, cwd string, now time.Time) error {
	if key == "" {
		return fmt.Errorf("session key が空")
	}
	if strings.ContainsAny(key, `/\`) {
		return fmt.Errorf("不正な session key: %q", key)
	}
	dir := sessionStateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(sessionState{State: state, Updated: now.Format(time.RFC3339), Cwd: cwd})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, key+".json"), data, 0o644)
}

// readSessionState は突合キー key のマーカーを読む。無ければ ok=false。
func readSessionState(key string) (sessionState, bool) {
	if key == "" || strings.ContainsAny(key, `/\`) {
		return sessionState{}, false
	}
	data, err := os.ReadFile(filepath.Join(sessionStateDir(), key+".json"))
	if err != nil {
		return sessionState{}, false
	}
	var m sessionState
	if err := json.Unmarshal(data, &m); err != nil || m.State == "" {
		return sessionState{}, false
	}
	return m, true
}

// sessionCell は list の SESSION 列のセルを返す。in-progress 以外は空。
// マーカーが無い (hook 未導入 or worktree 不明) ときは "?"。
func sessionCell(t Task, c colors) cell {
	if t.Status != "in-progress" {
		return cell{"", ""}
	}
	st, ok := readSessionState(taskSessionKey(t))
	if !ok {
		return cell{"?", c.dim}
	}
	switch st.State {
	case sessWaiting:
		return cell{"waiting", c.review} // 入力待ち = 要対応。目立たせる
	case sessWorking:
		return cell{"working", c.prog}
	case sessEnded:
		return cell{"ended", c.dim}
	default:
		return cell{st.State, ""}
	}
}

// cmdSessionHook は Claude Code の hook から呼ばれ、stdin の JSON を読んで
// セッションのマーカーを更新する。`--print-config` で settings.json 用スニペットを出す。
func cmdSessionHook(args []string) error {
	if slices.Contains(args, "--print-config") {
		fmt.Print(sessionHookConfig())
		return nil
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("hook 入力を読めません: %w", err)
	}
	var in struct {
		HookEventName    string `json:"hook_event_name"`
		NotificationType string `json:"notification_type"`
		Cwd              string `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return fmt.Errorf("hook 入力の JSON を解析できません: %w", err)
	}
	// 状態に影響しないイベントは黙って無視する (hook を失敗させない)。
	state := sessionStateFor(in.HookEventName, in.NotificationType)
	if state == "" {
		return nil
	}
	// worktree を特定できない (git 管理外など) セッションは突合できないので書かない。
	key := worktreeKey(in.Cwd)
	if key == "" {
		return nil
	}
	return writeSessionState(key, state, in.Cwd, time.Now())
}

// sessionHookConfig は ~/.claude/settings.json に貼る hooks スニペットを返す。
// session-hook 側が notification_type で絞るので matcher は不要。
func sessionHookConfig() string {
	return `# ~/.claude/settings.json の "hooks" にマージしてください (agent-tasks が PATH にある前提):
{
  "hooks": {
    "UserPromptSubmit": [{ "hooks": [{ "type": "command", "command": "agent-tasks session-hook" }] }],
    "Stop":             [{ "hooks": [{ "type": "command", "command": "agent-tasks session-hook" }] }],
    "Notification":     [{ "hooks": [{ "type": "command", "command": "agent-tasks session-hook" }] }],
    "SessionEnd":       [{ "hooks": [{ "type": "command", "command": "agent-tasks session-hook" }] }]
  }
}
`
}
