package main

import (
	"fmt"
	"os"
	"strings"
)

// spawnAgentArgs は子セッションの agent に渡す**引数だけ**を組み立てる。実行ファイル名は herdr の
// --kind が決めるので含めない。claude は -n でセッション名を付けられる (web/アプリのセッション一覧で
// どのタスクか分かる)。他 agent は -n 非対応なので付けない (agent 非依存)。
func spawnAgentArgs(agent, label, prompt string) []string {
	if agent == "claude" {
		return []string{"-n", label, prompt}
	}
	return []string{prompt}
}

// spawnLabel / spawnPrompt は子セッションの表示ラベル / 初期プロンプトを組み立てる
// (cmdSpawn と TUI の spawn で共有し、表記を 1 箇所に集約する)。
func spawnLabel(t Task) string  { return fmt.Sprintf("task %s: %s", t.ID, t.Title) }
func spawnPrompt(t Task) string { return fmt.Sprintf("タスク %s に着手して", t.ID) }

// spawnAgentName は herdr に登録する agent 名 (スラッグ) を組み立てる。人向けの表示ラベル
// (spawnLabel = 空白・大文字・日本語を含む) は herdr の名前制約
// (小文字始まり / [a-z0-9_-] / 1-32 文字) を通らないので、別物として持つ。
// project を混ぜるのは別 project の同じ id とぶつからないようにするため。32 文字の枠は
// **project 側だけを詰めて**守る (id は識別の要なので落とさない)。
func spawnAgentName(t Task) string {
	proj := herdrSlugify(t.Project)
	if budget := max(herdrAgentNameMax-len("task-")-1-len(t.ID), 0); len(proj) > budget {
		proj = proj[:budget]
	}
	return herdrSlugify("task-" + proj + "-" + t.ID)
}

// spawnTask は別 pane で新セッションを開き、対象タスクに着手させる spawn の中核 (fire-and-forget)。
// cmdSpawn (CLI) と TUI の spawn キーの両方から使う。親は pane を開いて指示を送ったら忘れてよい
// (worktree 作成・session-link・frontmatter 確定は子の start が行う)。成功すると起動された pane を返す。
//   - herdr 前提 (全面移行)。herdr 外なら分かりやすいエラー。
//   - 二重着手ガード: in-progress + session ありは別セッション作業中の可能性。force=false ならエラー。
//   - メインリポ root は cwd から解決する (cwd が worktree 内でも git-common-dir の親で解決)。
//     子の start がまだ無い worktree を作るので pane の cwd は main root。
func spawnTask(t Task, split string, focus, force bool) (*herdrPane, error) {
	if err := requireHerdr(); err != nil {
		return nil, fmt.Errorf("spawn は herdr 内で実行してください: %w", err)
	}
	if t.Status == "in-progress" && t.Session != "" && !force {
		return nil, fmt.Errorf("タスク %s/%s は既に in-progress (session: %s)。別セッションが作業中かもしれません。"+
			"引き継ぐ/再着手するなら --force を付けてください。", t.Project, t.ID, t.Session)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	root, err := mainRepoOf(cwd)
	if err != nil {
		return nil, fmt.Errorf("メインリポ root を特定できません (git リポジトリ内で実行してください): %w", err)
	}
	// agent 種別は AGENT_TASKS_AGENT (既定 claude) をそのまま herdr の --kind に渡す。
	// claude は -n でセッション名を付けられる (web/アプリのセッション一覧でタスクが分かる)。
	kind := defaultAgent()
	args := spawnAgentArgs(kind, spawnLabel(t), spawnPrompt(t))
	// timeoutMs=0 = herdr 既定 (30s) の起動待ち。agent start は「その pane で agent が検出され
	// 入力を受け付けられる状態になる」まで待つので、spawn は fire-and-forget のまま**起動の検証**まで
	// 済ませられる (親はここから先、子の進行をポーリングしない)。
	return herdrStartAgentInNewPane(spawnAgentName(t), kind, root, split, focus, 0, args)
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

	pane, err := spawnTask(t, split, focus, force)
	if err != nil {
		return err
	}

	fmt.Printf("spawned %s → pane %s\n", spawnLabel(t), pane.PaneID)
	fmt.Printf("  子セッションが起動し「%s」で start します (worktree 作成・追跡は子が行う)。\n", spawnPrompt(t))
	fmt.Println("  起動確認: agent-tasks --watch --status in-progress")
	return nil
}
