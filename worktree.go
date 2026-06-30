package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// cmdWorktreeInit は start/spawn が `git worktree add` した直後に呼ぶ「作成後フック」。
// メイン repo の .worktreeinclude にマッチする gitignored ファイルを新しい worktree に
// コピーし、.worktree-post-create があれば worktree 内で実行する。両方とも無ければ no-op
// なので、SKILL から無条件に呼んでよい。
//
// 冪等性: コピーは既存ファイルを上書きしない。post-create はマーカーで二重実行を防ぐ
// (spawn の親と子の start が両方呼んでも 1 回だけ走る)。--force で再実行できる。
func cmdWorktreeInit(args []string) error {
	force := false
	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch {
		case a == "--force":
			force = true
		case strings.HasPrefix(a, "-"):
			return usagef("unknown option: %s", a)
		default:
			s.positional(a)
		}
	}
	var dirArg string
	switch pos := s.rest(); len(pos) {
	case 0:
		return usagef("worktree-init は <worktree-dir> が必要")
	case 1:
		dirArg = pos[0]
	default:
		return usagef("worktree-init は <worktree-dir> を1つだけ取る")
	}

	worktreeDir, err := filepath.Abs(dirArg)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(worktreeDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("worktree ディレクトリがありません: %s", worktreeDir)
	}
	mainRepo, err := mainRepoOf(worktreeDir)
	if err != nil {
		return fmt.Errorf("%s のメイン repo を特定できません (git worktree ですか): %w", worktreeDir, err)
	}

	copied, err := copyWorktreeIncludes(mainRepo, worktreeDir)
	if err != nil {
		return fmt.Errorf(".worktreeinclude のコピーに失敗: %w", err)
	}
	for _, rel := range copied {
		fmt.Printf("copied: %s\n", rel)
	}

	ran, err := runPostCreate(mainRepo, worktreeDir, force)
	if err != nil {
		return fmt.Errorf(".worktree-post-create に失敗: %w", err)
	}

	if len(copied) == 0 && !ran {
		fmt.Println("worktree-init: 適用なし (.worktreeinclude / .worktree-post-create が無いか実行済み)")
	}
	return nil
}

// mainRepoOf は worktree からメイン作業ツリーの root を返す。
// git-common-dir はリンク worktree でもメイン repo の .git を指すので、その親が root。
func mainRepoOf(worktreeDir string) (string, error) {
	out, err := exec.Command("git", "-C", worktreeDir,
		"rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", err
	}
	commonDir := strings.TrimSpace(string(out))
	if commonDir == "" {
		return "", errors.New("git-common-dir が空")
	}
	// --path-format=absolute なので通常は絶対パスだが、念のため相対なら起点 (worktreeDir) で
	// 絶対化する。commonDir はメイン作業ツリーの .git を指すので、その親 = メイン repo root。
	if !filepath.IsAbs(commonDir) {
		if abs, err := filepath.Abs(filepath.Join(worktreeDir, commonDir)); err == nil {
			commonDir = abs
		}
	}
	return filepath.Dir(commonDir), nil
}

// copyWorktreeIncludes はメイン repo の .worktreeinclude を読み、マッチする gitignored
// ファイルを worktree へコピーする。コピーした相対パスを返す。
//
// .worktreeinclude は .gitignore 構文のサブセット (リテラルパス / 単純グロブ / ディレクトリ)
// として扱い、各行を repo root 起点に展開する。tracked ファイルを複製しないよう
// git check-ignore で「gitignore 対象」のものだけに絞る (Claude Code と同じ安全策)。
// 既存の dest は上書きしない (冪等)。
func copyWorktreeIncludes(mainRepo, worktreeDir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(mainRepo, ".worktreeinclude"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var candidates []string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(mainRepo, line))
		for _, m := range matches {
			info, err := os.Lstat(m) // Lstat: symlink を追従せず素のエントリを見る
			if err != nil {
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 {
				// symlink は追従しない (リンク先が repo 外を指し得るため。underRepo と同じ防御方針)。
				noteSkippedSymlink(mainRepo, m)
				continue
			}
			if info.IsDir() {
				filepath.WalkDir(m, func(p string, d fs.DirEntry, err error) error {
					if err != nil || d.IsDir() {
						return nil
					}
					if d.Type()&os.ModeSymlink != 0 {
						// ディレクトリ配下の symlink も同様に追従しない。
						noteSkippedSymlink(mainRepo, p)
						return nil
					}
					if rel, e := filepath.Rel(mainRepo, p); e == nil && underRepo(rel) {
						candidates = append(candidates, rel)
					}
					return nil
				})
			} else if rel, e := filepath.Rel(mainRepo, m); e == nil && underRepo(rel) {
				candidates = append(candidates, rel)
			}
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	ignored, err := filterIgnored(mainRepo, candidates)
	if err != nil {
		return nil, err
	}

	var copied []string
	for _, rel := range ignored {
		dst := filepath.Join(worktreeDir, rel)
		if _, err := os.Lstat(dst); err == nil {
			continue // 既存は上書きしない (Lstat: 壊れた symlink も「在る」とみなし、追従して target を作らない)
		}
		if err := copyFile(filepath.Join(mainRepo, rel), dst); err != nil {
			return copied, err
		}
		copied = append(copied, rel)
	}
	return copied, nil
}

// underRepo は rel (mainRepo からの相対パス) が repo 配下に収まるかを返す。
// .worktreeinclude に `../foo` や絶対パスが書かれても、コピー元が repo 外を指したり
// コピー先 (worktreeDir/rel) が worktree の外へ出るのを防ぐためのガード。
func underRepo(rel string) bool {
	if rel == "" || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// filterIgnored は rels のうち git が gitignore 対象とみなすものだけを返す。
// git check-ignore は「無視対象が1つも無い」とき exit 1 を返すが、これはエラーではない。
func filterIgnored(mainRepo string, rels []string) ([]string, error) {
	cmd := exec.Command("git", "-C", mainRepo, "check-ignore", "--stdin")
	cmd.Stdin = strings.NewReader(strings.Join(rels, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return nil, nil // 該当なし
		}
		return nil, fmt.Errorf("git check-ignore: %w", err)
	}
	var res []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			res = append(res, l)
		}
	}
	return res, nil
}

// noteSkippedSymlink は symlink を追従せずスキップしたことを stderr に知らせる
// (一覧に書いた include がコピーされない理由を利用者が分かるように)。
func noteSkippedSymlink(mainRepo, path string) {
	rel, err := filepath.Rel(mainRepo, path)
	if err != nil {
		rel = path
	}
	fmt.Fprintf(os.Stderr, "worktree-init: %s は symlink のためコピーしません (リンク先の追従を抑止)\n", rel)
}

// copyFile は src を dst へコピーする (親ディレクトリ作成 + パーミッション保持)。
// src が symlink の場合は追従しない (os.Open は symlink を辿ってリンク先=repo 外も
// 読み得るため)。呼び出し側で既に除外しているが、単体でも安全になるよう二重に防ぐ。
func copyFile(src, dst string) error {
	if fi, err := os.Lstat(src); err != nil {
		return err
	} else if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink はコピーしません: %s", src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// runPostCreate はメイン repo の .worktree-post-create を worktree 内で実行する。
// 実行したら true。スクリプトが無い / 実行済み (マーカーあり, force でない) なら false。
// 実行可能ビットがあれば直接実行 (shebang 尊重)、無ければ sh で実行する。
func runPostCreate(mainRepo, worktreeDir string, force bool) (bool, error) {
	hook := filepath.Join(mainRepo, ".worktree-post-create")
	info, err := os.Stat(hook)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	marker := postCreateMarker(worktreeDir)
	if !force && marker != "" {
		if _, err := os.Stat(marker); err == nil {
			fmt.Println("post-create: 実行済みのためスキップ (--force で再実行)")
			return false, nil
		}
	}

	var cmd *exec.Cmd
	if info.Mode().Perm()&0o111 != 0 {
		cmd = exec.Command(hook)
	} else {
		cmd = exec.Command("sh", hook)
	}
	cmd.Dir = worktreeDir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(),
		"AGENT_TASKS_WORKTREE="+worktreeDir,
		"AGENT_TASKS_MAIN="+mainRepo,
		"AGENT_TASKS_PROJECT="+filepath.Base(mainRepo),
	)
	fmt.Printf("post-create: %s を実行 (cwd: %s)\n", hook, worktreeDir)
	if err := cmd.Run(); err != nil {
		return true, err
	}
	// マーカーは二重実行ガード。書けない / git dir 特定不能だと冪等性が崩れ post-create が
	// 毎回再実行されるので、黙らず警告する (副作用付き post-create の二重実行に気づけるように)。
	if marker != "" {
		if err := os.WriteFile(marker, nil, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "warning: post-create 実行済みマーカーを書けませんでした (%s): %v — 次回も再実行されます\n", marker, err)
		}
	} else {
		fmt.Fprintln(os.Stderr, "warning: worktree の git dir を特定できず post-create マーカーを記録できません — 次回も再実行されます")
	}
	return true, nil
}

// postCreateMarker は post-create 実行済みマーカーのパスを返す。worktree 固有の git dir
// (main/.git/worktrees/<name>) 配下に置くので、作業ツリーを汚さない。特定不能なら空。
func postCreateMarker(worktreeDir string) string {
	out, err := exec.Command("git", "-C", worktreeDir, "rev-parse", "--absolute-git-dir").Output()
	if err != nil {
		return ""
	}
	gitDir := strings.TrimSpace(string(out))
	if gitDir == "" {
		return ""
	}
	return filepath.Join(gitDir, "agent-tasks-post-create-done")
}
