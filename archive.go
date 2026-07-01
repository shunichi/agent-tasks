package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// archive / unarchive は不要になったタスクを <project>/archive/ へ退避 / 復帰させる。
//
// 退避は「削除」ではない (内容は残す)。ただし通常の走査 (loadTasks) は archive/ を読まないので、
// list / -a / doctor の通常表示からは消える。明示的に見たいときだけ list --archived /
// show --archived で読める。status は変えない — 退避は状態変更ではなく「見える場所の移動」。
//
// ストアの git 同期 (commit/push) はここでは行わない (sync の責務)。リネームは
// 「旧パスの削除 + 新パスの追加」なので、両方を取りこぼさず stage できるよう、移動結果に
// from:/to: の store 相対パスを出力する。skill はこれを
// `agent-tasks sync --path <from> --path <to>` に渡して scoped に同期する。

// cmdArchive はアクティブなタスクを <project>/archive/ へ退避する。
func cmdArchive(args []string) error {
	project, id, err := parseProjectIDArgs(args)
	if err != nil {
		return err
	}
	src, err := resolveTaskPath(project, id)
	if err != nil {
		// 既にアーカイブ済みなら、その旨を分かりやすく伝える。
		if _, aerr := resolveTaskPathIn(project, id, archiveDirName); aerr == nil {
			return fmt.Errorf("%s / %s は既にアーカイブ済みです", project, normalizeID(id))
		}
		return err
	}
	dest, err := moveTaskToArchive(project, src)
	if err != nil {
		return err
	}
	reportMove("archived", project, id, src, dest)
	return nil
}

// moveTaskToArchive は src (アクティブなタスクファイルの絶対パス) を <project>/archive/ へ移し、
// 移動先の絶対パスを返す。archive ディレクトリが無ければ作る。退避先に同名ファイルが既にあれば
// 取り違え防止でエラーにする。単一退避 (cmdArchive) と一括退避 (cmdAutoArchive) で共有する。
func moveTaskToArchive(project, src string) (dest string, err error) {
	archiveDir := filepath.Join(storeDir(), project, archiveDirName)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return "", fmt.Errorf("アーカイブディレクトリを作成できません: %w", err)
	}
	dest = filepath.Join(archiveDir, filepath.Base(src))
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("退避先に同名ファイルが既にあります: %s", dest)
	}
	if err := os.Rename(src, dest); err != nil {
		return "", fmt.Errorf("アーカイブに失敗しました: %w", err)
	}
	return dest, nil
}

// autoArchiveDefaultDays は auto-archive の既定閾値 (完了後この日数を過ぎた done を対象にする)。
const autoArchiveDefaultDays = 30

// cmdAutoArchive は「完了後に一定期間が経過した done タスク」を一括で <project>/archive/ へ退避する。
// 一覧に古い完了タスクが溜まって見づらくなるのを防ぐ定期メンテ用。単一退避の archive と違い、
// 期間で対象を選んで複数まとめて動かす。対象は status:done かつ completed_at が閾値より古いものだけ
// (review / in-progress / completed_at 無しは対象外)。スコープは list と同じ規則
// (既定は現在 project、--all-projects で横断、--project/--projects で指定)。
// --dry-run で対象を表示するだけにできる (破壊的操作なので事前確認できる)。
//
// 実行時は退避した各タスクを archive と同じ from:/to: 形式で出力するので、skill は全 from:/to: を
// まとめて `agent-tasks sync --path ...` に渡して scoped 同期できる。途中で rename に失敗しても
// 既に動かした分の from:/to: は出力済みで、残りも試行し (best-effort)、最後に失敗をまとめて返す。
func cmdAutoArchive(args []string) error {
	days := autoArchiveDefaultDays
	var filterProjects []string
	allProjects := false
	dryRun := false
	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch a {
		case "--older-than":
			v, err := s.value("--older-than")
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return usagef("--older-than must be a non-negative integer (日数): %q", v)
			}
			days = n
		case "--project":
			v, err := s.value("--project")
			if err != nil {
				return err
			}
			filterProjects = append(filterProjects, v)
		case "--projects":
			v, err := s.value("--projects")
			if err != nil {
				return err
			}
			filterProjects = append(filterProjects, splitProjects(v)...)
		case "--all-projects":
			allProjects = true
		case "--dry-run":
			dryRun = true
		default:
			return usagef("unknown option: %s", a)
		}
	}
	if pos := s.rest(); len(pos) > 0 {
		return usagef("unexpected argument: %s", pos[0])
	}

	// done だけを読み (showAll は status 指定時は無関係)、完了後 days 日を過ぎたものに絞る。
	rows, _, _, err := selectTasks("done", filterProjects, true, allProjects, false, "", false)
	if err != nil {
		return err
	}
	now := time.Now()
	olderThan := time.Duration(days) * 24 * time.Hour
	targets := autoArchiveTargets(rows, olderThan, now)

	if len(targets) == 0 {
		fmt.Printf("対象なし (done かつ完了後 %d 日超のタスクはありません)\n", days)
		return nil
	}

	if dryRun {
		fmt.Printf("[dry-run] アーカイブ対象 %d 件 (完了後 %d 日超):\n", len(targets), days)
		for _, t := range targets {
			fmt.Printf("  %s/%s  完了 %s (%s前)  %s\n",
				t.Project, t.ID, displayDate(t.CompletedAt), humanizeSince(t.CompletedAt, now), t.Title)
		}
		fmt.Println("(--dry-run 指定のため移動していません。実行するには --dry-run を外してください)")
		return nil
	}

	var errs []error
	moved := 0
	for _, t := range targets {
		dest, err := moveTaskToArchive(t.Project, t.Path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s/%s: %w", t.Project, t.ID, err))
			continue
		}
		reportMove("archived", t.Project, t.ID, t.Path, dest)
		moved++
	}
	fmt.Printf("%d 件をアーカイブしました。上の from:/to: を sync --path にまとめて渡して同期してください。\n", moved)
	return errors.Join(errs...)
}

// autoArchiveTargets は rows から「status:done かつ completed_at が now から olderThan 以上前」の
// タスクだけを選ぶ (auto-archive の対象抽出)。completed_at が無い/壊れている done は期間判定
// できないので対象外。呼び出し側は done で絞って渡す前提だが、純粋関数として単体テストできるよう
// status もここで確認する (review / in-progress を弾く)。
func autoArchiveTargets(rows []Task, olderThan time.Duration, now time.Time) []Task {
	var out []Task
	for _, t := range rows {
		if t.Status != "done" {
			continue
		}
		ct, ok := parseTaskTime(t.CompletedAt)
		if !ok {
			continue
		}
		if now.Sub(ct) >= olderThan {
			out = append(out, t)
		}
	}
	return out
}

// cmdUnarchive はアーカイブ済みタスクを通常のディレクトリへ戻す。
func cmdUnarchive(args []string) error {
	project, id, err := parseProjectIDArgs(args)
	if err != nil {
		return err
	}
	src, err := resolveTaskPathIn(project, id, archiveDirName)
	if err != nil {
		return fmt.Errorf("アーカイブに見つかりません: %s / %s", project, normalizeID(id))
	}
	// 戻し先に同じ id のアクティブタスクが居ないか (番号は再利用しない方針なので通常は起きないが、
	// 万一被ると戻すと doctor が重複検出する。slug 違いでも検出できるよう id で照合する)。
	if existing, err := resolveTaskPath(project, id); err == nil {
		return fmt.Errorf("戻し先に同 id のアクティブタスクが既にあります (番号が再利用された可能性): %s", existing)
	}
	dest := filepath.Join(storeDir(), project, filepath.Base(src))
	if err := os.Rename(src, dest); err != nil {
		return fmt.Errorf("アーカイブ解除に失敗しました: %w", err)
	}
	reportMove("unarchived", project, id, src, dest)
	return nil
}

// parseProjectIDArgs は archive/unarchive の引数 ([<project>] <id>) を解決する。
// フラグは無いが `--` (オプション終端) は解釈して位置引数だけを取り出す。
func parseProjectIDArgs(args []string) (project, id string, err error) {
	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		s.positional(a)
	}
	return resolveProjectID(s.rest())
}

// reportMove は移動結果を表示する。1 行目は人間向けの要約、続く from:/to: は scoped sync 用の
// store 相対パス (リネームの旧/新を取りこぼさず stage するため)。
func reportMove(verb, project, id, src, dest string) {
	title := ""
	if t, err := parseTask(dest); err == nil {
		title = t.Title
	}
	fmt.Printf("%s %s/%s %s\n", verb, project, normalizeID(id), title)
	fmt.Printf("from: %s\n", storeRel(src))
	fmt.Printf("to:   %s\n", storeRel(dest))
}

// storeRel は store ルートからの相対パスを返す (sync --path にそのまま渡せる形)。
// 相対化できなければ元のパスをそのまま返す。
func storeRel(p string) string {
	if rel, err := filepath.Rel(storeDir(), p); err == nil {
		return rel
	}
	return p
}
