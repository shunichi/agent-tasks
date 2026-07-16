package main

import (
	"fmt"
	"os"
	"os/exec"
)

// cmdSessionRename は「**Claude セッション自体**の名前を対象タスク名 (`task <NNNN>: <title>`) に変える」。
// Claude Code のセッション名は `/rename` スラッシュコマンドでしか変えられず、Claude 自身はスラッシュ
// コマンドをツールから直接実行できない。そこで**自分の pane の入力欄に `/rename …` を打ち込んで発火**
// させる。本物の /rename 経路を通るので claude.ai web / スマホアプリのセッション名にも反映される。
//
// herdr 移行 (0111): **herdr 内なら `herdr pane run` で自 pane (`HERDR_PANE_ID`) に打ち込む**
// (tmux 非依存)。herdr 外は従来どおり tmux `send-keys` にフォールバック。tmux も無ければ
// `/rename …` 行を stdout に出してユーザーに実行してもらう。
// いずれの経路も「Claude セッション名を変える」= 同じゴール (herdr 内ラベルだけを変える agent rename とは別)。
//
// 送信の確実性 (0131): 旧実装は `pane send-text` (文字列注入) と `pane send-keys Enter` を**別々の
// herdr 呼び出し 2 回**に分けていたが、2 プロセス起動の間隔が可変で、Enter が「送信」ではなく入力欄への
// 「改行」として食われることが (負荷/タイミング依存で) あった。`pane run` は文字列 + 本物の Enter を
// **1 リクエストでアトミックに**送る (herdr が順序をサーバ側で保証) ので、この 2 呼び出し間のレースが
// 原理的に無くなる。herdr の recipe でも claude への入力送出 (submit) はこの `pane run` を使う。
//
// skill の start / batch から「着手指示の直後 (タスク特定・二重着手チェックの直後、worktree より
// 前)」に 1 回呼ぶ想定。自 pane の入力欄に打つので、**入力欄が空なうち (指示直後) が最も
// 競合しにくい**。打ち込まれた `/rename` はこのターン終了後に実行される (現在の作業は壊さない)。
func cmdSessionRename(args []string) error {
	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		s.positional(a)
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

	// `/rename` は Claude Code 固有のスラッシュコマンド。codex 等 /rename を持たない agent で送出すると
	// `/rename task …` が入力欄に混入して誤送信になるため、**自セッションが Claude と確証できるときだけ**
	// 送出する (それ以外は no-op で return nil = start フローは止めない)。中立化した session-link/状態表示と
	// 違い、この操作だけは Claude 固有機能に依存するので agent を見て分岐する。
	if !isClaudeSession() {
		fmt.Fprintln(os.Stderr, "session-rename: Claude セッションでないためスキップしました "+
			"(/rename は Claude Code 固有。codex 等ではセッション名の追従は行いません)")
		return nil
	}

	renameCmd := "/rename " + sessionRenameName(t)

	// herdr 内 (主経路): 自 pane の入力欄へ /rename + Enter を pane run で 1 リクエスト送出し発火。
	// text と Enter を分けず 1 リクエストにすることで 2 呼び出し間のレース (Enter が改行として
	// 食われる) を避ける (上のコメント 0131 参照)。tmux 非依存。
	if herdrEnabled() {
		pane := herdrPaneID()
		if pane == "" {
			return fmt.Errorf("HERDR_PANE_ID が未設定で自 pane を特定できません")
		}
		if err := herdrPaneRun(pane, renameCmd); err != nil {
			return fmt.Errorf("herdr pane run に失敗: %w", err)
		}
		fmt.Fprintf(os.Stderr, "セッション名を送信しました (herdr): %s (pane %s)\n", sessionRenameName(t), pane)
		return nil
	}

	// herdr 外フォールバック: tmux send-keys /rename。
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		// tmux も無い: 自動送信できないので、ユーザーが実行できる形で出す。
		fmt.Println(renameCmd)
		fmt.Fprintln(os.Stderr, "(herdr/tmux 外のため自動送信できません。上の行を実行するとセッション名が変わります)")
		return nil
	}
	// 自分の pane へリテラル送信 (-l) → Enter。exec 経由なのでシェルのクォート不要で、
	// title に `:` / 空白 / 引用符が含まれても安全に送れる。
	if err := exec.Command("tmux", "send-keys", "-t", pane, "-l", renameCmd).Run(); err != nil {
		return fmt.Errorf("tmux send-keys (文字列) に失敗: %w", err)
	}
	if err := exec.Command("tmux", "send-keys", "-t", pane, "Enter").Run(); err != nil {
		return fmt.Errorf("tmux send-keys (Enter) に失敗: %w", err)
	}
	fmt.Fprintf(os.Stderr, "セッション名を送信しました (tmux): %s (pane %s)\n", sessionRenameName(t), pane)
	return nil
}

// sessionRenameName は送出する新しいセッション名を返す (`task <NNNN>: <title>`)。
// タスク名 (`タスク`) より少しだけ表示幅が狭い英語表記にしている。
func sessionRenameName(t Task) string {
	return fmt.Sprintf("task %s: %s", t.ID, t.Title)
}

// isClaudeSession は「今このコマンドを呼んでいるセッションが Claude か」を判定する。
// session-rename の `/rename` 送出可否に使う (Claude 固有機能なので fail-safe: 確証がなければ false)。
//
//   - `CLAUDE_CODE_SESSION_ID` が空でなければ Claude (Claude Code が herdr 内外で export する)。
//   - それが無くても herdr 内なら自 pane の agent 種別 (`herdrSelfAgent().Agent`) が "claude" なら Claude。
//
// どちらでも確証できなければ false を返す (codex 等 = /rename を撃たない)。
// なお `AGENT_TASKS_AGENT` は spawn する子の agent 指定であって自セッションの種別ではないので、
// ここでは見ない (Claude から codex 子を spawn する設定でも自分の rename は撃てるべき)。
func isClaudeSession() bool {
	if os.Getenv("CLAUDE_CODE_SESSION_ID") != "" {
		return true
	}
	if herdrEnabled() {
		if self, err := herdrSelfAgent(); err == nil && self.Agent == "claude" {
			return true
		}
	}
	return false
}
