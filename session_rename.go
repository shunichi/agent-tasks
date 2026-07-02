package main

import (
	"fmt"
	"os"
	"os/exec"
)

// cmdSessionRename は「このセッションの表示名を対象タスク名 (`task <NNNN>: <title>`) に変える」。
//
// herdr 移行 (0111): **herdr 内なら `herdr agent rename <自 pane> <name>`** で自 pane の表示ラベルを
// 付ける (send-keys 不要・tmux 非依存)。herdr のサイドバー / モバイル attach に「今どのタスクか」が
// 出るので、元々の目的 (並行セッションを一覧で識別する) を満たす。
// ⚠️ これは **herdr 内ラベル**で、claude.ai web/アプリのセッション名は変わらない (Claude の `/rename`
// 経路とは別 surface)。web/アプリ側の命名が要るときは spawn 子の `claude -n` (起動時命名) で付く。
//
// herdr 外フォールバック: 従来どおり tmux 内なら**自分の pane** (`$TMUX_PANE`) へ `send-keys` で
// `/rename …` を打ち込む (本物の /rename 経路 = claude.ai 名も更新)。tmux 外なら `/rename …` 行を
// stdout に出してユーザーに実行してもらう。
//
// skill の start / batch から「着手指示の直後 (タスク特定・二重着手チェックの直後、worktree より
// 前)」に 1 回呼ぶ想定。id 指定だけで title 解決〜送出まで CLI 内で完結する。
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
	name := sessionRenameName(t)

	// herdr 内: 自 pane の herdr ラベルを設定する (主経路)。
	if herdrEnabled() {
		pane := herdrPaneID()
		if pane == "" {
			return fmt.Errorf("HERDR_PANE_ID が未設定で自 pane を特定できません")
		}
		if err := herdrAgentRename(pane, name); err != nil {
			return fmt.Errorf("herdr agent rename に失敗: %w", err)
		}
		fmt.Fprintf(os.Stderr, "herdr ラベルを設定しました: %s (pane %s)\n", name, pane)
		return nil
	}

	// herdr 外フォールバック: tmux send-keys /rename (claude.ai 名も更新) / 手実行案内。
	renameCmd := "/rename " + name
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
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
	fmt.Fprintf(os.Stderr, "セッション名を送信しました: %s (pane %s)\n", name, pane)
	return nil
}

// sessionRenameName は送出する新しいセッション名を返す (`task <NNNN>: <title>`)。
// タスク名 (`タスク`) より少しだけ表示幅が狭い英語表記にしている。
func sessionRenameName(t Task) string {
	return fmt.Sprintf("task %s: %s", t.ID, t.Title)
}
