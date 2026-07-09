package main

import "fmt"

// focus.go — 実行中タスクの herdr pane へフォーカスを移す (0047)。
//
// spawn で別 pane に散らばった作業セッションへ、一覧 (list / tui) から素早く飛ぶための導線。
// 特に blocked (承認/許可待ち) のタスクへ即座に飛べると、複数 pane を回すときの取りこぼしが減る。
//
// pane 特定は状態算出 (session.go の taskSessionState) と同じ突合を再利用する:
//   task → link (session-link が記録した session_id) → herdr agent list (session_id → pane) →
//   その pane_id を herdr agent focus に渡す。
// workspace / tab / pane をまたぐ切り替えは herdr の focus が内部で面倒を見るので、CLI 側で
// select-pane / switch-window を組み立てる必要はない。
//
// herdr 前提 (案A の全面移行方針)。herdr 外 (HERDR_ENV≠1) は requireHerdr でエラー終了する
// (黙って tmux にフォールバックしない)。

// resolveTaskPane は task t に紐づく herdr pane を特定する。link 未記録 / pane 終了 (ended) は
// それぞれ分かるエラーで返す (呼び出し側がそのまま表示できる文面にする)。
func resolveTaskPane(t Task) (herdrPane, error) {
	if t.IsHuman() {
		return herdrPane{}, fmt.Errorf("人手タスク (kind: human) はセッション/pane を持ちません")
	}
	key := taskSessionKey(t)
	if key == "" {
		return herdrPane{}, fmt.Errorf("worktree が未記録のため pane を特定できません (未着手?)")
	}
	link, ok := readSessionLink(key)
	if !ok {
		return herdrPane{}, fmt.Errorf("セッションが未リンクです (session-link されていません)。着手した pane で start/resume するとリンクされます")
	}
	agents, err := herdrAgentList()
	if err != nil {
		return herdrPane{}, err
	}
	// 最新セッション優先で突合する (link.SessionID が最新)。taskSessionState と同じ方針。
	ids := append([]string{link.SessionID}, linkSessionIDs(link)...)
	for _, sid := range ids {
		if sid == "" {
			continue
		}
		for i := range agents {
			if agents[i].AgentSession.Value == sid {
				return agents[i], nil
			}
		}
	}
	// link はあるが herdr に該当 agent 無し = pane 終了 (ended。list の SESSION 列と整合)。
	return herdrPane{}, fmt.Errorf("pane は終了済みです (ended)。resume / start で作業を再開できます")
}

// focusTaskPane は task t を実行中の herdr pane にフォーカスを移し、その pane id を返す。
// CLI の focus コマンドと TUI の f キーが共有する。特定できないときは resolveTaskPane の
// 分かりやすいエラーをそのまま返す。
func focusTaskPane(t Task) (string, error) {
	if err := requireHerdr(); err != nil {
		return "", err
	}
	pane, err := resolveTaskPane(t)
	if err != nil {
		return "", err
	}
	if err := herdrAgentFocus(pane.PaneID); err != nil {
		return "", err
	}
	return pane.PaneID, nil
}

// cmdFocus は `agent-tasks focus [<project>] <id>` を処理する。現在 project 既定、別 project は
// `focus <project> <id>` (resolveProjectID の規約に従う)。
func cmdFocus(args []string) error {
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
	pane, err := focusTaskPane(t)
	if err != nil {
		return err
	}
	fmt.Printf("focus: %s/%s → pane %s\n", t.Project, t.ID, pane)
	return nil
}
