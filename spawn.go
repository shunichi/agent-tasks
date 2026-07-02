package main

import (
	"fmt"
	"os"
	"strings"
)

// spawnArgv は子セッションの起動コマンド argv を組み立てる。claude は -n でセッション名を付けられる
// (web/アプリのセッション一覧でどのタスクか分かる)。他 agent は -n 非対応なので付けない (agent 非依存)。
func spawnArgv(agent, label, prompt string) []string {
	if agent == "claude" {
		return []string{agent, "-n", label, prompt}
	}
	return []string{agent, prompt}
}

// cmdSpawn は別 pane で新しい agent セッションを開き、対象タスクに着手させる (fire-and-forget)。
// herdr 全面移行 (0105/0108) 版。旧来の tmux `split-window` + `send-keys` ハック (SKILL 手順) を
// herdr `agent start` に置き換えたもの。親は pane を開いて指示を送ったら忘れてよい
// (worktree 作成・session-link・frontmatter 確定は子の start が行う)。
//
//		agent-tasks spawn <NNNN> | <project> <id> [--split right|down] [--focus] [--force]
//
//	  - **メインリポ root で開く**: 子の start がまだ無い worktree を作るので pane の cwd は worktree に
//	    できない。セッション追跡は session-link (session_id ベース) なので cwd がメインリポでよく、
//	    子が done で worktree を消しても自分の足元を消さない (安全)。
//	  - **二重着手ガード**: 対象が in-progress + session ありなら別セッション作業中の可能性。--force で上書き。
func cmdSpawn(args []string) error {
	split := "down"
	focus := false
	force := false

	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch {
		case a == "--split":
			v, err := s.value("--split")
			if err != nil {
				return err
			}
			if v != "right" && v != "down" {
				return usagef("--split は right か down: %q", v)
			}
			split = v
		case a == "--focus":
			focus = true
		case a == "--force":
			force = true
		default:
			if v, ok := strings.CutPrefix(a, "--split="); ok {
				if v != "right" && v != "down" {
					return usagef("--split は right か down: %q", v)
				}
				split = v
				continue
			}
			s.positional(a)
		}
	}

	// herdr 前提 (全面移行)。herdr 外なら分かりやすく止める。
	if err := requireHerdr(); err != nil {
		return fmt.Errorf("spawn は herdr 内で実行してください: %w", err)
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

	// 二重着手ガード: in-progress + session ありは別セッション作業中の可能性。
	if t.Status == "in-progress" && t.Session != "" && !force {
		return fmt.Errorf("タスク %s/%s は既に in-progress (session: %s)。別セッションが作業中かもしれません。"+
			"引き継ぐ/再着手するなら --force を付けてください。", t.Project, t.ID, t.Session)
	}

	// メインリポ root を求める (cwd が worktree 内でも git-common-dir の親で解決)。
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := mainRepoOf(cwd)
	if err != nil {
		return fmt.Errorf("メインリポ root を特定できません (git リポジトリ内で実行してください): %w", err)
	}

	// 子に渡す起動コマンドを組み立てる。agent は AGENT_TASKS_AGENT (既定 claude)。
	// claude は -n でセッション名を付けられる (web/アプリのセッション一覧でタスクが分かる)。
	// 他 agent は -n 非対応なので付けない (agent 非依存)。
	label := fmt.Sprintf("task %s: %s", t.ID, t.Title)
	prompt := fmt.Sprintf("タスク %s に着手して", t.ID)
	argv := spawnArgv(defaultAgent(), label, prompt)

	pane, err := herdrAgentStart(label, root, split, focus, argv)
	if err != nil {
		return fmt.Errorf("herdr で pane を起動できませんでした: %w", err)
	}

	fmt.Printf("spawned %s → pane %s (cwd: %s)\n", label, pane.PaneID, root)
	fmt.Printf("  子セッションが起動し「%s」で start します (worktree 作成・追跡は子が行う)。\n", prompt)
	fmt.Println("  起動確認: agent-tasks --watch --status in-progress")
	return nil
}
