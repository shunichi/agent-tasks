package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// cmdOpen はタスクに紐づく worktree (frontmatter の worktree:) をエディタで開く。
// エディタの決定は edit と同じ (AGENT_TASKS_EDITOR > VISUAL > EDITOR、既定 code)。
// worktree: は各コードリポジトリの root からの相対パス (start が作る ../<project>--<NNNN>) なので、
// 現在の cwd のメインリポ root を基点に絶対パスへ解決する (絶対パスならそのまま使う)。
//
// ストアのタスクファイル自体を開く edit とは別コマンド。こちらは「作業ツリーを開く」。
func cmdOpen(args []string) error {
	// フラグは無いが `--` (オプション終端) は解釈して位置引数だけを取り出す。
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
	if t.Worktree == "" {
		return fmt.Errorf("%s / %s に worktree が記録されていません (未着手か、worktree 無しのタスク)", project, normalizeID(id))
	}

	dir, err := resolveWorktreeDir(t.Worktree)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("worktree が見つかりません: %s (done で撤去済み? / 別のリポジトリから実行している?)", dir)
	}

	argv := append(editorArgv(), dir)
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return fmt.Errorf("エディタが見つかりません: %s (AGENT_TASKS_EDITOR / VISUAL / EDITOR で指定可)", argv[0])
	}
	cmd := exec.Command(bin, argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// resolveWorktreeDir は frontmatter の worktree: を絶対パスへ解決する。絶対パスならそのまま、
// 相対パス (../<project>--<NNNN> 等) は現在 cwd のメインリポ root を基点に解決する。
// git 外で相対パスを渡された場合は解決できないのでエラー (絶対パス指定を促す)。
func resolveWorktreeDir(worktree string) (string, error) {
	if filepath.IsAbs(worktree) {
		return filepath.Clean(worktree), nil
	}
	root, err := mainRepoOf(".")
	if err != nil {
		return "", fmt.Errorf("worktree の相対パス %q を解決できません。対象のコードリポジトリ内で実行してください", worktree)
	}
	return joinWorktree(root, worktree), nil
}

// joinWorktree は root を基点に worktree パスを絶対パスへ解決する (絶対パスはそのまま)。
// mainRepoOf に依存しない純粋関数 (解決ロジックのテスト用に分離)。
func joinWorktree(root, worktree string) string {
	if filepath.IsAbs(worktree) {
		return filepath.Clean(worktree)
	}
	return filepath.Clean(filepath.Join(root, worktree))
}
