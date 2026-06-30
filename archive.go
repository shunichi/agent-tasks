package main

import (
	"fmt"
	"os"
	"path/filepath"
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
	archiveDir := filepath.Join(storeDir(), project, archiveDirName)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return fmt.Errorf("アーカイブディレクトリを作成できません: %w", err)
	}
	dest := filepath.Join(archiveDir, filepath.Base(src))
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("退避先に同名ファイルが既にあります: %s", dest)
	}
	if err := os.Rename(src, dest); err != nil {
		return fmt.Errorf("アーカイブに失敗しました: %w", err)
	}
	reportMove("archived", project, id, src, dest)
	return nil
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
