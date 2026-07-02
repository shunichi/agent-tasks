package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// done / block は skill の done / block 手順の「frontmatter 確定」部分を CLI に寄せたもの。
// claim (start の in-progress 予約) と同じ設計思想: LLM が frontmatter を手編集すると
// completed_at 付け忘れ・blocked_at/blocked_reason の付け外し漏れ・日付推測ミスが起きやすいので、
// クリティカルな scalar キーの確定だけを project ロック下で決定的に行う。
//
// CLI が担うのは frontmatter の scalar キーのみ。判断・整形を伴う部分は skill 側に残す:
//   - worktree 撤去 (worktree-remove) / PR 作成 / コミット
//   - 進捗ログ (## 進捗ログ) への追記文言
//   - prs: / tracker: (YAML ブロックリスト。applyFrontmatterEdits は scalar 専用なので触らない)

// cmdDone は done / review 遷移の frontmatter を確定する。
//
//	agent-tasks done <id> | <project> <id> [--review]
//
// 既定 (--review 無し): status=done、completed_at を現在時刻で記録 (未設定時のみ = 初回完了を保持)。
// --review: status=review にし completed_at は付けない (done になった時点で付ける方針を踏襲)。
// どちらも保留マーカー (blocked_at/blocked_reason) は落とす (blocked から直接 done/review する場合の後始末)。
func cmdDone(args []string) error {
	review := false
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--review":
			review = true
		default:
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

	// claim / alloc-id と同じ project ロックを共有する (同 project の状態遷移を直列化する)。
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
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339)

	var sets []fmKV
	// blocked から直接抜ける場合の後始末 (もう保留ではない)。
	dels := []string{"blocked_at", "blocked_reason"}
	var msg string
	if review {
		sets = []fmKV{{"status", "review"}, {"updated", now}}
		msg = fmt.Sprintf("done %s/%s (review)", project, normalizeID(id))
	} else {
		sets = []fmKV{{"status", "done"}}
		if t.CompletedAt == "" {
			// 完了日時は初回のみ記録し、再 done では上書きしない (最初の完了を保持する)。
			sets = append(sets, fmKV{"completed_at", now})
		}
		sets = append(sets, fmKV{"updated", now})
		msg = fmt.Sprintf("done %s/%s (done)", project, normalizeID(id))
	}

	updated, err := applyFrontmatterEdits(content, sets, dels)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := atomicWriteFile(path, updated, 0o644); err != nil {
		return err
	}
	fmt.Println(msg)
	return nil
}

// cmdBlock は block 遷移の frontmatter を確定する。
//
//	agent-tasks block <id> | <project> <id> --reason <理由>
//
// status=blocked、blocked_at を現在時刻で記録、blocked_reason に理由を入れる (list が経過と理由を出す)。
// --reason は必須 (一覧で理由を表示するため。履歴としての理由は skill が ## 進捗ログ にも残す)。
func cmdBlock(args []string) error {
	var reason string
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--reason":
			if i+1 >= len(args) {
				return usagef("--reason には値が必要")
			}
			i++
			reason = args[i]
		default:
			if v, ok := strings.CutPrefix(a, "--reason="); ok {
				reason = v
				continue
			}
			if strings.HasPrefix(a, "--") {
				return usagef("unknown option: %s", a)
			}
			pos = append(pos, a)
		}
	}
	if strings.TrimSpace(reason) == "" {
		return usagef("block には --reason <理由> が必要 (一覧に表示する保留理由)")
	}

	project, id, err := resolveProjectID(pos)
	if err != nil {
		return err
	}

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
	if _, err := parseTask(path); err != nil {
		return fmt.Errorf("タスクを読めません: %w", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339)

	sets := []fmKV{
		{"status", "blocked"},
		{"blocked_at", now},
		{"blocked_reason", reason},
		{"updated", now},
	}
	// blocked は完了ではないので、done から保留へ戻すような場合は completed_at を落とす。
	dels := []string{"completed_at"}

	updated, err := applyFrontmatterEdits(content, sets, dels)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := atomicWriteFile(path, updated, 0o644); err != nil {
		return err
	}
	fmt.Printf("blocked %s/%s (%s)\n", project, normalizeID(id), reason)
	return nil
}
