package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"time"
)

// worktime-record は herdr プラグインの event hook (pane.agent_status_changed) から呼ばれ、
// pane の agent 状態遷移を worktime ログ (worktime/<session_id>.jsonl) に追記する内部コマンド。
// これが session-hook に代わる worktime の記録源で、状態ソースを herdr に一本化する (0114)。
//
// 経路: herdr が状態遷移ごとに短命プロセスとして本コマンドを起動し、env
// HERDR_PLUGIN_EVENT_JSON にイベント JSON を渡す。session-hook と同じ ephemeral モデルだが、
// 発火は herdr が管理する (Claude 固有 hook が不要になる)。stdout/stderr/exit_code は
// `herdr plugin log list` で観測できる。
//
// ⚠️ イベント JSON は pane_id / agent_status / agent のみで session_id を含まない。worktime は
// session_id キーなので、pane_id から `herdr agent get` して agent_session.value を解決する。
//
// 記録先は state dir (session.go の worktimeDir)。別名ビルド (agent-tasks-herdr) は progName 由来の
// 隔離 state dir を使うので、稼働中の本体版 worktime ログを壊さない (0113 の共存制約)。

// herdrPluginEvent は HERDR_PLUGIN_EVENT_JSON の構造 (実地確認済み・0114)。
// フィールドは data の下にネストする:
//
//	{"event":"pane_agent_status_changed",
//	 "data":{"type":"pane_agent_status_changed","pane_id":"w3:p8",
//	         "workspace_id":"w3","agent_status":"working","agent":"claude"}}
type herdrPluginEvent struct {
	Event string `json:"event"`
	Data  struct {
		Type        string `json:"type"`
		PaneID      string `json:"pane_id"`
		WorkspaceID string `json:"workspace_id"`
		AgentStatus string `json:"agent_status"`
		Agent       string `json:"agent"`
	} `json:"data"`
}

// cmdWorktimeRecord は event hook の本体。プラグイン用の内部コマンドなので、herdr の
// セッションを乱さないよう **失敗させない方針** (session-hook と同じ): 入力不正や herdr 解決
// 失敗は警告を stderr に出して exit 0 する (herdr plugin log で観測できる)。
// `--print-plugin` で導入用の herdr-plugin.toml スニペットを出す。
func cmdWorktimeRecord(args []string) error {
	if slices.Contains(args, "--print-plugin") {
		fmt.Print(worktimePluginManifest())
		return nil
	}
	for _, a := range args {
		return usagef("worktime-record: unexpected argument %q", a)
	}

	ev, ok := readPluginEvent()
	if !ok {
		return nil // 入力なし/不正: 警告済み。no-op で抜ける。
	}
	if ev.Data.AgentStatus == "" {
		return nil // 状態不明のイベントは記録しない。
	}
	pane := ev.Data.PaneID
	if pane == "" {
		fmt.Fprintln(os.Stderr, "agent-tasks worktime-record: イベントに pane_id がありません")
		return nil
	}

	// session_id はイベントに無いので pane から解決する (herdr agent get → agent_session.value)。
	//
	// 既知の限界: 解決が失敗するのは pane 消滅/セッション消失と重なる瞬間 = 多くは working を
	// 閉じる終端イベント (idle/done)。ここで取りこぼすと working 区間が閉じず、集計は openEnd
	// (未 done タスクは now) まで伸びて過大計上になり得る。通常はターン境界の idle は pane 生存中に
	// 発火し解決できるため実害は稀。done タスクは completed_at でクリップされ暴走しない (worktime.go)。
	sessionID, err := resolveSessionIDForPane(pane)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-tasks worktime-record: session_id を解決できません (pane %s): %v\n", pane, err)
		return nil
	}
	if sessionID == "" {
		// agent セッションを持たない pane (agent 未起動など)。記録対象外。
		return nil
	}

	now := time.Now()
	// herdr の agent_status を worktime の状態としてそのまま記録する。集計 (workingIntervals) は
	// "working" で区間を開き、それ以外 (idle/blocked/done/unknown) で閉じるので、値の変換は不要。
	state := ev.Data.AgentStatus

	// 連続同状態は記録しない (herdr は遷移時のみ発火するが、再起動時の再送などで重複し得る)。
	// 集計は重複に耐えるが、ログをきれいに保つため直近状態と同じならスキップする。
	if last, ok := lastWorktimeState(sessionID); ok && last == state {
		return nil
	}
	if err := appendWorktimeEvent(sessionID, state, now); err != nil {
		fmt.Fprintf(os.Stderr, "agent-tasks worktime-record: worktime ログ追記失敗: %v\n", err)
	}
	return nil
}

// readPluginEvent は HERDR_PLUGIN_EVENT_JSON からイベントを読んでパースする。
// 読めない/壊れているときは警告して ok=false。
//
// イベントは herdr が **env で渡す** のが唯一の経路 (docs 記載)。stdin フォールバックは
// 設けない: hook は短命プロセスなので、env 未設定時に stdin を読みにいくと、herdr が
// EOF を閉じない stdin を渡した場合に io.ReadAll が無限ブロックし、ephemeral なはずの
// プロセスがハングする。env が空なら警告して即抜ける。
func readPluginEvent() (herdrPluginEvent, bool) {
	raw := os.Getenv("HERDR_PLUGIN_EVENT_JSON")
	if raw == "" {
		fmt.Fprintln(os.Stderr, "agent-tasks worktime-record: HERDR_PLUGIN_EVENT_JSON が空です")
		return herdrPluginEvent{}, false
	}
	var ev herdrPluginEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		fmt.Fprintf(os.Stderr, "agent-tasks worktime-record: イベント JSON を解析できません: %v\n", err)
		return herdrPluginEvent{}, false
	}
	return ev, true
}

// resolveSessionIDForPane は pane id から agent の session_id (agent_session.value) を解決する。
// pane に agent が無い/セッション未確立なら ("", nil) を返す (記録対象外)。テストで差し替え可能。
var resolveSessionIDForPane = func(pane string) (string, error) {
	p, err := herdrAgentGet(pane)
	if err != nil {
		return "", err
	}
	return p.AgentSession.Value, nil
}

// lastWorktimeState は session の worktime ログの最新イベントの状態を返す。無ければ ok=false。
func lastWorktimeState(sessionID string) (string, bool) {
	evs, err := readWorktimeEvents(sessionID)
	if err != nil || len(evs) == 0 {
		return "", false
	}
	return evs[len(evs)-1].State, true
}

// worktimePluginManifest は導入用の herdr-plugin.toml (event hook 部分) を返す。
// repo 同梱の herdr-plugin.toml と同じ内容 (--print-plugin で案内に使う)。
func worktimePluginManifest() string {
	return `# herdr-plugin.toml (プラグイン root に置く) に含める event hook。
# 導入: herdr plugin link <このファイルのあるディレクトリ>
[[events]]
on = "pane.agent_status_changed"
command = ["agent-tasks-herdr", "worktime-record"]
`
}
