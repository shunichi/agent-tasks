package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
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
//       hook が session_id キーのマーカー (cwd 付き) も書き、session-link が <worktreeキー>.link.json に
//       対応を記録する。session_id は --session で明示 (Claude は self-id を知れる) するか、省略時は
//       hook の sess マーカーを現在 cwd で逆引きして特定する。list は両者の新しい方を採用する。
//
// 層の切り分け: マーカー・link の保管/突合 (この CLI) は agent 中立。状態の信号源 (hook) と
// self-id の取得方法は agent 固有なので SKILL 側に agent 別に置く。
// マーカー・link はマシンローカルな揮発情報なので、git 同期されるストアの外 (state dir) に置く。

// SessionIssue は frontmatter の session: が web URL 形式でない (ローカル session_id = UUID の
// 貼り間違いなど) ことを表す doctor の検出結果。
type SessionIssue struct {
	Project string
	ID      string
	Detail  string
	Path    string
}

// findSessionIssues は session: が空でなく URL 形式 (http(s)://) でないものを拾う。
// session: は「人が開く web セッション URL」(https://claude.ai/code/session_…) を入れる定義で、
// claim / session-link の --session に渡すローカル CLI の session_id (UUID) とは別物。エージェントは
// 自分の UUID (スクラッチパッドのパス末尾) は確実に知れるため、web URL の代わりに UUID を貼り間違える
// ことがある。UUID はリンクとして開けないので検出する (prs: / tracker: の URL 検査と同じ流儀。ホストや
// パス構造までは縛らない — 明らかに URL でないものだけ拾う)。
func findSessionIssues(tasks []Task) []SessionIssue {
	var out []SessionIssue
	for _, t := range tasks {
		s := strings.TrimSpace(t.Session)
		if s == "" {
			continue // 空は正常 (URL が取れなければ空でよい)
		}
		if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
			out = append(out, SessionIssue{t.Project, t.ID, "session: の値が URL ではない (ローカル session_id の貼り間違い?): " + s, t.Path})
		}
	}
	return out
}

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
//
// マルチセッション: 1 タスクが中断→別セッションで再開すると複数セッションを使い得るので、
// 使ったセッションを Sessions に**履歴として蓄積**する (worktime が全セッションの working を
// 合算するため)。SessionID/Updated は「最新にリンクしたセッション」= 現在のセッションで、
// SESSION 列や statusline の逆引きに使う (後方互換: 旧形式の単一フィールドとしても読める)。
type sessionLink struct {
	SessionID string       `json:"session_id"`         // 最新 (現在) のセッション
	Updated   string       `json:"updated"`            // RFC3339。最新リンク時刻
	Sessions  []sessionRef `json:"sessions,omitempty"` // このタスクが使った全セッション (worktime の union 用)
}

// sessionRef は sessionLink.Sessions の 1 要素 (タスクが使った 1 セッション)。
type sessionRef struct {
	SessionID string `json:"session_id"`
	Updated   string `json:"updated"` // RFC3339。このセッションを最後にリンクした時刻
}

// linkSessionIDs は link が指す全セッション ID を返す (重複なし)。worktime の union に使う。
func linkSessionIDs(l sessionLink) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, s := range l.Sessions {
		add(s.SessionID)
	}
	add(l.SessionID) // 旧形式 (Sessions 空) の保険
	return ids
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
	return atomicWriteFile(filepath.Join(dir, key+".json"), data, 0o644)
}

// atomicWriteFile は data を path に原子的に書く。同一ディレクトリの一時ファイルに書いてから
// rename で差し替えるので、読み手 (list / --watch) が書きかけの半端な内容を読んで JSON parse に
// 失敗する (= SESSION が "?" になる) ことがない。rename(2) は同一ファイルシステム内でアトミック。
// hook は PreToolUse/PostToolUse 等で高頻度に書くため、この保証が効く。
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // 成功時は rename 済みで存在しない (無害)、失敗時は後始末
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// worktimeEvent は実稼働ログ (worktime/<session_id>.jsonl) の 1 行。状態が変わった時刻を残し、
// working に入った時刻〜抜けた時刻のペアから「稼働区間」を復元する (worktime コマンドが集計)。
type worktimeEvent struct {
	Ts    string `json:"ts"`    // RFC3339。状態が変わった時刻
	State string `json:"state"` // working / waiting / ended
}

// worktimeDir は実稼働ログの置き場 (state dir の下)。マシンローカルな揮発データ。
func worktimeDir() string {
	return filepath.Join(sessionStateDir(), "worktime")
}

// worktimeLogPath は session_id の実稼働ログのパスを返す。session_id はファイル名になるので検証する。
func worktimeLogPath(sessionID string) (string, error) {
	if sessionID == "" || strings.ContainsAny(sessionID, `/\`) {
		return "", fmt.Errorf("不正な session_id: %q", sessionID)
	}
	return filepath.Join(worktimeDir(), sessionID+".jsonl"), nil
}

// appendWorktimeEvent は session の状態遷移を 1 行 JSONL で追記する (append-only)。
// hook が「状態が変わった時だけ」呼ぶ。O_APPEND での 1 行書き込みは PIPE_BUF (4096B) 未満なら
// 並行 hook でもインターリーブせずアトミックなので、行が壊れない。
func appendWorktimeEvent(sessionID, state string, now time.Time) error {
	path, err := worktimeLogPath(sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(worktimeEvent{Ts: now.Format(time.RFC3339), State: state})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// readWorktimeEvents は session の実稼働ログを時刻昇順で読む。無ければ空。壊れた行は飛ばす。
func readWorktimeEvents(sessionID string) ([]worktimeEvent, error) {
	path, err := worktimeLogPath(sessionID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var evs []worktimeEvent
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var e worktimeEvent
		if err := json.Unmarshal([]byte(ln), &e); err != nil || e.State == "" {
			continue // 壊れた行は無視 (追記中の途中書き等)
		}
		evs = append(evs, e)
	}
	slices.SortFunc(evs, func(a, b worktimeEvent) int {
		return parseSessionTime(a.Ts).Compare(parseSessionTime(b.Ts))
	})
	return evs, nil
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
	nowStr := now.Format(time.RFC3339)
	// 既存 link を読み、Sessions に sessionID を蓄積する (中断→別セッション再開の合算のため)。
	// 既に含まれていれば Updated を更新するだけ (同一セッションでの再 start は重複させない)。
	link, _ := readSessionLink(key)
	found := false
	for i := range link.Sessions {
		if link.Sessions[i].SessionID == sessionID {
			link.Sessions[i].Updated = nowStr
			found = true
			break
		}
	}
	if !found {
		link.Sessions = append(link.Sessions, sessionRef{SessionID: sessionID, Updated: nowStr})
	}
	link.SessionID = sessionID // 最新 (現在) のセッション
	link.Updated = nowStr
	data, err := json.Marshal(link)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(dir, key+".link.json"), data, 0o644)
}

// worktreeKeyForSession は session_id に紐づく worktree キー (<project>--<NNNN>) を逆引きする。
// session-link が書く <key>.link.json を走査し、SessionID が一致するもののうち最も新しい
// (Updated が後の) キーを返す。statusline が「このセッションが実行中のタスク」を引くのに使う
// (通常フローではセッションの cwd はメインリポなので worktree キーを cwd から取れない)。
// 一致が無ければ ok=false。
func worktreeKeyForSession(sessionID string) (string, bool) {
	if sessionID == "" {
		return "", false
	}
	entries, err := os.ReadDir(sessionStateDir())
	if err != nil {
		return "", false
	}
	var bestKey string
	var bestUpdated time.Time
	for _, e := range entries {
		name := e.Name()
		key, ok := strings.CutSuffix(name, ".link.json")
		if !ok {
			continue
		}
		link, ok := readSessionLink(key)
		if !ok || link.SessionID != sessionID {
			continue
		}
		if t := parseSessionTime(link.Updated); bestKey == "" || t.After(bestUpdated) {
			bestKey, bestUpdated = key, t
		}
	}
	return bestKey, bestKey != ""
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
	if err := json.Unmarshal(data, &l); err != nil {
		return sessionLink{}, false
	}
	// 後方互換: 旧形式 (Sessions 無し・単一 session_id) は Sessions に正規化する。
	if len(l.Sessions) == 0 && l.SessionID != "" {
		l.Sessions = []sessionRef{{SessionID: l.SessionID, Updated: l.Updated}}
	}
	// SessionID (最新) が空だが履歴があるなら、最も新しいものを最新にする。
	if l.SessionID == "" && len(l.Sessions) > 0 {
		newest := l.Sessions[0]
		for _, s := range l.Sessions[1:] {
			if parseSessionTime(s.Updated).After(parseSessionTime(newest.Updated)) {
				newest = s
			}
		}
		l.SessionID, l.Updated = newest.SessionID, newest.Updated
	}
	if l.SessionID == "" {
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
	if t.IsHuman() {
		return cell{"-", c.dim} // 人手タスクはセッション (エージェント) を持たない
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
		// (2a) 実稼働ログ: 状態が「変わった」ときだけ遷移を追記する (worktime 集計の生データ)。
		// 直前の状態は上書き前の sess マーカーから読む。working→working のような変化なしは
		// 記録しない (PreToolUse/PostToolUse 等の高頻度発火で肥大化しないため)。
		if prev, had := readSessionState(sessionMarkerKey(in.SessionID)); !had || prev.State != state {
			if err := appendWorktimeEvent(in.SessionID, state, now); err != nil {
				fmt.Fprintf(os.Stderr, "agent-tasks session-hook: worktime ログ追記失敗: %v\n", err)
			}
		}
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
	// --session <id> を先に抜く。残りを <project>/<id> として解決する。
	// 取得層は agent 固有なので、自分の session_id を言える agent (例: Claude Code) は
	// --session で明示する (cwd 逆引きの曖昧性を回避)。言えなければ省略し、hook が書いた
	// sess マーカーを cwd で逆引きするフォールバックに任せる。
	var explicitSession string
	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch {
		case a == "--session":
			v, err := s.value("--session")
			if err != nil {
				return err
			}
			explicitSession = v
		default:
			if v, ok := strings.CutPrefix(a, "--session="); ok {
				explicitSession = v
				continue
			}
			s.positional(a)
		}
	}
	project, id, err := resolveProjectID(s.rest())
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

	if strings.ContainsAny(explicitSession, `/\`) {
		return usagef("--session に / や \\ は使えません: %q", explicitSession)
	}
	sessionID := explicitSession
	via := "明示 (--session)"
	if sessionID == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		sessionID = resolveSessionByCwd(cwd)
		via = "cwd 逆引き"
		if sessionID == "" {
			fmt.Printf("セッションを特定できませんでした (cwd: %s)。--session <id> で明示するか、session-hook を導入してください。\n", cwd)
			fmt.Println("hook 導入: agent-tasks session-hook --print-config")
			return nil
		}
	}
	if err := writeSessionLink(key, sessionID, time.Now()); err != nil {
		return err
	}
	fmt.Printf("linked %s → session %s (%s)\n", key, sessionID, via)
	return nil
}

// --- session-prune: state dir に溜まる古いマーカー/link の掃除 ---
//
// hook / session-link が書くマーカー・link は state dir に書かれっぱなしで、タスク done や
// worktree 撤去の後も残り、長期運用で単調増加する (正確性には無害。sessionCell は in-progress の
// タスクしか読まない)。session-prune が安全に掃除する。ストア (タスク本体) には一切触れない。

// sessionPruneDefaultDays は sess マーカーを「古い」とみなす既定日数。
const sessionPruneDefaultDays = 7

// prunableFile は掃除対象の 1 ファイル (state dir 内の名前と理由。表示・削除に使う)。
type prunableFile struct {
	Name   string
	Reason string
}

// planSessionPrune は state dir を走査し、掃除してよいファイルを列挙する (破壊しない)。判定:
//   - worktree マーカー <key>.json / link <key>.link.json:
//     対応タスク (worktree キー) が存在しない or done なら対象。
//     in-progress / blocked / review / todo のタスクのものは残す (稼働・保留中を壊さない)。
//   - sess マーカー sess-<id>.json:
//     生存する link のどれからも参照されず (= どの現役タスクにも紐づかず)、かつ Updated から
//     retention 以上経過していれば対象。未参照でも retention 内なら残す (link 前の起動直後の
//     セッションを守る)。壊れて読めないマーカーは zero 時刻 = 十分古い扱い (未参照なら掃除)。
//
// tasks は全 project のアクティブタスク。now / retention は sess マーカーの鮮度判定に使う。
// state dir は sessionStateDir() (テストは AGENT_TASKS_STATE_DIR を t.TempDir() に向ける)。
func planSessionPrune(tasks []Task, now time.Time, retention time.Duration) ([]prunableFile, error) {
	active := map[string]string{} // worktree キー -> status
	for _, t := range tasks {
		if k := taskSessionKey(t); k != "" {
			active[k] = t.Status
		}
	}
	entries, err := os.ReadDir(sessionStateDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // state dir 未作成 = 掃除対象なし
		}
		return nil, err
	}
	aliveKey := func(key string) bool {
		st, ok := active[key]
		return ok && st != "done"
	}
	whyDead := func(key string) string {
		if _, ok := active[key]; !ok {
			return "対応タスクなし"
		}
		return "対応タスクが done"
	}

	var out []prunableFile
	referenced := map[string]bool{} // 生存 link が指す session_id (sess マーカー保護用)
	type sessInfo struct {
		name    string
		id      string
		updated time.Time
	}
	var sessMarks []sessInfo

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		switch {
		case strings.HasSuffix(name, ".link.json"):
			key := strings.TrimSuffix(name, ".link.json")
			if aliveKey(key) {
				// 生存タスクが使った全セッションを参照済みにする (sess マーカー・worktime ログを守る)。
				if l, ok := readSessionLink(key); ok {
					for _, sid := range linkSessionIDs(l) {
						referenced[sid] = true
					}
				}
				continue
			}
			out = append(out, prunableFile{name, "link (" + whyDead(key) + ")"})
		case strings.HasPrefix(name, "sess-") && strings.HasSuffix(name, ".json"):
			key := strings.TrimSuffix(name, ".json") // "sess-<id>"
			id := strings.TrimPrefix(key, "sess-")
			var upd time.Time
			if st, ok := readSessionState(key); ok {
				upd = parseSessionTime(st.Updated)
				if st.SessionID != "" {
					id = st.SessionID
				}
			}
			sessMarks = append(sessMarks, sessInfo{name, id, upd})
		case strings.HasSuffix(name, ".json"):
			key := strings.TrimSuffix(name, ".json")
			if aliveKey(key) {
				continue
			}
			out = append(out, prunableFile{name, "worktree マーカー (" + whyDead(key) + ")"})
		}
		// .tmp-* など上記に当たらないファイルは触らない (安全側)。
	}

	// sess マーカーは link 判定後に決める (生存 link の参照集合が要るため)。
	days := int(retention / (24 * time.Hour))
	for _, s := range sessMarks {
		if referenced[s.id] {
			continue // 現役タスクに紐づく → 残す
		}
		if now.Sub(s.updated) < retention {
			continue // 新しい → 残す (link 前の起動直後を守る)
		}
		out = append(out, prunableFile{s.name, fmt.Sprintf("sess マーカー (未参照・%d 日超未更新)", days)})
	}

	// 実稼働ログ (worktime/<session_id>.jsonl): sess マーカーと同じ基準で掃除する。
	// 生存 link から参照されず (どの現役タスクにも紐づかず)、かつ retention 超未更新のものが対象。
	// 返す Name は state dir からの相対パス ("worktime/<id>.jsonl") なので cmdSessionPrune の Remove が届く。
	if wtEntries, err := os.ReadDir(worktimeDir()); err == nil {
		for _, e := range wtEntries {
			if e.IsDir() {
				continue
			}
			id, ok := strings.CutSuffix(e.Name(), ".jsonl")
			if !ok {
				continue
			}
			if referenced[id] {
				continue // 現役タスクに紐づく → 残す
			}
			info, err := e.Info()
			if err != nil || now.Sub(info.ModTime()) < retention {
				continue // 新しい (最終追記が retention 内) → 残す
			}
			rel := filepath.Join("worktime", e.Name())
			out = append(out, prunableFile{rel, fmt.Sprintf("worktime ログ (未参照・%d 日超未更新)", days)})
		}
	}

	slices.SortFunc(out, func(a, b prunableFile) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// cmdSessionPrune は state dir の古いマーカー/link を掃除する。既定で削除し、--dry-run で対象のみ表示。
// auto-archive と同じ流儀 (破壊的操作は --dry-run で事前確認)。
func cmdSessionPrune(args []string) error {
	days := sessionPruneDefaultDays
	dryRun := false
	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch a {
		case "--older-than":
			v, err := s.value("--older-than")
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return usagef("--older-than must be a non-negative integer (日数): %q", v)
			}
			days = n
		case "--dry-run":
			dryRun = true
		default:
			return usagef("unknown option: %s", a)
		}
	}
	if pos := s.rest(); len(pos) > 0 {
		return usagef("unexpected argument: %s", pos[0])
	}

	tasks, err := loadTasks(storeDir())
	if err != nil {
		return err
	}
	now := time.Now()
	retention := time.Duration(days) * 24 * time.Hour
	targets, err := planSessionPrune(tasks, now, retention)
	if err != nil {
		return err
	}
	dir := sessionStateDir()
	if len(targets) == 0 {
		fmt.Printf("掃除対象なし (state dir: %s)\n", dir)
		return nil
	}
	if dryRun {
		fmt.Printf("[dry-run] 掃除対象 %d 件 (state dir: %s):\n", len(targets), dir)
		for _, f := range targets {
			fmt.Printf("  %s  — %s\n", f.Name, f.Reason)
		}
		fmt.Println("(--dry-run 指定のため削除していません。実行するには --dry-run を外してください)")
		return nil
	}
	var errs []error
	removed := 0
	for _, f := range targets {
		if err := os.Remove(filepath.Join(dir, f.Name)); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.Name, err))
			continue
		}
		fmt.Printf("removed %s  — %s\n", f.Name, f.Reason)
		removed++
	}
	fmt.Printf("%d 件を掃除しました (state dir: %s)\n", removed, dir)
	return errors.Join(errs...)
}
