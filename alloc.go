package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 採番 (alloc-id) は「既存最大 + 1」を CLI 側で原子的に確保するための機構。
// skill (agent) が「最大 + 1 を自分で計算 → ファイル作成」していたのを置き換える。
// 旧フローは計算とファイル作成の間に隙間があり (TOCTOU)、ローカル並行 create で同じ id を
// 引く競合が実際に多発した。alloc-id は project ごとのロック下で「採番 → 予約ファイル作成」を
// 行い、ローカル並行セッション間の衝突を確実に防ぐ。
//
// 予約ファイルは空の <NNNN>-<slug>.md。stdout にそのパスを 1 行返すので、skill は中身
// (frontmatter + 本文) を書き込むだけでよい。別マシン間の衝突 (push 時に判明) は git の性質上
// 残るため、doctor の重複検査をフォールバックとして併用する。
const (
	allocLockName  = ".alloc.lock"    // project ディレクトリ直下のロックファイル名
	allocLockStale = 30 * time.Second // これより古いロックは残骸とみなして奪う
	allocLockWait  = 5 * time.Second  // 生存中ロックを待つ上限 (採番は一瞬なので十分)
	allocLockPoll  = 50 * time.Millisecond
)

// cmdAllocID はタスク id を原子的に採番し、予約用の空ファイルを作って絶対パスを stdout に出す。
// 使い方: agent-tasks alloc-id [--project <p>] --slug <slug> [--pull]
//   - --project 省略時は cwd の git リポジトリから判定 (list と同じ規則)。
//   - --pull を付けると採番前にストアを git pull --rebase する (別マシン衝突の軽減。best-effort)。
func cmdAllocID(args []string) error {
	var project, slug string
	pull := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--project":
			if i+1 >= len(args) {
				return usagef("--project には値が必要")
			}
			i++
			project = args[i]
		case "--slug":
			if i+1 >= len(args) {
				return usagef("--slug には値が必要")
			}
			i++
			slug = args[i]
		case "--pull":
			pull = true
		default:
			if v, ok := strings.CutPrefix(a, "--project="); ok {
				project = v
				continue
			}
			if v, ok := strings.CutPrefix(a, "--slug="); ok {
				slug = v
				continue
			}
			return usagef("unknown option: %s", a)
		}
	}

	if project == "" {
		project = currentProject()
		if project == "" {
			return usagef("project を判定できません。git リポジトリ内で実行するか --project <name> を指定してください")
		}
	}
	if err := validateSlug(slug); err != nil {
		return err
	}

	projDir := filepath.Join(storeDir(), project)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		return fmt.Errorf("project ディレクトリを作成できません: %w", err)
	}
	if pull {
		pullStore() // best-effort。失敗してもローカル採番は続行する
	}

	id, path, err := allocTaskFile(projDir, slug)
	if err != nil {
		return err
	}
	// stderr に人間向けの一言、stdout は予約ファイルの絶対パス 1 行 (skill / スクリプトが取り込む)。
	fmt.Fprintf(os.Stderr, "予約しました: %s/%s-%s.md (id %s)\n", project, id, slug, id)
	fmt.Println(path)
	return nil
}

// validateSlug は slug が英小文字・数字・ハイフンのみのケバブケースかを検査する。
// ファイル名 <NNNN>-<slug>.md に直接使うため、パス区切りや空白を弾く。
func validateSlug(slug string) error {
	if slug == "" {
		return usagef("--slug が必要 (内容を表す英語ケバブケース)")
	}
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return usagef("--slug は英小文字・数字・ハイフンのみ使用可: %q", slug)
		}
	}
	if strings.HasPrefix(slug, "-") || strings.HasSuffix(slug, "-") {
		return usagef("--slug の先頭/末尾にハイフンは使えません: %q", slug)
	}
	return nil
}

// allocTaskFile は projDir のロックを取り、その下で「最大 id + 1」を採番して
// 予約ファイル <NNNN>-<slug>.md を作成する。返り値は採番した id とその絶対パス。
//
// ロック下なので採番とファイル作成は他のローカルプロセスと排他される。O_EXCL は
// 万一同名ファイルが既にある場合 (同 id 同 slug) の保険で、その場合は次の番号へ進む。
func allocTaskFile(projDir, slug string) (id, path string, err error) {
	unlock, err := lockProject(projDir)
	if err != nil {
		return "", "", err
	}
	defer unlock()

	next := maxTaskID(projDir) + 1
	for {
		id = fmt.Sprintf("%04d", next)
		path = filepath.Join(projDir, id+"-"+slug+".md")
		f, openErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if openErr == nil {
			f.Close()
			return id, path, nil
		}
		if errors.Is(openErr, os.ErrExist) {
			next++ // 同名が既存。次の番号へ
			continue
		}
		return "", "", fmt.Errorf("予約ファイルを作成できません: %w", openErr)
	}
}

// maxTaskID は projDir のタスク id の最大値を返す。アクティブ (projDir 直下) に加え、
// アーカイブ (projDir/archive/) も対象にする — 番号は再利用しない方針なので、退避済みの
// 番号と被らないよう採番にはアーカイブの最大値も算入する。
// 空 / 読み取り失敗時は 0 (次の採番は 0001)。
func maxTaskID(projDir string) int {
	max := maxIDInDir(projDir)
	if a := maxIDInDir(filepath.Join(projDir, archiveDirName)); a > max {
		max = a
	}
	return max
}

// maxIDInDir は d 直下の *.md のうちファイル名先頭連番 (NNNN) の最大値を返す。
// ロックファイル等の非タスクファイルは leadingID が空なので自然に無視される。
// 予約直後の空ファイルも leadingID を持つので算入され、連続採番でも番号が重ならない。
func maxIDInDir(d string) int {
	entries, err := os.ReadDir(d)
	if err != nil {
		return 0
	}
	max := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		lead := leadingID(name)
		if lead == "" {
			continue
		}
		if n, err := strconv.Atoi(lead); err == nil && n > max {
			max = n
		}
	}
	return max
}

// lockProject は projDir の採番ロックを取得し、解放関数を返す (lockFile の薄いラッパ)。
func lockProject(projDir string) (func(), error) {
	return lockFile(filepath.Join(projDir, allocLockName), allocLockWait, allocLockStale)
}

// lockFile は lockPath を O_CREATE|O_EXCL で作って排他ロックとし、解放関数 (ファイル削除) を返す。
// 生存中のロックは wait まで待ち、stale より古い残骸ロック (途中で死んだプロセスの置き土産) は
// 奪って続行する。採番 (alloc-id) と同期 (sync) で共有するプロセス間ロック。
func lockFile(lockPath string, wait, stale time.Duration) (func(), error) {
	deadline := time.Now().Add(wait)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			return func() { os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("ロックを取得できません: %w", err)
		}
		// 既存ロックが残骸 (mtime が古い) なら奪って再試行する。
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > stale {
			os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("ロック %s を取得できませんでした (別の処理が動作中の可能性)", lockPath)
		}
		time.Sleep(allocLockPoll)
	}
}

// pullStore はストア (git repo) を pull --rebase する。採番前の最新化用 (best-effort)。
func pullStore() {
	cmd := exec.Command("git", "-C", storeDir(), "pull", "--rebase", "--quiet")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}
