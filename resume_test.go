package main

import (
	"os"
	"strings"
	"testing"
)

// blocked タスク (blocked_at/blocked_reason 付き)。本文・進捗ログの保全確認も兼ねる。
const resumeBlockedTask = "---\nid: \"0001\"\nproject: webapp\ntitle: x\nstatus: blocked\nagent: codex\nsession:\nstarted_at: \"2026-06-28T00:00:00+09:00\"\nblocked_at: \"2026-06-29T00:00:00+09:00\"\nblocked_reason: API 待ち\ncreated: \"2026-06-28T00:00:00+09:00\"\nupdated: \"2026-06-29T00:00:00+09:00\"\n---\n\n# 要件\n本文はそのまま。\n\n## 進捗ログ\n- 2026-06-28 登録\n"

// resume は blocked を in-progress に戻し、blocked_* を落とし、started_at・agent・本文を保全する。
func TestResumeFromBlocked(t *testing.T) {
	store := t.TempDir()
	writeTask(t, store, "webapp", "", "0001-x.md", resumeBlockedTask)
	t.Setenv("AGENT_TASKS_STORE", store)

	if err := cmdResume([]string{"webapp", "0001"}); err != nil {
		t.Fatal(err)
	}
	pt, _ := parseTask(store + "/webapp/0001-x.md")
	if pt.Status != "in-progress" {
		t.Errorf("status = %q, want in-progress", pt.Status)
	}
	if pt.BlockedAt != "" || pt.BlockedReason != "" {
		t.Errorf("blocked_* が残っている: at=%q reason=%q", pt.BlockedAt, pt.BlockedReason)
	}
	if pt.StartedAt != "2026-06-28T00:00:00+09:00" {
		t.Errorf("started_at が変わった (再開では保持するはず): %q", pt.StartedAt)
	}
	if pt.Agent != "codex" {
		t.Errorf("agent = %q, want codex (--agent 無しなら既存を保持)", pt.Agent)
	}
	b, _ := os.ReadFile(store + "/webapp/0001-x.md")
	raw := string(b)
	for _, want := range []string{"# 要件", "本文はそのまま。", "- 2026-06-28 登録"} {
		if !strings.Contains(raw, want) {
			t.Errorf("本文/進捗ログが保全されていない (%q なし):\n%s", want, raw)
		}
	}
}

// resume は review からも in-progress に戻せる。
func TestResumeFromReview(t *testing.T) {
	store := t.TempDir()
	body := "---\nid: \"0001\"\nproject: webapp\ntitle: x\nstatus: review\nagent: claude\nsession:\nstarted_at: \"2026-06-28T00:00:00+09:00\"\ncreated: \"2026-06-28T00:00:00+09:00\"\nupdated: \"2026-06-30T00:00:00+09:00\"\n---\n"
	writeTask(t, store, "webapp", "", "0001-x.md", body)
	t.Setenv("AGENT_TASKS_STORE", store)

	if err := cmdResume([]string{"webapp", "0001"}); err != nil {
		t.Fatal(err)
	}
	pt, _ := parseTask(store + "/webapp/0001-x.md")
	if pt.Status != "in-progress" {
		t.Errorf("status = %q, want in-progress", pt.Status)
	}
}

// --agent を明示すると既存より優先される。--session は記録される。
func TestResumeOverrideAgentSession(t *testing.T) {
	store := t.TempDir()
	writeTask(t, store, "webapp", "", "0001-x.md", resumeBlockedTask)
	t.Setenv("AGENT_TASKS_STORE", store)

	if err := cmdResume([]string{"webapp", "0001", "--agent", "claude", "--session", "https://claude.ai/code/session_abc"}); err != nil {
		t.Fatal(err)
	}
	pt, _ := parseTask(store + "/webapp/0001-x.md")
	if pt.Agent != "claude" {
		t.Errorf("agent = %q, want claude (--agent 明示が優先)", pt.Agent)
	}
	if pt.Session != "https://claude.ai/code/session_abc" {
		t.Errorf("session = %q, want 記録された URL", pt.Session)
	}
}

// todo は resume 対象外 (start へ誘導)。
func TestResumeRejectsTodo(t *testing.T) {
	store := t.TempDir()
	body := "---\nid: \"0001\"\nproject: webapp\ntitle: x\nstatus: todo\nagent:\nsession:\ncreated: \"2026-06-28T00:00:00+09:00\"\nupdated: \"2026-06-28T00:00:00+09:00\"\n---\n"
	writeTask(t, store, "webapp", "", "0001-x.md", body)
	t.Setenv("AGENT_TASKS_STORE", store)

	err := cmdResume([]string{"webapp", "0001"})
	if err == nil {
		t.Fatal("todo の resume はエラーになるべき")
	}
	if !strings.Contains(err.Error(), "start") {
		t.Errorf("エラーが start へ誘導していない: %v", err)
	}
	// 状態は変わっていないこと。
	pt, _ := parseTask(store + "/webapp/0001-x.md")
	if pt.Status != "todo" {
		t.Errorf("status が変わった: %q", pt.Status)
	}
}

// done は resume 対象外 (worktree 作り直しのため start へ誘導)。
func TestResumeRejectsDone(t *testing.T) {
	store := t.TempDir()
	body := "---\nid: \"0001\"\nproject: webapp\ntitle: x\nstatus: done\nagent: claude\nsession:\nstarted_at: \"2026-06-28T00:00:00+09:00\"\ncompleted_at: \"2026-06-30T00:00:00+09:00\"\ncreated: \"2026-06-28T00:00:00+09:00\"\nupdated: \"2026-06-30T00:00:00+09:00\"\n---\n"
	writeTask(t, store, "webapp", "", "0001-x.md", body)
	t.Setenv("AGENT_TASKS_STORE", store)

	err := cmdResume([]string{"webapp", "0001"})
	if err == nil {
		t.Fatal("done の resume はエラーになるべき")
	}
	if !strings.Contains(err.Error(), "start") {
		t.Errorf("エラーが start へ誘導していない: %v", err)
	}
}

// in-progress は冪等に成功する (updated 更新・blocked_* 落とすだけ)。
func TestResumeIdempotentOnInProgress(t *testing.T) {
	store := t.TempDir()
	// 不整合で blocked_at が残った in-progress を渡し、resume が掃除することも確認。
	body := "---\nid: \"0001\"\nproject: webapp\ntitle: x\nstatus: in-progress\nagent: claude\nsession:\nstarted_at: \"2026-06-28T00:00:00+09:00\"\nblocked_at: \"2026-06-29T00:00:00+09:00\"\nblocked_reason: stale\ncreated: \"2026-06-28T00:00:00+09:00\"\nupdated: \"2026-06-29T00:00:00+09:00\"\n---\n"
	writeTask(t, store, "webapp", "", "0001-x.md", body)
	t.Setenv("AGENT_TASKS_STORE", store)

	if err := cmdResume([]string{"webapp", "0001"}); err != nil {
		t.Fatal(err)
	}
	pt, _ := parseTask(store + "/webapp/0001-x.md")
	if pt.Status != "in-progress" {
		t.Errorf("status = %q, want in-progress", pt.Status)
	}
	if pt.BlockedAt != "" || pt.BlockedReason != "" {
		t.Errorf("blocked_* が掃除されていない: at=%q reason=%q", pt.BlockedAt, pt.BlockedReason)
	}
}
