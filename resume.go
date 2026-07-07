package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// resume は blocked / review にしたタスクの作業を再開するとき、status を in-progress に戻す。
//
// なぜ専用コマンドか: 状態を書き換える CLI は claim (todo→in-progress の着手予約) / done / block だけで、
// 一度 blocked / review にしたタスクを再び触るときに in-progress へ戻す正規の手段が無かった。結果として
// 一覧の status が blocked/review のまま残り、blocked_at/blocked_reason が居座って経過表示が実態とズレ、
// worktime も再開が記録されない、といった不整合が起きていた。resume は done/block と同じく scalar キーの
// 確定だけを project ロック下で決定的に行う (LLM の手編集による付け外し漏れを避ける)。
//
// claim との違い: claim は「新規着手の予約」で二重着手をガードする (in-progress ならエラー)。resume は
// 「既に着手済みのタスクの再開」なので、対象は blocked / review (と冪等な in-progress) に限る。worktree が
// 既に在る状態からの再開を想定するため、worktree を持たない todo は start へ、worktree を撤去済みの done は
// start での作り直しへ誘導する (どちらもエラーで案内)。
//
// セッションの同一化 (session-rename / session-link) は agent 固有・対話的なので skill の resume 手順に
// 置く (start と対称)。resume コマンド自体は frontmatter の scalar 確定のみで agent 中立。
//
// 使い方:
//
//	agent-tasks resume <id> | <project> <id> [--agent <name>] [--session <url>]
func cmdResume(args []string) error {
	var agent, session string
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--agent":
			if i+1 >= len(args) {
				return usagef("--agent には値が必要")
			}
			i++
			agent = args[i]
		case "--session":
			if i+1 >= len(args) {
				return usagef("--session には値が必要")
			}
			i++
			session = args[i]
		default:
			if v, ok := strings.CutPrefix(a, "--agent="); ok {
				agent = v
				continue
			}
			if v, ok := strings.CutPrefix(a, "--session="); ok {
				session = v
				continue
			}
			if strings.HasPrefix(a, "--") {
				return usagef("unknown option: %s", a)
			}
			pos = append(pos, a)
		}
	}

	project, id, err := resolveProjectID(pos)
	if err != nil {
		return err
	}

	// claim / done / block と同じ project ロックを共有する (同 project の状態遷移を直列化する)。
	projDir := filepath.Join(storeDir(), project)
	unlock, err := lockProject(projDir)
	if err != nil {
		return err
	}
	defer unlock()

	path, err := resolveTaskPath(project, id)
	if err != nil {
		return err
	}
	t, err := parseTask(path)
	if err != nil {
		return fmt.Errorf("タスクを読めません: %w", err)
	}

	// 再開できるのは「既に着手済みで worktree が残っている」状態だけ。todo/done は誘導する。
	switch t.Status {
	case "blocked", "review", "in-progress":
		// ok (in-progress は冪等: updated を更新して blocked_* を落とすだけ)。
	case "todo":
		return fmt.Errorf("%s/%s は todo です。未着手のタスクは start で着手してください (resume は blocked/review からの再開用)", project, normalizeID(id))
	case "done":
		return fmt.Errorf("%s/%s は done です。完了タスクの再開は worktree の作り直しが要るため start を使ってください", project, normalizeID(id))
	default:
		return fmt.Errorf("%s/%s は %s です。resume できるのは blocked/review のタスクだけです", project, normalizeID(id), statusOrUnknown(t.Status))
	}
	prev := t.Status

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339)

	// agent は明示 (--agent) が最優先、無ければ既存を保持、それも無ければ既定。
	// (done/block で agent が消えることは無いので通常は既存が残っている。)
	if agent == "" {
		agent = t.Agent
	}
	if agent == "" {
		agent = defaultAgent()
	}

	sets := []fmKV{{"status", "in-progress"}, {"agent", agent}}
	if session != "" {
		sets = append(sets, fmKV{"session", session})
	}
	if t.StartedAt == "" {
		// 通常 blocked/review には started_at が在るので保持される。万一欠けていれば再開時刻を入れる
		// (リードタイム算出の基点を欠かさない)。既存があれば上書きしない。
		sets = append(sets, fmKV{"started_at", now})
	}
	sets = append(sets, fmKV{"updated", now})
	// in-progress へ戻るので保留・完了マーカーは落とす (blocked からの再開 / 念のため completed_at も)。
	dels := []string{"blocked_at", "blocked_reason", "completed_at"}

	updated, err := applyFrontmatterEdits(content, sets, dels)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := atomicWriteFile(path, updated, 0o644); err != nil {
		return err
	}
	fmt.Printf("resumed %s/%s (in-progress, was %s)\n", project, normalizeID(id), prev)
	return nil
}
