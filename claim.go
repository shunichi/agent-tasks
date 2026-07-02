package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// claim は start 着手の「in-progress 予約」を project ロック下で原子的に行う。
//
// なぜ必要か (TOCTOU): 旧 start 手順は frontmatter を in-progress に確定するのが worktree 作成の
// 後だった。着手指示から in-progress 確定までに worktree 作成・post-create フック・LLM 思考ぶんの
// 長い窓があり、その間に別セッションが同じ project を start すると、双方のコンフリクトチェックが
// 互いを in-progress として観測できず素通りしていた。これは create の id 採番で起きていた TOCTOU
// (alloc-id で解決済み) と同じ構造。claim は alloc-id と同じ project ロック下で「読む →
// 二重着手をレースなく判定 → in-progress を書き戻す」を原子的に行い、窓を「ロック下の一瞬の書き込み」
// まで縮める。start 手順は「タスク特定 → claim → コンフリクトチェック → worktree → session-link」の順にする。
//
// 使い方:
//
//	agent-tasks claim <id> | <project> <id> [--agent <name>] [--session <url>] [--force]
//	agent-tasks claim <id> --release        # 直後にコンフリクトで「やめる」ときに todo へ戻す
//
// 書き換えるのは frontmatter の既知キーだけ (行単位)。本文 (# 要件 / ## 進捗ログ / コメント) は
// 保全する。進捗ログの追記など非クリティカルな整形は従来どおり skill 側が担う。
func cmdClaim(args []string) error {
	var agent, session string
	releaseTo := "todo" // --release の既定の戻し先
	var pos []string
	release, force := false, false
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
		case "--release":
			release = true
		case "--to":
			if i+1 >= len(args) {
				return usagef("--to には値が必要")
			}
			i++
			releaseTo = args[i]
			release = true // --to は release 専用 (指定されたら release とみなす)
		case "--force":
			force = true
		default:
			if v, ok := strings.CutPrefix(a, "--agent="); ok {
				agent = v
				continue
			}
			if v, ok := strings.CutPrefix(a, "--session="); ok {
				session = v
				continue
			}
			if v, ok := strings.CutPrefix(a, "--to="); ok {
				releaseTo = v
				release = true
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

	// alloc-id と同じ project ロックを共有する (claim 同士・claim と alloc を直列化する)。
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
	var dels []string
	var doneMsg string

	if release {
		// claim の取り消し: in-progress を指定の状態 (既定 todo) に戻す。戻し先は skill が --to で選べる
		// (例: blocked から再開して claim した後にやめるなら --to blocked)。
		if t.Status != "in-progress" {
			return fmt.Errorf("release できるのは in-progress のタスクだけです (%s/%s は %s)", project, normalizeID(id), statusOrUnknown(t.Status))
		}
		if !isReleaseTarget(releaseTo) {
			return usagef("--to は %s のいずれか (got %q)", strings.Join(releaseTargets, "|"), releaseTo)
		}
		sets = []fmKV{{"status", releaseTo}, {"updated", now}}
		if releaseTo == "todo" {
			// 未着手へ戻すので claim が付けた着手情報も落とす (誰も着手していない状態に戻す)。
			sets = append(sets, fmKV{"agent", ""}, fmKV{"session", ""})
			dels = []string{"started_at"}
		}
		// todo 以外 (blocked/review/done) は着手情報を保持する (実際に着手済みなので)。
		// blocked_reason/blocked_at・completed_at 等の付随情報は skill 側で調整する。
		doneMsg = fmt.Sprintf("released %s/%s (%s)", project, normalizeID(id), releaseTo)
	} else {
		// 二重着手ガード (レースフリー: ロック下で status を見て判定)。
		if t.Status == "in-progress" && !force {
			sameSession := session != "" && session == t.Session
			if !sameSession {
				who := t.Agent
				if who == "" {
					who = "?"
				}
				return fmt.Errorf("%s/%s は既に in-progress です (agent=%s)。再着手/引き継ぎは --force を、別タスクは別 id を指定してください", project, normalizeID(id), who)
			}
		}
		if agent == "" {
			agent = defaultAgent()
		}
		sets = []fmKV{{"status", "in-progress"}, {"agent", agent}}
		if session != "" {
			sets = append(sets, fmKV{"session", session})
		}
		if t.StartedAt == "" {
			// 初回着手のみ記録し、再 claim では上書きしない (リードタイムを正しく測るため)。
			sets = append(sets, fmKV{"started_at", now})
		}
		sets = append(sets, fmKV{"updated", now})
		// in-progress へ入るので保留・完了マーカーは落とす
		// (blocked からの再開 / done からの再オープンを 1 操作で正す)。
		dels = []string{"blocked_at", "blocked_reason", "completed_at"}
		doneMsg = fmt.Sprintf("claimed %s/%s (in-progress)", project, normalizeID(id))
	}

	updated, err := applyFrontmatterEdits(content, sets, dels)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := atomicWriteFile(path, updated, 0o644); err != nil {
		return err
	}
	fmt.Println(doneMsg)
	return nil
}

// defaultAgent は claim で記録する agent 名の既定値。AGENT_TASKS_AGENT、未設定なら "claude"。
// spawn の初期プロンプト送出 (AGENT_TASKS_AGENT) と揃える。
func defaultAgent() string {
	if v := os.Getenv("AGENT_TASKS_AGENT"); v != "" {
		return v
	}
	return "claude"
}

// releaseTargets は --release の戻し先として許可する status。in-progress への release は無意味なので除く。
var releaseTargets = []string{"todo", "blocked", "review", "done"}

func isReleaseTarget(s string) bool {
	for _, v := range releaseTargets {
		if s == v {
			return true
		}
	}
	return false
}

func statusOrUnknown(s string) string {
	if s == "" {
		return "(status なし)"
	}
	return s
}

// fmKV は frontmatter の 1 キーへの set 指示 (val が空なら "key:" と空値で書く)。
type fmKV struct{ key, val string }

// applyFrontmatterEdits はタスク Markdown 先頭の frontmatter を行単位で書き換える。
//   - sets: 既存キーは値を差し替え、無いキーは終端 (---) の直前に追記する。
//   - dels: 該当する既存キー行を削除する (無ければ何もしない)。
//
// frontmatter 外 (本文) と、frontmatter 内のコメント行 (#...) / インデント行 (YAML ブロックリストの
// "  - item") は一切触らない。そのため scalar キーの編集にのみ使う (prs/tracker のようなブロック
// リストキー自体を sets/dels の対象にすると、残ったインデント項目が孤児化するので渡さないこと)。
func applyFrontmatterEdits(content []byte, sets []fmKV, dels []string) ([]byte, error) {
	lines := strings.Split(string(content), "\n")

	start := -1
	for i, ln := range lines {
		s := strings.TrimSpace(strings.TrimPrefix(ln, "\ufeff"))
		if s == "---" {
			start = i
			break
		}
		if s != "" {
			break // 先頭に非空・非 --- 行 → frontmatter なし
		}
	}
	if start == -1 {
		return nil, fmt.Errorf("frontmatter (先頭の ---) が見つかりません")
	}
	end := -1
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return nil, fmt.Errorf("frontmatter の終端 (---) が見つかりません")
	}

	delSet := make(map[string]bool, len(dels))
	for _, d := range dels {
		delSet[d] = true
	}
	setMap := make(map[string]string, len(sets))
	var setOrder []string
	for _, s := range sets {
		if _, seen := setMap[s.key]; !seen {
			setOrder = append(setOrder, s.key)
		}
		setMap[s.key] = s.val
	}
	applied := make(map[string]bool, len(sets))

	out := make([]string, 0, len(lines)+len(sets))
	out = append(out, lines[:start+1]...)
	for i := start + 1; i < end; i++ {
		ln := lines[i]
		trimmed := strings.TrimSpace(ln)
		// コメント行・インデント行 (リスト項目など) は素通し。
		if strings.HasPrefix(trimmed, "#") || (len(ln) > 0 && (ln[0] == ' ' || ln[0] == '\t')) {
			out = append(out, ln)
			continue
		}
		key, _, ok := strings.Cut(ln, ":")
		if !ok {
			out = append(out, ln)
			continue
		}
		k := strings.TrimSpace(key)
		if delSet[k] {
			continue // 行を削除
		}
		if v, ok := setMap[k]; ok {
			out = append(out, fmLine(k, v))
			applied[k] = true
			continue
		}
		out = append(out, ln)
	}
	for _, k := range setOrder {
		if !applied[k] {
			out = append(out, fmLine(k, setMap[k]))
		}
	}
	out = append(out, lines[end:]...)
	return []byte(strings.Join(out, "\n")), nil
}

// fmLine は frontmatter の 1 行を組み立てる。空値は "key:"、値に ':' 等を含むならダブルクォートで囲む
// (時刻や URL は ':' を含むため。既存ファイルの表記に合わせる)。
func fmLine(key, val string) string {
	if val == "" {
		return key + ":"
	}
	if strings.ContainsAny(val, ":#") || val != strings.TrimSpace(val) {
		return key + ": \"" + val + "\""
	}
	return key + ": " + val
}
