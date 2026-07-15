package main

import (
	"strings"
	"testing"
	"time"
)

// parseTask は kind: を読み、IsHuman / effectiveKind が種別を返す。省略時は code 既定。
func TestParseKind(t *testing.T) {
	store := t.TempDir()
	human := writeTask(t, store, "webapp", "", "0001-deploy.md",
		"---\nid: \"0001\"\nproject: webapp\nstatus: todo\nkind: human\ntitle: デプロイ設定変更\n---\n")
	code := writeTask(t, store, "webapp", "", "0002-feat.md",
		"---\nid: \"0002\"\nproject: webapp\nstatus: todo\ntitle: 機能追加\n---\n")

	ht, err := parseTask(human)
	if err != nil {
		t.Fatal(err)
	}
	if ht.Kind != kindHuman || !ht.IsHuman() || effectiveKind(ht) != kindHuman {
		t.Errorf("human task: Kind=%q IsHuman=%v effectiveKind=%q", ht.Kind, ht.IsHuman(), effectiveKind(ht))
	}

	ct, err := parseTask(code)
	if err != nil {
		t.Fatal(err)
	}
	if ct.Kind != "" || ct.IsHuman() || effectiveKind(ct) != kindCode {
		t.Errorf("code task (kind 省略): Kind=%q IsHuman=%v effectiveKind=%q", ct.Kind, ct.IsHuman(), effectiveKind(ct))
	}
}

// displayTitle は human タスクにだけ識別プレフィックスを付ける (検索対象の Title は変えない)。
func TestDisplayTitleHuman(t *testing.T) {
	h := Task{Title: "顧客に確認", Kind: kindHuman}
	if got := displayTitle(h); !strings.HasPrefix(got, humanTitlePrefix) || !strings.Contains(got, "顧客に確認") {
		t.Errorf("human displayTitle = %q, want %q プレフィックス付き", got, humanTitlePrefix)
	}
	c := Task{Title: "機能追加"}
	if got := displayTitle(c); got != "機能追加" {
		t.Errorf("code displayTitle = %q, want 装飾なし", got)
	}
}

// displayTitle は簡易登録 (draft) タスクに [draft] プレフィックスを付ける (Title 自体は変えない)。
func TestDisplayTitleDraft(t *testing.T) {
	d := Task{Title: "あとで詳細化するやつ", Draft: true}
	if got := displayTitle(d); !strings.HasPrefix(got, draftTitlePrefix) || !strings.Contains(got, "あとで詳細化するやつ") {
		t.Errorf("draft displayTitle = %q, want %q プレフィックス付き", got, draftTitlePrefix)
	}
	// draft でなければ装飾しない。
	if got := displayTitle(Task{Title: "通常タスク"}); got != "通常タスク" {
		t.Errorf("非 draft displayTitle = %q, want 装飾なし", got)
	}
}

// selectTasks の --kind フィルタは human / code を実効種別で絞る (kind 省略は code 側)。
func TestSelectTasksKindFilter(t *testing.T) {
	store := t.TempDir()
	t.Setenv("AGENT_TASKS_STORE", store)
	writeTask(t, store, "webapp", "", "0001-h.md",
		"---\nid: \"0001\"\nproject: webapp\nstatus: todo\nkind: human\ntitle: h\n---\n")
	writeTask(t, store, "webapp", "", "0002-c.md",
		"---\nid: \"0002\"\nproject: webapp\nstatus: todo\ntitle: c\n---\n")

	human, _, _, err := selectTasks("", nil, false, true, false, "", false, kindHuman)
	if err != nil {
		t.Fatal(err)
	}
	if len(human) != 1 || human[0].ID != "0001" {
		t.Errorf("--kind human = %+v, want only 0001", human)
	}

	code, _, _, err := selectTasks("", nil, false, true, false, "", false, kindCode)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 1 || code[0].ID != "0002" {
		t.Errorf("--kind code = %+v, want only 0002 (kind 省略)", code)
	}

	all, _, _, err := selectTasks("", nil, false, true, false, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("--kind なし = %d 件, want 2", len(all))
	}
}

// findKindProblems は human/code/空 以外の kind: 値 (typo) だけを拾う。
func TestFindKindProblems(t *testing.T) {
	tasks := []Task{
		{ID: "0001", Kind: kindHuman},
		{ID: "0002", Kind: kindCode},
		{ID: "0003", Kind: ""},
		{ID: "0004", Kind: "humann"}, // typo → 検出
	}
	probs := findKindProblems(tasks)
	if len(probs) != 1 || probs[0].ID != "0004" {
		t.Fatalf("findKindProblems = %+v, want only 0004", probs)
	}
}

// JSON 出力は human のとき kind を含み、code (省略) では落とす (omitempty)。
func TestKindJSON(t *testing.T) {
	now := time.Now()
	if j := toTaskJSON(Task{Kind: kindHuman}, now); j.Kind != kindHuman {
		t.Errorf("human JSON Kind = %q, want %q", j.Kind, kindHuman)
	}
	if j := toTaskJSON(Task{}, now); j.Kind != "" {
		t.Errorf("code JSON Kind = %q, want 空 (omitempty)", j.Kind)
	}
	// human タスクはセッションを持たないので、in-progress でも session_state を付けない。
	if j := toTaskJSON(Task{Status: "in-progress", Kind: kindHuman}, now); j.SessionState != "" {
		t.Errorf("human in-progress の session_state = %q, want 空", j.SessionState)
	}
}
