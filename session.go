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
// hook が渡す session_id (ローカル CLI の UUID) は frontmatter の session URL
// (claude.ai の session_<id>) と一致しないため、突合は 2 経路を併用する:
//   (1) worktree 経路: hook の cwd の git root basename (例 agent-tasks--0020) でマーカーを書く。
//       spawn 子セッションは worktree 内で動くので確実に当たる。
//   (2) session 経路: 同一セッションで start したタスク (cwd がメインリポのまま) は (1) では当たらない。
//       hook が session_id キーのマーカー (cwd 付き) も書き、session-link が現在 cwd で逆引きして
//       <worktreeキー>.link.json に対応を記録する。list は両者の新しい方を採用する。
//
// マーカー・link はマシンローカルな揮発情報なので、git 同期されるストアの外 (state dir) に置く。

// セッション状態の値。
const (
	sessWorking = "working" // 処理中 (入力を受け取って動いている)
	sessWaiting = "waiting" // ユーザーの入力/許可待ちで止まっている
	sessEnded   = "ended"   // セッション終了 (タスクが in-progress のままなら未 done のサイン)
)

// sessionState はマーカーファイルの中身。
type sessionState struct {
	State     string `json:"state"`
	Updated   string `json:"updated"`              // RFC3339
	Cwd       string `json:"cwd,omitempty"`        // 参考: hook 実行時の cwd
	SessionID string `json:"session_id,omitempty"` // hook の session_id (sess-<id> マーカー用。突合に使う)
}

// sessionLink は「タスク (worktree キー) → セッション」の対応。
// 同一セッション start (cwd がメインリポのまま) のとき、worktree キーのマーカーが
// 書かれない (hook の cwd が worktree 外) ので、ここでセッションを明示的に紐づける。
// session-link コマンドが書き、sessionCell が worktree マーカーの代わり/補完として読む。
// マシンローカルな揮発情報なので、worktree マーカー同様に同期ストアの外 (state dir) に置く。
type sessionLink struct {
	SessionID string `json:"session_id"`
	Updated   string `json:"updated"` // RFC3339
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

// taskSessionState は task に紐づくセッションの最新状態を返す。
// 2 経路を突合する: (1) worktree キーのマーカー (spawn 子: cwd が worktree 内) と
// (2) <key>.link.json 経由の sess-<id> マーカー (同一セッション start: session-link が記録)。
// 両方ある場合は updated が新しい方を採用する。どちらも無ければ ok=false (= list で "?")。
func taskSessionState(t Task) (sessionState, bool) {
	key := taskSessionKey(t)
	if key == "" {
		return sessionState{}, false
	}
	var best sessionState
	found := false
	if st, ok := readSessionState(key); ok {
		best, found = st, true
	}
	if link, ok := readSessionLink(key); ok {
		if st, ok := readSessionState(sessionMarkerKey(link.SessionID)); ok {
			if !found || sessionUpdatedAfter(st, best) {
				best, found = st, true
			}
		}
	}
	return best, found
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
	return writeSessionMarker(key, sessionState{State: state, Updated: now.Format(time.RFC3339), Cwd: cwd})
}

// writeSessionMarker は sessionState をそのままマーカーとして書く (SessionID 付きの sess-<id> 用)。
func writeSessionMarker(key string, st sessionState) error {
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
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, key+".json"), data, 0o644)
}

// sessionMarkerKey は session_id 突合用マーカーのキー (sess-<id>) を返す。
// worktree キー (<project>--<NNNN>) と名前空間が衝突しないよう prefix を付ける。
func sessionMarkerKey(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	return "sess-" + sessionID
}

// writeSessionLink は worktree キー key のセッション対応 (<key>.link.json) を書く。
func writeSessionLink(key, sessionID string, now time.Time) error {
	if key == "" || strings.ContainsAny(key, `/\`) {
		return fmt.Errorf("不正な link key: %q", key)
	}
	if sessionID == "" {
		return fmt.Errorf("session_id が空")
	}
	dir := sessionStateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(sessionLink{SessionID: sessionID, Updated: now.Format(time.RFC3339)})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, key+".link.json"), data, 0o644)
}

// readSessionLink は worktree キー key のセッション対応を読む。無ければ ok=false。
func readSessionLink(key string) (sessionLink, bool) {
	if key == "" || strings.ContainsAny(key, `/\`) {
		return sessionLink{}, false
	}
	data, err := os.ReadFile(filepath.Join(sessionStateDir(), key+".link.json"))
	if err != nil {
		return sessionLink{}, false
	}
	var l sessionLink
	if err := json.Unmarshal(data, &l); err != nil || l.SessionID == "" {
		return sessionLink{}, false
	}
	return l, true
}

// canonPath は比較用にパスを正規化する (symlink 解決 → 失敗時は Clean)。
// hook の cwd (Claude の起動 cwd) と os.Getwd() が symlink の有無で食い違うのを吸収する。
func canonPath(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// resolveSessionByCwd は cwd に対応する sess-<id> マーカーのうち最新のものの session_id を返す。
// 同一セッション start のとき、エージェントは自分の session_id を直接知れない
// (CLAUDE_SESSION_ID のような env は無い) ため、hook が書いた sess マーカーの cwd で逆引きする。
// 一致が無ければ ""。
func resolveSessionByCwd(cwd string) string {
	want := canonPath(cwd)
	if want == "" {
		return ""
	}
	entries, err := os.ReadDir(sessionStateDir())
	if err != nil {
		return ""
	}
	var best sessionState
	var bestID string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "sess-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sessionStateDir(), name))
		if err != nil {
			continue
		}
		var st sessionState
		if err := json.Unmarshal(data, &st); err != nil {
			continue
		}
		if canonPath(st.Cwd) != want {
			continue
		}
		id := st.SessionID
		if id == "" { // 古いマーカー互換: ファイル名から復元
			id = strings.TrimSuffix(strings.TrimPrefix(name, "sess-"), ".json")
		}
		if bestID == "" || sessionUpdatedAfter(st, best) {
			best, bestID = st, id
		}
	}
	return bestID
}

// sessionUpdatedAfter は a.Updated が b.Updated より新しいかを返す (パース不能は zero 扱い)。
func sessionUpdatedAfter(a, b sessionState) bool {
	return parseSessionTime(a.Updated).After(parseSessionTime(b.Updated))
}

func parseSessionTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
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
	st, ok := taskSessionState(t)
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
	// hook は失敗させない方針 (非ゼロ終了で Claude Code のセッションを乱さない)。
	// 入力エラー/不正 JSON は警告だけ出して no-op で抜ける。入力は防御的に上限を設ける。
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-tasks session-hook: 入力を読めません: %v\n", err)
		return nil
	}
	var in struct {
		HookEventName    string `json:"hook_event_name"`
		NotificationType string `json:"notification_type"`
		Cwd              string `json:"cwd"`
		SessionID        string `json:"session_id"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		fmt.Fprintf(os.Stderr, "agent-tasks session-hook: JSON を解析できません: %v\n", err)
		return nil
	}
	// 状態に影響しないイベントは黙って無視する (hook を失敗させない)。
	state := sessionStateFor(in.HookEventName, in.NotificationType)
	if state == "" {
		return nil
	}
	now := time.Now()
	// (1) worktree キーのマーカー: cwd が worktree 内 (spawn 子) のとき突合に使う。
	if key := worktreeKey(in.Cwd); key != "" {
		if err := writeSessionState(key, state, in.Cwd, now); err != nil {
			fmt.Fprintf(os.Stderr, "agent-tasks session-hook: マーカー書込み失敗: %v\n", err)
		}
	}
	// (2) session_id キーのマーカー: 同一セッション start (cwd がメインリポのまま) でも、
	// session-link が cwd で逆引きして紐づけられるよう、cwd とともに常に記録する。
	if in.SessionID != "" {
		st := sessionState{State: state, Updated: now.Format(time.RFC3339), Cwd: in.Cwd, SessionID: in.SessionID}
		if err := writeSessionMarker(sessionMarkerKey(in.SessionID), st); err != nil {
			fmt.Fprintf(os.Stderr, "agent-tasks session-hook: sess マーカー書込み失敗: %v\n", err)
		}
	}
	// hook は失敗させない方針なので、書込みエラーは警告のみで非ゼロ終了しない。
	return nil
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

// cmdSessionLink は「このセッション ↔ このタスク」を紐づける。
// 同一セッションで start したタスク (cwd がメインリポのまま) は worktree キーのマーカーが
// 書かれないため、start 手順の中で本コマンドを呼んでセッションを明示的に対応づける。
// 自分の session_id は直接知れない (env が無い) ので、hook が書いた sess マーカーを
// 現在 cwd で逆引きして特定する。見つからない (hook 未導入など) ときはエラーにせず案内のみ。
func cmdSessionLink(args []string) error {
	project, id, err := resolveProjectID(args)
	if err != nil {
		return err
	}
	path, err := resolveTaskPath(project, id)
	if err != nil {
		return err
	}
	t, err := parseTask(path)
	if err != nil {
		return err
	}
	key := taskSessionKey(t)
	if key == "" {
		return fmt.Errorf("タスク %s/%s に worktree が記録されていません (start 済みか確認してください)", project, id)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	sessionID := resolveSessionByCwd(cwd)
	if sessionID == "" {
		fmt.Printf("セッションを特定できませんでした (cwd: %s)。session-hook が未導入か、まだ発火していない可能性があります。\n", cwd)
		fmt.Println("hook 導入: agent-tasks session-hook --print-config")
		return nil
	}
	if err := writeSessionLink(key, sessionID, time.Now()); err != nil {
		return err
	}
	fmt.Printf("linked %s → session %s\n", key, sessionID)
	return nil
}
