package main

import (
	"fmt"
	"os"
	"os/exec"
)

// cmdSessionRename は「このセッションの名前を対象タスク名 (`task <NNNN>: <title>`) に変える」。
// Claude Code のセッション名は `/rename` でしか変えられず、Claude 自身はスラッシュコマンドを
// ツールから直接実行できない。そこで tmux 内なら**自分の pane** (`$TMUX_PANE`) へ `send-keys` で
// `/rename …` を打ち込んで発火させる。本物の `/rename` 経路を通るので web / スマホアプリの
// セッション名 (Bridge 同期先) にも反映される。tmux 外では発火できないので、ユーザーが実行
// できるよう `/rename …` 行を stdout に出すフォールバックにする。
//
// skill の start / batch から「着手指示の直後 (タスク特定・二重着手チェックの直後、worktree より
// 前)」に 1 回呼ぶ想定。id 指定だけで title 解決〜送出まで CLI 内で完結させ、Claude 側の手数
// (= 入力欄が空でなくなり send-keys と競合する窓) を最小にする。
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

	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		// tmux 外: 自動送信できないので、ユーザーが実行できる形で出す (フォールバック)。
		fmt.Println(renameCmd)
		fmt.Fprintln(os.Stderr, "(tmux 外のため自動送信できません。上の行を実行するとセッション名が変わります)")
		return nil
	}

	// 自分の pane へリテラル送信 (-l) → Enter。exec 経由なのでシェルのクォート不要で、
	// title に `:` / 空白 / 引用符が含まれても安全に送れる。send-keys された `/rename` は
	// このターン終了後に入力として実行される (現在の作業は壊さない)。
	if err := exec.Command("tmux", "send-keys", "-t", pane, "-l", renameCmd).Run(); err != nil {
		return fmt.Errorf("tmux send-keys (文字列) に失敗: %w", err)
	}
	if err := exec.Command("tmux", "send-keys", "-t", pane, "Enter").Run(); err != nil {
		return fmt.Errorf("tmux send-keys (Enter) に失敗: %w", err)
	}
	fmt.Fprintf(os.Stderr, "セッション名を送信しました: %s (pane %s)\n", sessionRenameName(t), pane)
	return nil
}

// sessionRenameName は送出する新しいセッション名を返す (`task <NNNN>: <title>`)。
// タスク名 (`タスク`) より少しだけ表示幅が狭い英語表記にしている。
func sessionRenameName(t Task) string {
	return fmt.Sprintf("task %s: %s", t.ID, t.Title)
}
