package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
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
// 失敗時は stderr を含むエラーにする (JSON エラーなら herdrCLIError にして code を保つ)。
var herdrRun = func(args ...string) ([]byte, error) {
	out, err := exec.Command(herdrBinary, args...).Output()
	if err != nil {
		sub := "?"
		if len(args) > 0 {
			sub = strings.Join(args, " ")
		}
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			if cerr := parseHerdrCLIError(sub, ee.Stderr, err); cerr != nil {
				return nil, cerr
			}
			return nil, fmt.Errorf("herdr %s: %w: %s", sub, err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("herdr %s: %w", sub, err)
	}
	return out, nil
}

// herdrCLIError は herdr CLI が非 0 終了時に stderr へ返す JSON エラー
// (`{"error":{"code":…,"message":…},"id":…}`) を型として保つ。呼び出し側が `code` で分岐できる
// ようにするため (メッセージの文字列マッチに頼らない)。JSON でない失敗 (herdr 自体が無い等) では
// 作られず、素の fmt.Errorf が返る。
type herdrCLIError struct {
	Sub     string // 実行したサブコマンド (メッセージ用)
	Code    string // 例 agent_not_found / agent_not_ready
	Message string
	err     error // 元の exec エラー (exit status)
}

func (e *herdrCLIError) Error() string {
	return fmt.Sprintf("herdr %s: %v: %s (%s)", e.Sub, e.err, e.Message, e.Code)
}

func (e *herdrCLIError) Unwrap() error { return e.err }

// parseHerdrCLIError は stderr を herdr の JSON エラーとして読む。code が取れなければ nil
// (呼び出し側は従来の stderr 文字列付きエラーにフォールバックする)。
func parseHerdrCLIError(sub string, stderr []byte, cause error) *herdrCLIError {
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(stderr, &body) != nil || body.Error.Code == "" {
		return nil
	}
	return &herdrCLIError{Sub: sub, Code: body.Error.Code, Message: body.Error.Message, err: cause}
}

// herdrErrorCode は err に含まれる herdr のエラー code を返す (無ければ空)。
func herdrErrorCode(err error) string {
	var cerr *herdrCLIError
	if errors.As(err, &cerr) {
		return cerr.Code
	}
	return ""
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
// text と Enter が分かれない (アトミック) ので、send-text + send-keys の 2 呼び出しに分けたとき
// 2 プロセス間のギャップで Enter が改行として食われる問題を避けられる (0131)。
// ただし pane 層なので相手が agent かは問わない。agent の入力欄へ送るなら herdrSendPrompt を使う。
func herdrPaneRun(pane, command string) error {
	_, err := herdrRun("pane", "run", pane, command)
	return err
}

// herdrAgentPrompt は agent の入力欄へ 1 行を submit する (agent 層 API)。pane 層の herdrPaneRun と
// アトミック性は同じだが、agent 層は送出前に**その agent の生きた bracketed-paste モードを見てから**
// Enter を送る (herdr #1525) ので、入力欄が paste モードのときに Enter が「改行」として食われる余地が
// 無い。対象が agent を載せた pane かも検証する (agent_not_found / agent_not_ready)。
//
// --wait は意図的に付けない。唯一の呼び出し元 (session-rename) は**自分のターン中 = すでに working**
// から送るが、herdr の --wait は turn を追跡せず「すでに working なら その active turn の完了に
// マッチしうる」ため、自分のターンの終了を待つだけで送信検証にならない。idle の相手を起こす用途が
// できたときに wait オプションを足す。
func herdrAgentPrompt(target, text string) error {
	_, err := herdrRun("agent", "prompt", target, text)
	return err
}

// herdrSendPrompt は pane の agent の入力欄へ 1 行を submit する (入力送出の推奨口)。
// 主経路は agent 層の agent prompt。agent 検出が効いていない pane (未対応 agent / 起動途中) では
// agent prompt が対象を拒むので、そのときだけ pane 層の pane run に落とす (0131 までの経路。
// bracketed-paste を見ないが打ち込みはできる)。
//
// 落とすのは **submit 前に確実に失敗した code だけ** に限る。agent_prompt_failed のような送出途中の
// 失敗で落とすと、既に入っていた 1 行に重ねて二重送信になりうるため。
func herdrSendPrompt(pane, text string) error {
	err := herdrAgentPrompt(pane, text)
	switch herdrErrorCode(err) {
	case "agent_not_found", "agent_not_ready":
		return herdrPaneRun(pane, text)
	}
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

// herdrAgentNameMax は herdr の agent 名 (スラッグ) の最大長。herdr は「小文字始まり /
// [a-z0-9_-] / 1-32 文字」しか受け付けない (invalid_agent_name)。
const herdrAgentNameMax = 32

// herdrSlugify は任意の文字列を herdr の agent 名に使える字種へ落とす (小文字化し、
// [a-z0-9_-] 以外を "-" に潰して連続を 1 つに畳み、前後の "-" を落とす)。
// 「小文字始まり」までは保証しない — 呼び出し側が "task-" のような接頭辞で担保する。
func herdrSlugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			// 連続する区切りは 1 つに畳む。日本語のように 1 語が複数 rune になる入力で
			// "-" が伸び続けて 32 文字枠を食い潰すのを防ぐ。
			if cur := b.String(); cur != "" && !strings.HasSuffix(cur, "-") {
				b.WriteByte('-')
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// herdrAgentNameCandidates は name を第一候補に、衝突時の代替名 (番号サフィックス) を並べて返す。
// herdr は agent 名の一意性を要求し (agent_name_taken)、**終了した agent でも pane が残っている限り
// 名前を握り続ける**ので、同じタスクを spawn し直すと第一候補は普通にぶつかる。
func herdrAgentNameCandidates(name string) []string {
	base := herdrSlugify(name)
	if base == "" {
		base = "agent"
	}
	names := []string{fitHerdrAgentName(base, "")}
	for i := 2; i <= 9; i++ {
		names = append(names, fitHerdrAgentName(base, "-"+strconv.Itoa(i)))
	}
	return names
}

// fitHerdrAgentName は base + suffix を herdrAgentNameMax に収める。溢れる分は base 側だけを詰める
// (suffix は衝突回避の識別子なので落とさない)。
func fitHerdrAgentName(base, suffix string) string {
	if n := max(herdrAgentNameMax-len(suffix), 0); len(base) > n {
		base = strings.TrimRight(base[:n], "-")
	}
	return base + suffix
}

// herdrPaneSplit は現在の pane を分割して新しい pane を作る (中は素の対話シェル)。
// direction は right|down (空なら herdr 既定)、cwd は新 pane の作業ディレクトリ、
// focus=false なら背面に作る (親のフォーカスを奪わない)。
func herdrPaneSplit(cwd, direction string, focus bool) (*herdrPane, error) {
	args := []string{"pane", "split", "--current"}
	if direction != "" {
		args = append(args, "--direction", direction)
	}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	if focus {
		args = append(args, "--focus")
	} else {
		args = append(args, "--no-focus")
	}
	out, err := herdrRun(args...)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result struct {
			Pane herdrPane `json:"pane"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("herdr pane split: JSON パース失敗: %w", err)
	}
	return &resp.Result.Pane, nil
}

// herdrPaneClose は pane を閉じる。
func herdrPaneClose(pane string) error {
	_, err := herdrRun("pane", "close", pane)
	return err
}

// herdrAgentStart は**既存の** pane (対話シェルのプロンプト状態) で agent を起動する。
// name は herdr 全体で一意なスラッグ、kind は herdr がサポートする agent 種別
// (claude/codex/…。実行ファイルはこれで決まる)、agentArgs は **その agent に渡す引数だけ**
// (実行ファイル名は含めない)。timeoutMs<=0 なら herdr 既定 (30s) の起動待ちになる。
// 成功 = その pane で期待した agent が検出され、入力を受け付けられる状態になったこと。
func herdrAgentStart(name, kind, pane string, timeoutMs int, agentArgs []string) (*herdrPane, error) {
	if err := requireHerdr(); err != nil {
		return nil, err
	}
	if name == "" || kind == "" || pane == "" {
		return nil, fmt.Errorf("herdr agent start: name/kind/pane は必須 (name=%q kind=%q pane=%q)", name, kind, pane)
	}
	args := []string{"agent", "start", name, "--kind", kind, "--pane", pane}
	if timeoutMs > 0 {
		args = append(args, "--timeout", strconv.Itoa(timeoutMs))
	}
	if len(agentArgs) > 0 {
		args = append(args, "--")
		args = append(args, agentArgs...)
	}
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

// herdrStartAgentInNewPane は「pane を分割 → そこで agent を起動」を 1 操作にまとめる (spawn の中核)。
// herdr の agent start が pane を自分で作らなくなり既存 pane を要求するようになったので 2 段になった。
//
// 名前の衝突 (agent_name_taken) は候補名を替えて**同じ pane で**試し直す。pane は既に対話シェルの
// プロンプト状態にあり、名前の検証は起動コマンド送出より前に行われるので、作り直す必要がない。
func herdrStartAgentInNewPane(name, kind, cwd, split string, focus bool, timeoutMs int, agentArgs []string) (*herdrPane, error) {
	if err := requireHerdr(); err != nil {
		return nil, err
	}
	pane, err := herdrPaneSplit(cwd, split, focus)
	if err != nil {
		return nil, err
	}
	for _, n := range herdrAgentNameCandidates(name) {
		agent, err := herdrAgentStartWhenReady(n, kind, pane.PaneID, timeoutMs, agentArgs)
		if err == nil {
			return agent, nil
		}
		if herdrErrorCode(err) == "agent_name_taken" {
			continue
		}
		return nil, herdrStartFailed(pane.PaneID, err)
	}
	// 候補を使い切った = 一度も起動コマンドを送っていないので pane は空のまま。閉じてよい。
	_ = herdrPaneClose(pane.PaneID)
	return nil, fmt.Errorf("herdr agent start: 使える agent 名がありません (%s とその代替名がすべて使用中)", herdrSlugify(name))
}

// herdrPaneReadyTimeout / herdrPaneReadyInterval は split 直後の pane が対話シェルのプロンプトに
// 落ち着くまで待つ上限と再試行間隔 (テストで縮められるよう var)。
var (
	herdrPaneReadyTimeout  = 15 * time.Second
	herdrPaneReadyInterval = 200 * time.Millisecond
)

// herdrAgentStartWhenReady は pane が対話シェルのプロンプトに落ち着くのを待ちながら agent を起動する。
// split した直後の pane はまだシェルの起動中で、herdr の agent start は「使えるシェルではない」と
// agent_pane_busy で即座に断る。herdr 側に pane の ready 待ちが無いのでここで埋める。
//
// 再試行してよい根拠: agent_pane_busy は**起動コマンドを送る前**の拒否なので、繰り返しても
// agent が二重に立ち上がることはない。
func herdrAgentStartWhenReady(name, kind, pane string, timeoutMs int, agentArgs []string) (*herdrPane, error) {
	deadline := time.Now().Add(herdrPaneReadyTimeout)
	for {
		agent, err := herdrAgentStart(name, kind, pane, timeoutMs, agentArgs)
		if herdrErrorCode(err) != "agent_pane_busy" || !time.Now().Before(deadline) {
			return agent, err
		}
		time.Sleep(herdrPaneReadyInterval)
	}
}

// herdrStartFailed は 2 段起動の後始末を決める。**起動前に確実に失敗した**とき (名前・kind・pane の
// 検証エラー) だけ、作った空 pane を閉じる。timeout や未知の失敗では agent が遅れて立ち上がって
// いるかもしれないので閉じず、pane id を添えて返す — 生きている agent を巻き添えに殺すより、
// 空 pane が 1 つ残るほうが害が小さい。
func herdrStartFailed(pane string, err error) error {
	switch herdrErrorCode(err) {
	case "invalid_agent_name", "agent_name_taken", "invalid_agent_kind", "pane_not_found", "agent_pane_busy":
		_ = herdrPaneClose(pane)
		return err
	}
	return fmt.Errorf("%w (pane %s は残しました。agent が遅れて起動しているかもしれません。不要なら herdr pane close %s)", err, pane, pane)
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
