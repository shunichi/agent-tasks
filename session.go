package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// session の状態は Claude Code の hook から書き込まれるマーカーで管理する。
// hook が `agent-tasks session-hook` を呼ぶと、stdin の JSON (session_id 等) から
// 状態を判定して 1 セッション 1 ファイルでマーカーを書く。list がそれを読んで
// in-progress のセッションが「処理中 (working) か入力待ち (waiting) か」を表示する。
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
	Updated string `json:"updated"` // RFC3339
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

// sessionIDFromURL は frontmatter の session 値からセッション ID を取り出す。
// 形式は https://claude.ai/code/session_<id>。末尾セグメントの session_ プレフィックスを外す。
// hook が渡す session_id と一致する想定。
func sessionIDFromURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[i+1:]
	}
	return strings.TrimPrefix(u, "session_")
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

// writeSessionState はセッション ID のマーカーを書く。
func writeSessionState(id, state string, now time.Time) error {
	if id == "" {
		return fmt.Errorf("session id が空")
	}
	if strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("不正な session id: %q", id)
	}
	dir := sessionStateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(sessionState{State: state, Updated: now.Format(time.RFC3339)})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, id+".json"), data, 0o644)
}

// readSessionState はセッション ID のマーカーを読む。無ければ ok=false。
func readSessionState(id string) (sessionState, bool) {
	if id == "" || strings.ContainsAny(id, `/\`) {
		return sessionState{}, false
	}
	data, err := os.ReadFile(filepath.Join(sessionStateDir(), id+".json"))
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
// マーカーが無い (hook 未導入 or セッション不明) ときは "?"。
func sessionCell(t Task, c colors) cell {
	if t.Status != "in-progress" {
		return cell{"", ""}
	}
	id := sessionIDFromURL(t.Session)
	st, ok := readSessionState(id)
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
		SessionID        string `json:"session_id"`
		HookEventName    string `json:"hook_event_name"`
		NotificationType string `json:"notification_type"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return fmt.Errorf("hook 入力の JSON を解析できません: %w", err)
	}
	// 識別できない/状態に影響しない入力は黙って無視する (hook を失敗させない)。
	if in.SessionID == "" {
		return nil
	}
	state := sessionStateFor(in.HookEventName, in.NotificationType)
	if state == "" {
		return nil
	}
	return writeSessionState(in.SessionID, state, time.Now())
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
