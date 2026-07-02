package main

import (
	"os"
	"strings"
	"testing"
)

// applyFrontmatterEdits は既知キーの差し替え・追記・削除を行い、本文/コメント/リスト項目を保全する。
func TestApplyFrontmatterEdits(t *testing.T) {
	src := `---
id: "0001"
project: webapp
title: サンプル
status: todo
agent:
session:
# start したとき付ける:
prs:
  - https://example.com/pull/1
blocked_at: "2026-06-28T14:30:00+09:00"
blocked_reason: 確認待ち
---

# 要件
本文はそのまま。

## 進捗ログ
- 2026-06-28 登録
`
	got, err := applyFrontmatterEdits([]byte(src),
		[]fmKV{{"status", "in-progress"}, {"agent", "claude"}, {"started_at", "2026-07-02T09:00:00+09:00"}, {"updated", "2026-07-02T09:00:00+09:00"}},
		[]string{"blocked_at", "blocked_reason", "completed_at"},
	)
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)

	// 差し替え
	if !strings.Contains(out, "status: in-progress") {
		t.Errorf("status が in-progress に差し替わっていない:\n%s", out)
	}
	if !strings.Contains(out, "agent: claude") {
		t.Errorf("agent が差し替わっていない:\n%s", out)
	}
	// 追記 (: を含むのでクォートされる)
	if !strings.Contains(out, `started_at: "2026-07-02T09:00:00+09:00"`) {
		t.Errorf("started_at が追記されていない:\n%s", out)
	}
	// 削除
	if strings.Contains(out, "blocked_at") || strings.Contains(out, "blocked_reason") {
		t.Errorf("blocked_at/blocked_reason が削除されていない:\n%s", out)
	}
	// 保全: 本文・コメント・リスト項目
	if !strings.Contains(out, "# 要件") || !strings.Contains(out, "本文はそのまま。") {
		t.Errorf("本文が保全されていない:\n%s", out)
	}
	if !strings.Contains(out, "# start したとき付ける:") {
		t.Errorf("frontmatter 内コメントが保全されていない:\n%s", out)
	}
	if !strings.Contains(out, "  - https://example.com/pull/1") {
		t.Errorf("prs のリスト項目が保全されていない:\n%s", out)
	}
	// parseTask で読み直しても整合する
	tmp := writeTmp(t, out)
	pt, err := parseTask(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if pt.Status != "in-progress" || pt.Agent != "claude" || pt.StartedAt != "2026-07-02T09:00:00+09:00" {
		t.Errorf("parseTask 不整合: %+v", pt)
	}
	if pt.BlockedAt != "" || pt.BlockedReason != "" {
		t.Errorf("blocked_* が残っている: %+v", pt)
	}
	if len(pt.PRs) != 1 || pt.PRs[0] != "https://example.com/pull/1" {
		t.Errorf("prs が壊れた: %+v", pt.PRs)
	}
}

// frontmatter が無いファイルはエラーにする。
func TestApplyFrontmatterEditsNoFrontmatter(t *testing.T) {
	if _, err := applyFrontmatterEdits([]byte("# 見出し\n本文\n"), []fmKV{{"status", "x"}}, nil); err == nil {
		t.Error("frontmatter 無しでエラーを期待")
	}
}

// claim は todo を in-progress に確定し、started_at を付ける。
func TestClaimBasic(t *testing.T) {
	store := t.TempDir()
	writeTask(t, store, "webapp", "", "0001-x.md", "---\nid: \"0001\"\nproject: webapp\ntitle: x\nstatus: todo\nagent:\nsession:\ncreated: \"2026-06-28T00:00:00+09:00\"\nupdated: \"2026-06-28T00:00:00+09:00\"\n---\n\n# 要件\n")
	t.Setenv("AGENT_TASKS_STORE", store)

	if err := cmdClaim([]string{"webapp", "0001", "--agent", "claude"}); err != nil {
		t.Fatal(err)
	}
	pt, _ := parseTask(store + "/webapp/0001-x.md")
	if pt.Status != "in-progress" {
		t.Errorf("status = %q, want in-progress", pt.Status)
	}
	if pt.Agent != "claude" {
		t.Errorf("agent = %q, want claude", pt.Agent)
	}
	if pt.StartedAt == "" {
		t.Error("started_at が付いていない")
	}
}

// 二重着手ガード: 既に in-progress なら別セッションの claim は拒否 (fail-safe)。--force で通る。
func TestClaimDoubleGuard(t *testing.T) {
	store := t.TempDir()
	writeTask(t, store, "webapp", "", "0001-x.md", "---\nid: \"0001\"\nproject: webapp\ntitle: x\nstatus: in-progress\nagent: claude\nsession:\nstarted_at: \"2026-06-28T00:00:00+09:00\"\ncreated: \"2026-06-28T00:00:00+09:00\"\nupdated: \"2026-06-28T00:00:00+09:00\"\n---\n")
	t.Setenv("AGENT_TASKS_STORE", store)

	if err := cmdClaim([]string{"webapp", "0001"}); err == nil {
		t.Error("既に in-progress の claim はエラーを期待 (fail-safe)")
	}
	// --force なら通り、started_at は初回のものを保持 (上書きしない)。
	if err := cmdClaim([]string{"webapp", "0001", "--force"}); err != nil {
		t.Fatalf("--force で通るはず: %v", err)
	}
	pt, _ := parseTask(store + "/webapp/0001-x.md")
	if pt.StartedAt != "2026-06-28T00:00:00+09:00" {
		t.Errorf("started_at が上書きされた: %q", pt.StartedAt)
	}
}

// --release --to blocked は状態を戻し、started_at を保持する。--to todo は着手情報を落とす。
func TestClaimRelease(t *testing.T) {
	store := t.TempDir()
	body := "---\nid: \"0001\"\nproject: webapp\ntitle: x\nstatus: in-progress\nagent: claude\nsession:\nstarted_at: \"2026-06-28T00:00:00+09:00\"\ncreated: \"2026-06-28T00:00:00+09:00\"\nupdated: \"2026-06-28T00:00:00+09:00\"\n---\n"
	writeTask(t, store, "webapp", "", "0001-x.md", body)
	t.Setenv("AGENT_TASKS_STORE", store)

	if err := cmdClaim([]string{"webapp", "0001", "--release", "--to", "blocked"}); err != nil {
		t.Fatal(err)
	}
	pt, _ := parseTask(store + "/webapp/0001-x.md")
	if pt.Status != "blocked" {
		t.Errorf("status = %q, want blocked", pt.Status)
	}
	if pt.StartedAt == "" {
		t.Error("--to blocked では started_at を保持するはず")
	}

	// todo へ戻すと着手情報が落ちる。
	writeTask(t, store, "webapp", "", "0001-x.md", body)
	if err := cmdClaim([]string{"webapp", "0001", "--release"}); err != nil {
		t.Fatal(err)
	}
	pt, _ = parseTask(store + "/webapp/0001-x.md")
	if pt.Status != "todo" {
		t.Errorf("status = %q, want todo", pt.Status)
	}
	if pt.StartedAt != "" || pt.Agent != "" {
		t.Errorf("--to todo では着手情報を落とすはず: started_at=%q agent=%q", pt.StartedAt, pt.Agent)
	}
}

func writeTmp(t *testing.T, content string) string {
	t.Helper()
	p := t.TempDir() + "/task.md"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
