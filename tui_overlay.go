package main

import (
	"encoding/json"
	"os"
)

// tui-overlay は herdr プラグインの action (open-tui) から呼ばれ、tui を **アクティブ pane の
// プロジェクト**で overlay 表示する内部コマンド (0124)。
//
// なぜ action 経由で薄いラッパーが要るか (0117 で判明):
//   - overlay を開く pane の cwd は、開いた側 (アクティブ) の pane と無関係 (プラグイン root 等)。
//     tui の「現在 project = cwd の git root basename」が効かず、意図した project にならない。
//   - 開かれる pane 自身は「どの pane から開かれたか」を知らない (自 pane の HERDR_PANE_ID のみ)。
//     アクティブ pane の情報は **action の呼び出しコンテキスト** (env HERDR_PLUGIN_CONTEXT_JSON) に
//     しか無い。static な manifest argv では展開できないので、この env を読むラッパーを挟む。
//
// やること: HERDR_PLUGIN_CONTEXT_JSON の focused_pane_cwd を取り出し、
//   herdr plugin pane open --plugin agent-tasks --entrypoint tui --placement overlay --cwd <cwd>
// を実行する。overlay pane がアクティブ pane の cwd で起動 → tui の現在 project がそれになる
// (tui 自体は無変更。currentProject が cwd の git root basename を解決)。cwd が取れなければ
// --cwd を付けずに開く (tui は横断表示 or プラグイン root の project にフォールバック)。

// herdrPluginContext は HERDR_PLUGIN_CONTEXT_JSON の必要フィールド (実地確認済み・0124)。
// 例: {"workspace_id":"wB","workspace_cwd":"…/workforce","focused_pane_id":"wB:p1",
//
//	"focused_pane_cwd":"…/workforce","focused_pane_agent":"claude",…}
type herdrPluginContext struct {
	FocusedPaneCwd string `json:"focused_pane_cwd"`
	WorkspaceCwd   string `json:"workspace_cwd"`
}

// focusedPaneCwd は action コンテキストからアクティブ pane の cwd を返す。
// focused_pane_cwd 優先、無ければ workspace_cwd。どちらも無ければ ""。
func focusedPaneCwd() string {
	raw := os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")
	if raw == "" {
		return ""
	}
	var ctx herdrPluginContext
	if err := json.Unmarshal([]byte(raw), &ctx); err != nil {
		return ""
	}
	if ctx.FocusedPaneCwd != "" {
		return ctx.FocusedPaneCwd
	}
	return ctx.WorkspaceCwd
}

// cmdTuiOverlay は action open-tui の本体。アクティブ pane の cwd で tui overlay を開く。
func cmdTuiOverlay(args []string) error {
	for _, a := range args {
		return usagef("tui-overlay: unexpected argument %q", a)
	}
	openArgs := []string{"plugin", "pane", "open", "--plugin", "agent-tasks", "--entrypoint", "tui", "--placement", "overlay"}
	if cwd := focusedPaneCwd(); cwd != "" {
		openArgs = append(openArgs, "--cwd", cwd)
	}
	// herdr CLI をそのまま呼ぶ (action には HERDR_* env が注入済み。herdrRun は PATH の herdr を実行)。
	_, err := herdrRun(openArgs...)
	return err
}
