package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// herdr 連携の共通クライアント層。herdr 全面移行 (0105 で合意) の基盤。
// 以降の herdr 移行タスク (spawn=0108 / 状態検出=0109 / 自 session_id=0110 / rename=0111 …) は
// すべてこの層を通して herdr を叩く (パース・エラー処理・pane 特定を 1 箇所に集約する)。
//
// 方式: まず herdr CLI (`herdr <sub> …`) をシェルアウトして JSON をパースする薄い層にする
// (0105 の実機検証で列挙/検査系が JSON を返すことを確認済み)。ホットパスの socket 直叩き
// (`$HERDR_SOCKET_PATH` への JSON-RPC) は必要になってから検討する。
//
// テスト容易性: 実際の exec は herdrRun 変数越しに行うので、テストはこれをスタブに差し替えて
// 実 herdr 無しでパース・引数組み立てを検証できる。
//
// 識別子形式: workspace=w<n> / pane=w<n>:p<n> / tab=w<n>:t<n>。自 pane は env HERDR_PANE_ID。

// herdrBinary は呼び出す herdr 実行ファイル名 (PATH 解決)。
const herdrBinary = "herdr"

// herdrRun は herdr サブコマンドを実行し stdout を返す (テストで差し替え可能な seam)。
// 失敗時は stderr を含むエラーにする。
var herdrRun = func(args ...string) ([]byte, error) {
	out, err := exec.Command(herdrBinary, args...).Output()
	if err != nil {
		sub := "?"
		if len(args) > 0 {
			sub = strings.Join(args, " ")
		}
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("herdr %s: %w: %s", sub, err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("herdr %s: %w", sub, err)
	}
	return out, nil
}

// --- env ヘルパ (herdr が pane 内プロセスへ注入する変数) ---

// herdrEnabled は herdr の pane 内で動いているか (HERDR_ENV=1)。
func herdrEnabled() bool { return os.Getenv("HERDR_ENV") == "1" }

// herdrPaneID は自 pane の識別子 (例 w3:p1)。herdr 外なら空。
func herdrPaneID() string { return os.Getenv("HERDR_PANE_ID") }

// herdrWorkspaceID は自 workspace の識別子 (例 w3)。herdr 外なら空。
func herdrWorkspaceID() string { return os.Getenv("HERDR_WORKSPACE_ID") }

// herdrTabID は自 tab の識別子 (例 w3:t1)。herdr 外なら空。
func herdrTabID() string { return os.Getenv("HERDR_TAB_ID") }

// herdrSocketPath は herdr server の socket パス。herdr 外なら空。
func herdrSocketPath() string { return os.Getenv("HERDR_SOCKET_PATH") }

// requireHerdr は herdr 前提の操作の入口ガード。全面移行方針 (案A) では herdr 内であることを
// 要求し、そうでなければ分かりやすいエラーを返す (黙って tmux にフォールバックしない)。
func requireHerdr() error {
	if !herdrEnabled() {
		return fmt.Errorf("herdr の外で実行されています (HERDR_ENV≠1)。herdr の pane 内で起動してください")
	}
	if herdrSocketPath() == "" {
		return fmt.Errorf("HERDR_SOCKET_PATH が未設定です (herdr server に接続できません)")
	}
	return nil
}

// --- 型 (CLI の JSON 応答をパースする) ---

// herdrAgentSession は pane に紐づく agent セッション情報。value はローカル session UUID
// (Claude 統合が捕捉。0110 で self session_id 取得に使う。claude.ai URL とは別物)。
type herdrAgentSession struct {
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`   // 例 "id"
	Source string `json:"source"` // 例 "herdr:claude"
	Value  string `json:"value"`  // 例 session UUID
}

// herdrPane は agent get/list・pane get/list の 1 要素。フィールドは両者で共通
// (pane に agent が無ければ Agent="" / AgentStatus="unknown")。
type herdrPane struct {
	Agent        string            `json:"agent"`
	AgentSession herdrAgentSession `json:"agent_session"`
	AgentStatus  string            `json:"agent_status"` // idle|working|blocked|unknown
	Cwd          string            `json:"cwd"`
	Focused      bool              `json:"focused"`
	PaneID       string            `json:"pane_id"`
	TabID        string            `json:"tab_id"`
	WorkspaceID  string            `json:"workspace_id"`
}

// --- 列挙/検査 ---

// herdrAgentGet は 1 pane の agent 情報を返す。target は pane id (例 w3:p1) や agent 名など。
func herdrAgentGet(target string) (*herdrPane, error) {
	out, err := herdrRun("agent", "get", target)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result struct {
			Agent herdrPane `json:"agent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("herdr agent get %s: JSON パース失敗: %w", target, err)
	}
	return &resp.Result.Agent, nil
}

// herdrAgentList は agent が居る pane を列挙する。
func herdrAgentList() ([]herdrPane, error) {
	out, err := herdrRun("agent", "list")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result struct {
			Agents []herdrPane `json:"agents"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("herdr agent list: JSON パース失敗: %w", err)
	}
	return resp.Result.Agents, nil
}

// herdrPaneList は pane を列挙する。workspace が空なら全 workspace。
func herdrPaneList(workspace string) ([]herdrPane, error) {
	args := []string{"pane", "list"}
	if workspace != "" {
		args = append(args, "--workspace", workspace)
	}
	out, err := herdrRun(args...)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result struct {
			Panes []herdrPane `json:"panes"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("herdr pane list: JSON パース失敗: %w", err)
	}
	return resp.Result.Panes, nil
}

// herdrSelfAgent は自 pane (HERDR_PANE_ID) の agent 情報を返す。herdr 外なら requireHerdr のエラー。
// 0110 (自 session_id) / 0109 (自セッションの状態) の起点。
func herdrSelfAgent() (*herdrPane, error) {
	if err := requireHerdr(); err != nil {
		return nil, err
	}
	pane := herdrPaneID()
	if pane == "" {
		return nil, fmt.Errorf("HERDR_PANE_ID が未設定です (自 pane を特定できません)")
	}
	return herdrAgentGet(pane)
}

// --- 出力読取 ---

// herdrPaneRead は pane の内容を読む (alt-screen でも読める)。source は visible|recent|recent-unwrapped。
// lines<=0 なら --lines を付けない (herdr の既定行数)。CLI はプレーンテキストを返す。
func herdrPaneRead(pane, source string, lines int) (string, error) {
	args := []string{"pane", "read", pane}
	if source != "" {
		args = append(args, "--source", source)
	}
	if lines > 0 {
		args = append(args, "--lines", strconv.Itoa(lines))
	}
	out, err := herdrRun(args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// --- 入力送出 ---

// herdrPaneSendText は pane にリテラル文字列を注入する (Enter を付けない)。
func herdrPaneSendText(pane, text string) error {
	_, err := herdrRun("pane", "send-text", pane, text)
	return err
}

// herdrPaneRun は pane に文字列 + 本物の Enter を 1 リクエストで送る (末尾に Enter が付く)。
// text と Enter が分かれない (アトミック) ので、claude の入力欄への submit を確実に発火させたい
// とき (session-rename の /rename 打ち込みなど) に使う。send-text + send-keys の 2 呼び出しに
// 分けると 2 プロセス間のギャップで Enter が改行として食われることがある (0131)。
func herdrPaneRun(pane, command string) error {
	_, err := herdrRun("pane", "run", pane, command)
	return err
}

// herdrPaneSendKeys は pane にキー (名前付きキー含む) を送る。例: send-keys で Enter を押す。
// send-text (リテラル) と組み合わせて「文字列を入れてから Enter」を実現できるが、submit を
// 確実にしたいときは 2 呼び出しに分けず herdrPaneRun を使う (0131)。
func herdrPaneSendKeys(pane string, keys ...string) error {
	args := append([]string{"pane", "send-keys", pane}, keys...)
	_, err := herdrRun(args...)
	return err
}

// --- 待機 ---

// herdrWaitAgentStatus は pane の agent 状態が status になるまでブロックする。
// status は idle|working|blocked|done|unknown。timeoutMs<=0 なら --timeout を付けない。
// タイムアウトすると herdr が非 0 終了するのでエラーになる。
func herdrWaitAgentStatus(pane, status string, timeoutMs int) error {
	args := []string{"wait", "agent-status", pane, "--status", status}
	if timeoutMs > 0 {
		args = append(args, "--timeout", strconv.Itoa(timeoutMs))
	}
	_, err := herdrRun(args...)
	return err
}

// --- ラベル ---

// herdrAgentRename は pane/agent の表示ラベルを設定する (0111 で /rename ハックを置換)。
func herdrAgentRename(target, name string) error {
	_, err := herdrRun("agent", "rename", target, name)
	return err
}

// --- フォーカス (focus コマンド = 0047) ---

// herdrAgentFocus は target の pane を前面に出す (workspace / tab / pane をまたぐ切り替えは
// herdr が内部で面倒を見る)。target は pane id (例 w3:p1) を渡す。session_id は target に
// 取れない (agent_not_found になる) ので、呼び出し側は必ず agent list で pane_id を得てから
// 渡すこと (resolveTaskPane 参照)。
func herdrAgentFocus(target string) error {
	if err := requireHerdr(); err != nil {
		return err
	}
	_, err := herdrRun("agent", "focus", target)
	return err
}

// --- pane 起動 (spawn の中核) ---

// herdrAgentStart は新しい pane で agent を起動する (spawn=0108 の中核)。
// name は herdr の表示ラベル、cwd は起動ディレクトリ (空なら herdr 既定)、split は right|down
// (空なら herdr 既定)、focus=false で背面起動 (親のフォーカスを奪わない)。argv は pane 内で
// 実行するコマンド (例 ["claude","-n","task 0001: …","タスク 0001 に着手して"])。
// 作成された pane 情報を返す。
func herdrAgentStart(name, cwd, split string, focus bool, argv []string) (*herdrPane, error) {
	if err := requireHerdr(); err != nil {
		return nil, err
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("herdr agent start: argv が空")
	}
	args := []string{"agent", "start", name}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	if split != "" {
		args = append(args, "--split", split)
	}
	if focus {
		args = append(args, "--focus")
	} else {
		args = append(args, "--no-focus")
	}
	args = append(args, "--")
	args = append(args, argv...)
	out, err := herdrRun(args...)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result struct {
			Agent herdrPane `json:"agent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("herdr agent start: JSON パース失敗: %w", err)
	}
	return &resp.Result.Agent, nil
}

// --- herdr-probe: クライアント層の疎通確認 (開発/デバッグ用) ---

// cmdHerdrProbe は herdr クライアント層の疎通を確認する内部コマンド。env と自 pane の
// agent 情報を表示するだけ (副作用なし)。0106 の動作確認 + 以降のタスクの足場。
// 補完には出さない (デバッグ用途)。
func cmdHerdrProbe(args []string) error {
	for _, a := range args {
		return usagef("herdr-probe: unexpected argument %q", a)
	}
	fmt.Printf("HERDR_ENV=%q HERDR_PANE_ID=%q HERDR_WORKSPACE_ID=%q\n",
		os.Getenv("HERDR_ENV"), herdrPaneID(), herdrWorkspaceID())
	fmt.Printf("HERDR_SOCKET_PATH=%q\n", herdrSocketPath())
	self, err := herdrSelfAgent()
	if err != nil {
		return err
	}
	fmt.Printf("self pane %s: agent=%q status=%q session_id=%q cwd=%s\n",
		self.PaneID, self.Agent, self.AgentStatus, self.AgentSession.Value, self.Cwd)
	agents, err := herdrAgentList()
	if err != nil {
		return err
	}
	fmt.Printf("agents (%d):\n", len(agents))
	for _, a := range agents {
		fmt.Printf("  %s  status=%-8s agent=%s\n", a.PaneID, a.AgentStatus, a.Agent)
	}
	return nil
}
