package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// issue はタスクを GitHub issue として共有するための機能 (store → issue の一方向)。
//
// ストアは private で人に見せづらいので、共有したいタスクだけ `agent-tasks issue` で起票し、
// その URL を frontmatter の issue: に記録する (1 タスク 1 issue)。issue が既に紐づいていれば
// 本文を再同期する (更新)。issue 側の編集を取り込む双方向同期はしない。
//
// 本文は frontmatter (branch/worktree/session 等の内部メタ) を除いた Markdown 本文だけを送る。
// 作成先 repo は --repo 明示が最優先、省略時は cwd のコード repo から gh で推論する。

// cmdIssue はタスクを GitHub issue として起票 (未連携) / 本文更新 (連携済み) する。
func cmdIssue(args []string) error {
	var repo string
	s := newArgScan(args)
	for {
		a, ok := s.token()
		if !ok {
			break
		}
		switch a {
		case "--repo":
			v, err := s.value("--repo")
			if err != nil {
				return err
			}
			repo = v
		default:
			s.positional(a)
		}
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
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh (GitHub CLI) が必要です。https://cli.github.com/ を導入し `gh auth login` してください")
	}

	title := t.Title
	body := taskBody(path)

	// 連携済み: 既存 issue の本文を再同期する (store → issue 一方向)。
	if t.Issue != "" {
		out, err := ghRun(strings.NewReader(body), "issue", "edit", t.Issue, "--title", title, "--body-file", "-")
		if err != nil {
			return fmt.Errorf("gh issue edit に失敗しました:\n%s", out)
		}
		fmt.Printf("updated %s/%s %s\n%s\n", project, normalizeID(id), title, t.Issue)
		return nil
	}

	// 未連携: 新規起票。repo は --repo 明示 > cwd 推論。
	if repo == "" {
		inferred, ierr := inferRepo()
		if ierr != nil {
			return fmt.Errorf("issue 作成先 repo を特定できません。--repo owner/repo を指定するか、対象のコード repo 内で実行してください")
		}
		// 推論した repo が project と食い違うときは取り違え (例: store 内で実行) の可能性が高いので止める。
		if !strings.EqualFold(repoBase(inferred), project) {
			return fmt.Errorf("推論した repo (%s) が project (%s) と一致しません。意図した repo を --repo owner/repo で明示してください", inferred, project)
		}
		repo = inferred
	}
	out, err := ghRun(strings.NewReader(body), "issue", "create", "--repo", repo, "--title", title, "--body-file", "-")
	if err != nil {
		return fmt.Errorf("gh issue create に失敗しました:\n%s", out)
	}
	url := lastURL(out)
	if url == "" {
		return fmt.Errorf("issue は作成されましたが URL を取得できませんでした:\n%s", out)
	}
	// issue URL を frontmatter に記録し、updated も今に更新する (CLI が記録するので agent の手編集が不要)。
	now := time.Now().Format(time.RFC3339)
	if err := setFrontmatterFields(path, []fmField{{"issue", url}, {"updated", now}}); err != nil {
		return fmt.Errorf("issue: の記録に失敗しました (issue は作成済み: %s): %w", url, err)
	}
	fmt.Printf("created %s/%s %s\n%s\n", project, normalizeID(id), title, url)
	fmt.Printf("from: %s\n", storeRel(path)) // scoped sync 用 (agent-tasks sync --path <from>)
	return nil
}

// inferRepo は cwd の git リポジトリから GitHub の owner/repo を gh で推論する。
func inferRepo() (string, error) {
	out, err := exec.Command("gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner").Output()
	if err != nil {
		return "", err
	}
	repo := strings.TrimSpace(string(out))
	if repo == "" {
		return "", fmt.Errorf("repo を取得できません")
	}
	return repo, nil
}

// repoBase は owner/repo の repo 部分 (basename) を返す。host/owner/repo 形式にも耐える。
func repoBase(repo string) string {
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		return repo[i+1:]
	}
	return repo
}

// ghRun は gh を stdin 付きで実行し stdout を trim して返す。失敗時は stderr+stdout を返す。
func ghRun(stdin io.Reader, args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	cmd.Stdin = stdin
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(se.String() + "\n" + so.String()), err
	}
	return strings.TrimSpace(so.String()), nil
}

// lastURL は出力中で http(s):// で始まる最後の行を返す (gh issue create は URL を stdout に出す)。
func lastURL(out string) string {
	url := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			url = line
		}
	}
	return url
}

// taskBody はタスクファイルから frontmatter を除いた Markdown 本文を返す (先頭の空行は落とす)。
// frontmatter が無ければ全体を返す。issue 本文には内部メタ (branch/worktree/session) を出さない。
func taskBody(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := strings.TrimPrefix(string(data), "\ufeff")
	lines := strings.Split(s, "\n")
	// 先頭が "---" なら次の "---" までを frontmatter として飛ばす。
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				lines = lines[i+1:]
				break
			}
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
}

// fmField は setFrontmatterFields に渡す frontmatter の key/value。
type fmField struct{ key, val string }

// setFrontmatterFields は frontmatter 内の各 key を upsert する (存在すれば置換、無ければ
// 閉じ "---" の直前に挿入)。値は ":" や "#" を含むときダブルクォートで囲む (URL/日時)。
// flat な key: value 前提 (prs: のブロックリスト項目は字下げされるので誤マッチしない)。
func setFrontmatterFields(path string, fields []fmField) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	start, end := -1, -1
	for i, ln := range lines {
		if strings.TrimSpace(strings.TrimPrefix(ln, "\ufeff")) == "---" {
			if start == -1 {
				start = i
			} else {
				end = i
				break
			}
		}
	}
	if start == -1 || end == -1 {
		return fmt.Errorf("frontmatter が見つかりません: %s", path)
	}
	for _, f := range fields {
		newLine := f.key + ": " + quoteFrontmatterValue(f.val)
		replaced := false
		for i := start + 1; i < end; i++ {
			trimmed := strings.TrimSpace(lines[i])
			if trimmed == f.key+":" || strings.HasPrefix(trimmed, f.key+": ") {
				lines[i] = newLine
				replaced = true
				break
			}
		}
		if !replaced {
			lines = append(lines[:end:end], append([]string{newLine}, lines[end:]...)...)
			end++ // 挿入した分だけ閉じ "---" の位置が後ろへずれる
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// quoteFrontmatterValue は ":" や "#" を含む値をダブルクォートで囲む (YAML で安全に1行値にする)。
func quoteFrontmatterValue(v string) string {
	if v != "" && !strings.ContainsAny(v, ":#") {
		return v
	}
	return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
}

// issueSummary は show の末尾に出す issue リンクの1行を返す。issue が無ければ ""。
func issueSummary(t Task, c colors) string {
	if t.Issue == "" {
		return ""
	}
	return c.bold + "issue:" + c.reset + " " + t.Issue
}

// IssueProblem は issue: の値が URL として明らかに不正なものを表す doctor の検出結果。
type IssueProblem struct {
	Project string
	ID      string
	Detail  string
	Path    string
}

// findIssueProblems は issue: が http(s):// 形式かを軽く検査する (PR と同じ緩い検査)。
func findIssueProblems(tasks []Task) []IssueProblem {
	var out []IssueProblem
	for _, t := range tasks {
		if t.Issue == "" {
			continue
		}
		if !strings.HasPrefix(t.Issue, "http://") && !strings.HasPrefix(t.Issue, "https://") {
			out = append(out, IssueProblem{t.Project, t.ID, "issue: の値が URL ではない: " + t.Issue, t.Path})
		}
	}
	return out
}
