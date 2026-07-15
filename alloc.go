package main

import (
	"errors"
	"fmt"
	"io"
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
// 予約ファイルは既定では空の <NNNN>-<slug>.md。stdout にそのパスを 1 行返すので、skill は中身
// (frontmatter + 本文) を書き込むだけでよい。別マシン間の衝突 (push 時に判明) は git の性質上
// 残るため、doctor の重複検査をフォールバックとして併用する。
//
// --title を渡すと「フル生成モード」になり、採番と同時に frontmatter + 本文まで書き込む。
// これは Claude Code の Write ツールが「既存 (空) ファイルを未読で上書き」を拒否する
// (`File has not been read yet`) のを避けるため: alloc-id が中身まで書けば skill は Write を
// 使わずに済む (Read→Write の往復とエラーが消える)。本文は stdin (既定) か --body-file で受ける。
// id/branch/worktree/created 等の id 依存メタは採番後に CLI が埋めるので、skill は id を知らずに
// title と本文だけ渡せばよい (chicken-and-egg を避ける)。
const (
	allocLockName  = ".alloc.lock"    // project ディレクトリ直下のロックファイル名
	allocLockStale = 30 * time.Second // これより古いロックは残骸とみなして奪う
	allocLockWait  = 5 * time.Second  // 生存中ロックを待つ上限 (採番は一瞬なので十分)
	allocLockPoll  = 50 * time.Millisecond
)

// cmdAllocID はタスク id を原子的に採番し、予約ファイルを作って絶対パスを stdout に出す。
// 使い方:
//
//		agent-tasks alloc-id [--project <p>] --slug <slug> [--pull]                # 空予約 (従来)
//		agent-tasks alloc-id [--project <p>] --slug <slug> --title <t> [--kind human] [--body-file <f>]  # フル生成
//
//	  - --project 省略時は cwd の git リポジトリから判定 (list と同じ規則)。
//	  - --pull を付けると採番前にストアを git pull --rebase する (別マシン衝突の軽減。best-effort)。
//	  - --title を渡すと frontmatter + 本文まで書き込む (フル生成モード)。本文 (要件) は
//	    --body-file <path> (`-` で stdin) から、省略時は stdin から読む。--kind human で人手タスク
//	    (branch/worktree を空にする)。--title 無しなら従来どおり空ファイルだけ予約する。
func cmdAllocID(args []string) error {
	var project, slug, title, kind, bodyFile string
	pull := false
	full := false // --title が来たらフル生成モード
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
		case "--title":
			if i+1 >= len(args) {
				return usagef("--title には値が必要")
			}
			i++
			title = args[i]
			full = true
		case "--kind":
			if i+1 >= len(args) {
				return usagef("--kind には値が必要")
			}
			i++
			kind = args[i]
		case "--body-file":
			if i+1 >= len(args) {
				return usagef("--body-file には値が必要")
			}
			i++
			bodyFile = args[i]
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
			if v, ok := strings.CutPrefix(a, "--title="); ok {
				title = v
				full = true
				continue
			}
			if v, ok := strings.CutPrefix(a, "--kind="); ok {
				kind = v
				continue
			}
			if v, ok := strings.CutPrefix(a, "--body-file="); ok {
				bodyFile = v
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
	kind, err := normalizeKind(kind)
	if err != nil {
		return err
	}
	if !full && (kind != "" || bodyFile != "") {
		return usagef("--kind / --body-file は --title (フル生成モード) と併用してください")
	}

	projDir := filepath.Join(storeDir(), project)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		return fmt.Errorf("project ディレクトリを作成できません: %w", err)
	}
	if pull {
		pullStore() // best-effort。失敗してもローカル採番は続行する
	}

	// フル生成モードは採番前に本文を読み込む (採番ロックの保持時間を最小化し、read エラーで
	// 空ファイルだけ予約されるのを防ぐ)。
	var write func(id string) []byte
	if full {
		if strings.TrimSpace(title) == "" {
			return usagef("--title は空にできません")
		}
		body, err := readBody(bodyFile)
		if err != nil {
			return err
		}
		now := time.Now()
		write = func(id string) []byte {
			return buildNewTaskMarkdown(id, project, slug, title, kind, body, now, false)
		}
	}

	id, path, err := allocReserve(projDir, slug, write)
	if err != nil {
		return err
	}
	// stderr に人間向けの一言、stdout は予約ファイルの絶対パス 1 行 (skill / スクリプトが取り込む)。
	verb := "予約しました"
	if full {
		verb = "作成しました"
	}
	fmt.Fprintf(os.Stderr, "%s: %s/%s-%s.md (id %s)\n", verb, project, id, slug, id)
	fmt.Println(path)
	return nil
}

// normalizeKind は --kind の値を検証する。空 / "code" は "" (= 既定の code タスク。kind 行は書かない)、
// "human" はそのまま返す。それ以外はエラー (doctor が弾く不正値を入口で防ぐ)。
func normalizeKind(kind string) (string, error) {
	switch kind {
	case "", "code":
		return "", nil
	case "human":
		return "human", nil
	default:
		return "", usagef("--kind は human か code のいずれか (got %q)", kind)
	}
}

// readBody はフル生成モードの本文 (要件) を読む。bodyFile が空か "-" なら stdin、そうでなければ
// そのファイルから読む。末尾は buildNewTaskMarkdown 側で整形するのでここでは素の内容を返す。
func readBody(bodyFile string) (string, error) {
	if bodyFile == "" || bodyFile == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("stdin から本文を読めません: %w", err)
		}
		return string(b), nil
	}
	b, err := os.ReadFile(bodyFile)
	if err != nil {
		return "", fmt.Errorf("--body-file を読めません: %w", err)
	}
	return string(b), nil
}

// buildNewTaskMarkdown は新規 todo タスクの完全な Markdown (frontmatter + 本文) を組み立てる。
// id 依存のメタ (id / branch / worktree) と時刻 (created/updated/登録ログ) は CLI が採番後に埋める。
// kind=="human" のときは branch/worktree を空にする (start で worktree を作らないタスク)。
// title は既存ファイルの表記に合わせて素のまま書く (parseTask は最初の ':' で切るので ':' を含む
// title も読める)。body は要件セクションに入れ、進捗ログの「登録」行を 1 行付ける。
// draft=true (TUI の簡易登録) のときは frontmatter に draft: true を立て、要件が未整理であることと
// 着手前にエージェントが詳細化する導線を本文に残す (draft フラグは機械可読な印、本文注記は人/agent
// 向けの示唆で役割を分ける)。
func buildNewTaskMarkdown(id, project, slug, title, kind, body string, now time.Time, draft bool) []byte {
	iso := now.Format(time.RFC3339)
	logDate := now.Format("2006-01-02 15:04")
	branch, worktree := "", ""
	if kind != "human" {
		branch = "task/" + id + "-" + slug
		worktree = "../" + project + "--" + id
	}

	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %q\n", id)
	b.WriteString("project: " + project + "\n")
	b.WriteString("title: " + title + "\n")
	b.WriteString("status: todo\n")
	if kind == "human" {
		b.WriteString("kind: human\n")
	}
	if draft {
		b.WriteString("draft: true\n")
	}
	b.WriteString("agent:\n")
	b.WriteString("session:\n")
	b.WriteString(fmLine("branch", branch) + "\n") // human は空値 (末尾スペースなしの "branch:")
	b.WriteString(fmLine("worktree", worktree) + "\n")
	fmt.Fprintf(&b, "created: %q\n", iso)
	fmt.Fprintf(&b, "updated: %q\n", iso)
	b.WriteString("---\n\n")

	b.WriteString("# 要件\n\n")
	if body = strings.TrimSpace(body); body != "" {
		b.WriteString(body + "\n\n")
	}
	if draft {
		b.WriteString("> 簡易登録 (TUI から作成)。要件は未整理のため、着手前にエージェントが詳細化する。\n\n")
	}
	b.WriteString("## 進捗ログ\n")
	logVerb := "登録"
	if draft {
		logVerb = "簡易登録 (TUI)"
	}
	b.WriteString("- " + logDate + " " + logVerb + "\n")
	return []byte(b.String())
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

// slugMaxLen は自動生成 slug の最大長 (ファイル名が長くなりすぎないよう頭打ち)。
const slugMaxLen = 40

// slugFromTitle は日本語などを含むタイトルから、ファイル名に使える ASCII ケバブケースの slug を
// 機械生成する。ASCII 英数字はそのまま (小文字化)、その他の文字は区切りのハイフンに畳む。ASCII
// 英数字が全く無い (例: 日本語のみの) タイトルでは結果が空になるので "task" にフォールバックする。
// TUI の簡易登録は AI を通さず英語 slug を訳せないので、この機械生成 + フォールバックで賄う
// (slug はファイル名の飾りで、一次キーは id。着手時にエージェントが必要なら整える)。
// 返り値は必ず validateSlug を満たす (英小文字・数字・ハイフンのみ、先頭/末尾ハイフンなし、非空)。
func slugFromTitle(title string) string {
	var b strings.Builder
	prevHyphen := true // 先頭のハイフンを防ぐ (空の状態からは区切りを出さない)
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > slugMaxLen {
		s = strings.Trim(s[:slugMaxLen], "-")
	}
	if s == "" {
		return "task"
	}
	return s
}

// createDraftTask は TUI の簡易登録 (draft) タスクを作り、採番した id を返す。タイトル (必須) と
// 任意の説明だけを受け、slug は slugFromTitle で機械生成、要件は未整理のまま draft: true で登録する
// (着手前にエージェントが詳細化する導線は buildNewTaskMarkdown が本文に残す)。採番 + 書き込みは
// allocReserve に委ね、ローカル並行 create の id 競合を避ける。dir はストアの root (通常 storeDir())。
func createDraftTask(dir, project, title, desc string) (id string, err error) {
	projDir := filepath.Join(dir, project)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		return "", fmt.Errorf("project ディレクトリを作成できません: %w", err)
	}
	slug := slugFromTitle(title)
	now := time.Now()
	write := func(id string) []byte {
		return buildNewTaskMarkdown(id, project, slug, title, "", desc, now, true)
	}
	id, _, err = allocReserve(projDir, slug, write)
	return id, err
}

// allocTaskFile は projDir のロックを取り、その下で「最大 id + 1」を採番して
// 空の予約ファイル <NNNN>-<slug>.md を作成する。返り値は採番した id とその絶対パス。
// (allocReserve の空予約版。中身まで書くフル生成は allocReserve に write を渡す。)
func allocTaskFile(projDir, slug string) (id, path string, err error) {
	return allocReserve(projDir, slug, nil)
}

// allocReserve は projDir のロック下で「最大 id + 1」を採番し、予約ファイル <NNNN>-<slug>.md を
// O_EXCL で作成する。write != nil ならその id 向けの中身を書き込む (フル生成モード)、nil なら
// 空ファイル (従来の予約)。返り値は採番した id とその絶対パス。
//
// ロック下なので採番とファイル作成は他のローカルプロセスと排他される。O_EXCL は
// 万一同名ファイルが既にある場合 (同 id 同 slug) の保険で、その場合は次の番号へ進む。
func allocReserve(projDir, slug string, write func(id string) []byte) (id, path string, err error) {
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
			if write != nil {
				if _, werr := f.Write(write(id)); werr != nil {
					f.Close()
					os.Remove(path) // 中途半端なファイルを残さない
					return "", "", fmt.Errorf("タスクファイルを書き込めません: %w", werr)
				}
			}
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
