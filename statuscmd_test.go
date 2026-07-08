package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStderr は f 実行中の os.Stderr を捕捉して返す。
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	f()
	w.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const doneBlockTaskTodo = "---\nid: \"0001\"\nproject: webapp\ntitle: x\nstatus: in-progress\nagent: claude\nsession:\nstarted_at: \"2026-06-28T00:00:00+09:00\"\ncreated: \"2026-06-28T00:00:00+09:00\"\nupdated: \"2026-06-28T00:00:00+09:00\"\n---\n\n# 要件\n本文はそのまま。\n\n## 進捗ログ\n- 2026-06-28 登録\n"

// done は status=done + completed_at を確定し、本文/進捗ログを保全する。
func TestDoneBasic(t *testing.T) {
	store := t.TempDir()
	writeTask(t, store, "webapp", "", "0001-x.md", doneBlockTaskTodo)
	t.Setenv("AGENT_TASKS_STORE", store)

	if err := cmdDone([]string{"webapp", "0001"}); err != nil {
		t.Fatal(err)
	}
	pt, _ := parseTask(store + "/webapp/0001-x.md")
	if pt.Status != "done" {
		t.Errorf("status = %q, want done", pt.Status)
	}
	if pt.CompletedAt == "" {
		t.Error("completed_at が付いていない")
	}
	// 本文・進捗ログは保全される。
	b, _ := os.ReadFile(store + "/webapp/0001-x.md")
	raw := string(b)
	for _, want := range []string{"# 要件", "本文はそのまま。", "- 2026-06-28 登録"} {
		if !strings.Contains(raw, want) {
			t.Errorf("本文/進捗ログが保全されていない (%q なし):\n%s", want, raw)
		}
	}
}

// done --review は status=review にし、completed_at は付けない。
func TestDoneReview(t *testing.T) {
	store := t.TempDir()
	writeTask(t, store, "webapp", "", "0001-x.md", doneBlockTaskTodo)
	t.Setenv("AGENT_TASKS_STORE", store)

	if err := cmdDone([]string{"webapp", "0001", "--review"}); err != nil {
		t.Fatal(err)
	}
	pt, _ := parseTask(store + "/webapp/0001-x.md")
	if pt.Status != "review" {
		t.Errorf("status = %q, want review", pt.Status)
	}
	if pt.CompletedAt != "" {
		t.Errorf("review では completed_at を付けないはず: %q", pt.CompletedAt)
	}
}

// 再 done は最初の completed_at を保持する (上書きしない)。
func TestDoneKeepsFirstCompletedAt(t *testing.T) {
	store := t.TempDir()
	body := "---\nid: \"0001\"\nproject: webapp\ntitle: x\nstatus: done\nagent: claude\nsession:\nstarted_at: \"2026-06-28T00:00:00+09:00\"\ncompleted_at: \"2026-06-30T10:00:00+09:00\"\ncreated: \"2026-06-28T00:00:00+09:00\"\nupdated: \"2026-06-30T10:00:00+09:00\"\n---\n"
	writeTask(t, store, "webapp", "", "0001-x.md", body)
	t.Setenv("AGENT_TASKS_STORE", store)

	if err := cmdDone([]string{"webapp", "0001"}); err != nil {
		t.Fatal(err)
	}
	pt, _ := parseTask(store + "/webapp/0001-x.md")
	if pt.CompletedAt != "2026-06-30T10:00:00+09:00" {
		t.Errorf("completed_at が上書きされた: %q", pt.CompletedAt)
	}
}

// blocked から done へ抜けると blocked_at/blocked_reason を落とす。
func TestDoneClearsBlocked(t *testing.T) {
	store := t.TempDir()
	body := "---\nid: \"0001\"\nproject: webapp\ntitle: x\nstatus: blocked\nagent: claude\nsession:\nblocked_at: \"2026-06-29T00:00:00+09:00\"\nblocked_reason: 確認待ち\ncreated: \"2026-06-28T00:00:00+09:00\"\nupdated: \"2026-06-29T00:00:00+09:00\"\n---\n"
	writeTask(t, store, "webapp", "", "0001-x.md", body)
	t.Setenv("AGENT_TASKS_STORE", store)

	if err := cmdDone([]string{"webapp", "0001"}); err != nil {
		t.Fatal(err)
	}
	pt, _ := parseTask(store + "/webapp/0001-x.md")
	if pt.BlockedAt != "" || pt.BlockedReason != "" {
		t.Errorf("blocked_* が残っている: at=%q reason=%q", pt.BlockedAt, pt.BlockedReason)
	}
}

// started_at が無いまま done すると、完了後の整合チェックが警告する (案B: start を経ない done の再発防止)。
func TestDoneWarnsMissingStartedAt(t *testing.T) {
	store := t.TempDir()
	// started_at 無し (claim を経ずに done されるケース)。
	body := "---\nid: \"0001\"\nproject: webapp\ntitle: x\nstatus: in-progress\nagent: claude\nsession:\ncreated: \"2026-06-28T00:00:00+09:00\"\nupdated: \"2026-06-28T00:00:00+09:00\"\n---\n"
	writeTask(t, store, "webapp", "", "0001-x.md", body)
	t.Setenv("AGENT_TASKS_STORE", store)

	var doneErr error
	stderr := captureStderr(t, func() { doneErr = cmdDone([]string{"webapp", "0001"}) })
	if doneErr != nil {
		t.Fatal(doneErr)
	}
	// done 自体は成功する (警告は出すが失敗させない)。
	pt, _ := parseTask(store + "/webapp/0001-x.md")
	if pt.Status != "done" {
		t.Errorf("status = %q, want done", pt.Status)
	}
	if !strings.Contains(stderr, "started_at が無い") {
		t.Errorf("started_at 欠落の警告が stderr に出ていない: %q", stderr)
	}
}

// 整合の取れた done (started_at あり) では警告を出さない。
func TestDoneNoWarnWhenConsistent(t *testing.T) {
	store := t.TempDir()
	writeTask(t, store, "webapp", "", "0001-x.md", doneBlockTaskTodo) // started_at あり
	t.Setenv("AGENT_TASKS_STORE", store)

	stderr := captureStderr(t, func() {
		if err := cmdDone([]string{"webapp", "0001"}); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(stderr, "警告") {
		t.Errorf("整合の取れた done で警告が出た: %q", stderr)
	}
}

// block は status=blocked + blocked_at + blocked_reason を確定する。
func TestBlockBasic(t *testing.T) {
	store := t.TempDir()
	writeTask(t, store, "webapp", "", "0001-x.md", doneBlockTaskTodo)
	t.Setenv("AGENT_TASKS_STORE", store)

	if err := cmdBlock([]string{"webapp", "0001", "--reason", "API 仕様の確認待ち"}); err != nil {
		t.Fatal(err)
	}
	pt, _ := parseTask(store + "/webapp/0001-x.md")
	if pt.Status != "blocked" {
		t.Errorf("status = %q, want blocked", pt.Status)
	}
	if pt.BlockedAt == "" {
		t.Error("blocked_at が付いていない")
	}
	if pt.BlockedReason != "API 仕様の確認待ち" {
		t.Errorf("blocked_reason = %q", pt.BlockedReason)
	}
}

// block は --reason 必須。
func TestBlockRequiresReason(t *testing.T) {
	store := t.TempDir()
	writeTask(t, store, "webapp", "", "0001-x.md", doneBlockTaskTodo)
	t.Setenv("AGENT_TASKS_STORE", store)

	if err := cmdBlock([]string{"webapp", "0001"}); err == nil {
		t.Error("--reason 無しの block はエラーを期待")
	}
}

// blocked_reason に ':' を含む理由はクォートされ、parseTask で正しく読み戻せる。
func TestBlockReasonWithColon(t *testing.T) {
	store := t.TempDir()
	writeTask(t, store, "webapp", "", "0001-x.md", doneBlockTaskTodo)
	t.Setenv("AGENT_TASKS_STORE", store)

	reason := "待ち: 仕様確認"
	if err := cmdBlock([]string{"webapp", "0001", "--reason", reason}); err != nil {
		t.Fatal(err)
	}
	pt, _ := parseTask(store + "/webapp/0001-x.md")
	if pt.BlockedReason != reason {
		t.Errorf("blocked_reason = %q, want %q", pt.BlockedReason, reason)
	}
}
