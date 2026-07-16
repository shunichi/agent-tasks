package main

import (
	"fmt"
	"os"
	"os/exec"
)

// cmdSessionRename は「**セッション自体**の名前を対象タスク名 (`task <NNNN>: <title>`) に変える」。
// `/rename` スラッシュコマンドは **Claude Code と codex の両方が対応** ("rename the current thread")
// しており、agent 自身はスラッシュコマンドをツールから直接実行できない。そこで**自分の pane の入力欄に
// `/rename …` を打ち込んで発火**させる。本物の /rename 経路を通るので web / スマホアプリのセッション名にも
// 反映される。送出内容 (`/rename …`) は agent 非依存なので、Claude/codex どちらの pane でもそのまま効く。
//
// (0152→0153 の経緯) 一時期「codex に /rename が無い」と誤認して非 Claude では no-op 化したが、codex にも
// /rename があると判明したため gating を撤回し、両対応の元の挙動に戻した。/rename を持たない別 agent が
// 出てきたら、そのとき改めて分岐を検討する (現状の対応 agent はいずれも /rename を持つ)。
//
// herdr 移行 (0111): **herdr 内なら `herdr pane run` で自 pane (`HERDR_PANE_ID`) に打ち込む**
// (tmux 非依存)。herdr 外は従来どおり tmux `send-keys` にフォールバック。tmux も無ければ
// `/rename …` 行を stdout に出してユーザーに実行してもらう。
// いずれの経路も「セッション名を変える」= 同じゴール (herdr 内ラベルだけを変える agent rename とは別)。
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
