package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// templates/ をバイナリに同梱する。symlink 経由でもパス解決に悩まず使えるよう embed する。
//
//go:embed templates
var templatesFS embed.FS

// scaffoldFile はテンプレ内のファイル名と、プロジェクトに書き出すときの名前・実行ビットの対応。
type scaffoldFile struct {
	tmpl string      // templates/<stack>/<tmpl>
	dst  string      // プロジェクト root に書き出す名前
	mode os.FileMode // パーミッション
}

var scaffoldFiles = []scaffoldFile{
	{"worktreeinclude", ".worktreeinclude", 0o644},
	{"post-create", ".worktree-post-create", 0o755},
	{"post-remove", ".worktree-post-remove", 0o755},
}

// cmdScaffoldWorktree はスタック (firebase/rails/...) の推奨 worktree 設定
// (.worktreeinclude / .worktree-post-create) をプロジェクトに展開する。
// これらは worktree-init (作成後フック) が参照する設定で、本コマンドは「設定を置くだけ」。
func cmdScaffoldWorktree(args []string) error {
	var stack, dir string
	force, list, printOnly := false, false, false
	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch {
		case a == "--force":
			force = true
		case a == "--list":
			list = true
		case a == "--print" || a == "--dry-run":
			printOnly = true
		case a == "--dir":
			v, err := s.value("--dir")
			if err != nil {
				return err
			}
			dir = v
		case strings.HasPrefix(a, "-"):
			return usagef("unknown option: %s", a)
		default:
			s.positional(a)
		}
	}
	switch pos := s.rest(); len(pos) {
	case 0:
		// stack 未指定 (自動検出に任せる)
	case 1:
		stack = pos[0]
	default:
		return usagef("scaffold-worktree は <stack> を1つだけ取る")
	}

	stacks, err := availableStacks()
	if err != nil {
		return err
	}

	if list {
		fmt.Println("利用可能なスタック:")
		for _, s := range stacks {
			fmt.Printf("  %s\n", s)
		}
		return nil
	}

	// --print/--dry-run は書き出さず stdout にプレビューするだけなので、書き込み先ディレクトリや
	// git リポジトリを必要としない。スタックは明示指定があればそれ、無ければ cwd から自動検出する。
	if printOnly {
		if stack == "" {
			stack = detectStack(".")
			if stack == "" {
				return fmt.Errorf("スタックを自動検出できませんでした。<stack> を指定してください (利用可能: %s)",
					strings.Join(stacks, ", "))
			}
		}
		if !slices.Contains(stacks, stack) {
			return fmt.Errorf("未知のスタック %q (利用可能: %s)", stack, strings.Join(stacks, ", "))
		}
		return scaffoldPrint(stack)
	}

	if dir == "" {
		dir = "."
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return err
	}

	if stack == "" {
		stack = detectStack(dir)
		if stack == "" {
			return fmt.Errorf("スタックを自動検出できませんでした。<stack> を指定してください (利用可能: %s)",
				strings.Join(stacks, ", "))
		}
		fmt.Printf("検出したスタック: %s\n", stack)
	}
	if !slices.Contains(stacks, stack) {
		return fmt.Errorf("未知のスタック %q (利用可能: %s)", stack, strings.Join(stacks, ", "))
	}

	written, skipped, err := scaffoldInto(stack, dir, force)
	if err != nil {
		return err
	}
	for _, f := range written {
		fmt.Printf("wrote: %s\n", f)
	}
	for _, f := range skipped {
		fmt.Printf("skip (既存, 上書きは --force): %s\n", f)
	}
	if len(written) > 0 {
		fmt.Println("\n次の手順:")
		fmt.Println("  1. 生成された .worktreeinclude / .worktree-post-create を確認・調整する")
		fmt.Println("  2. コミットする (以降 start/spawn が worktree 作成時に自動適用)")
	}
	return nil
}

// scaffoldPrint は stack のテンプレを書き出さず、ファイルごとに区切り見出しを付けて stdout に出す。
// 適用前に中身を確認・比較したいとき (--print / --dry-run) に使う。書き込み先を必要としないので
// git リポジトリ外でも実行できる。
func scaffoldPrint(stack string) error {
	for _, f := range scaffoldFiles {
		data, err := templatesFS.ReadFile(path.Join("templates", stack, f.tmpl))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // テンプレに無いファイルは飛ばす
			}
			return err
		}
		fmt.Printf("===== %s (mode %o) =====\n", f.dst, f.mode.Perm())
		os.Stdout.Write(data)
		if len(data) > 0 && data[len(data)-1] != '\n' {
			fmt.Println()
		}
		fmt.Println()
	}
	return nil
}

// availableStacks は埋め込みテンプレからスタック名 (templates 直下のディレクトリ) を返す。
func availableStacks() ([]string, error) {
	entries, err := templatesFS.ReadDir("templates")
	if err != nil {
		return nil, err
	}
	var stacks []string
	for _, e := range entries {
		if e.IsDir() {
			stacks = append(stacks, e.Name())
		}
	}
	slices.Sort(stacks)
	return stacks, nil
}

// detectStack は dir の中身からスタックを推測する。判定できなければ空文字。
func detectStack(dir string) string {
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(dir, rel))
		return err == nil
	}
	switch {
	case exists("firebase.json") || exists(".firebaserc"):
		return "firebase"
	case exists("bin/rails") || exists("config/environment.rb"):
		return "rails"
	default:
		return ""
	}
}

// scaffoldInto は stack のテンプレを dir に書き出す。書き出した名前と (既存で) スキップした名前を返す。
func scaffoldInto(stack, dir string, force bool) (written, skipped []string, err error) {
	for _, f := range scaffoldFiles {
		data, err := templatesFS.ReadFile(path.Join("templates", stack, f.tmpl))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // テンプレに無いファイルは飛ばす
			}
			return written, skipped, err
		}
		dstPath := filepath.Join(dir, f.dst)
		if !force {
			if _, err := os.Stat(dstPath); err == nil {
				skipped = append(skipped, f.dst)
				continue
			}
		}
		if err := os.WriteFile(dstPath, data, f.mode); err != nil {
			return written, skipped, err
		}
		written = append(written, f.dst)
	}
	return written, skipped, nil
}
