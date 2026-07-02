package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeModelID(t *testing.T) {
	cases := map[string]string{
		"claude-haiku-4-5-20251001": "claude-haiku-4-5",
		"claude-opus-4-8":           "claude-opus-4-8",
		"claude-sonnet-4-6":         "claude-sonnet-4-6",
		"<synthetic>":               "<synthetic>",
		"claude-opus-4-8-abc":       "claude-opus-4-8-abc", // 8 桁数字でない suffix は残す
	}
	for in, want := range cases {
		if got := normalizeModelID(in); got != want {
			t.Errorf("normalizeModelID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanIntAndCount(t *testing.T) {
	if got := humanInt(1234567); got != "1,234,567" {
		t.Errorf("humanInt = %q, want 1,234,567", got)
	}
	if got := humanInt(0); got != "0" {
		t.Errorf("humanInt(0) = %q", got)
	}
	if got := humanCount(1234); got != "1.2k" {
		t.Errorf("humanCount(1234) = %q, want 1.2k", got)
	}
	if got := humanCount(2_500_000); got != "2.5M" {
		t.Errorf("humanCount = %q, want 2.5M", got)
	}
	if got := humanCount(999); got != "999" {
		t.Errorf("humanCount(999) = %q, want 999", got)
	}
}

// writeTranscript は 1 行 JSONL を持つ transcript を projects/<enc>/<sid>.jsonl に書く。
func writeTranscript(t *testing.T, projectsDir, enc, sid string, lines []string) {
	t.Helper()
	dir := filepath.Join(projectsDir, enc)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, sid+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAccumulateTranscriptDedupAndWindow(t *testing.T) {
	dir := t.TempDir()
	// 同じ message.id (msgA) を 3 回、別 (msgB) を 1 回。dedup で msgA は 1 回だけ数える。
	// msgC は窓外の timestamp。
	lineA := `{"type":"assistant","timestamp":"2026-07-02T10:00:00.000Z","message":{"id":"msgA","model":"claude-opus-4-8","usage":{"input_tokens":100,"output_tokens":10,"cache_read_input_tokens":1000,"cache_creation_input_tokens":50,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":50}}}}`
	lineB := `{"type":"assistant","timestamp":"2026-07-02T10:05:00.000Z","message":{"id":"msgB","model":"claude-opus-4-8","usage":{"input_tokens":5,"output_tokens":20,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`
	lineC := `{"type":"assistant","timestamp":"2026-07-02T23:00:00.000Z","message":{"id":"msgC","model":"claude-opus-4-8","usage":{"input_tokens":999,"output_tokens":999,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`
	writeTranscript(t, dir, "enc", "sid1", []string{lineA, lineA, lineA, lineB, lineC})

	winStart, _ := time.Parse(time.RFC3339, "2026-07-02T09:00:00Z")
	winEnd, _ := time.Parse(time.RFC3339, "2026-07-02T12:00:00Z")
	seen := map[string]bool{}
	byModel := map[string]*usageAgg{}
	if err := accumulateTranscript(filepath.Join(dir, "enc", "sid1.jsonl"), winStart, winEnd, seen, byModel); err != nil {
		t.Fatal(err)
	}
	agg := byModel["claude-opus-4-8"]
	if agg == nil {
		t.Fatal("opus の集計が無い")
	}
	// msgA (1 回) + msgB。msgC は窓外で除外。
	if agg.input != 105 { // 100 + 5
		t.Errorf("input = %d, want 105 (dedup + 窓外除外)", agg.input)
	}
	if agg.output != 30 { // 10 + 20
		t.Errorf("output = %d, want 30", agg.output)
	}
	if agg.cacheRead != 1000 {
		t.Errorf("cacheRead = %d, want 1000", agg.cacheRead)
	}
	if agg.cacheWrite1h != 50 || agg.cacheWrite5m != 0 {
		t.Errorf("cacheWrite 5m/1h = %d/%d, want 0/50", agg.cacheWrite5m, agg.cacheWrite1h)
	}
}

func TestComputeTaskCostEndToEnd(t *testing.T) {
	state := t.TempDir()
	t.Setenv("AGENT_TASKS_STATE_DIR", state)
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	// タスク→セッションの link を書く (worktree キー = worktree basename)。
	if err := writeSessionLink("proj--0001", "sidX", now); err != nil {
		t.Fatal(err)
	}
	// transcript: opus で入力 1000, 出力 500 (窓内)。
	line := `{"type":"assistant","timestamp":"2026-07-02T11:00:00.000Z","message":{"id":"m1","model":"claude-opus-4-8","usage":{"input_tokens":1000,"output_tokens":500,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`
	writeTranscript(t, filepath.Join(cfg, "projects"), "proj", "sidX", []string{line})

	task := Task{
		Project: "proj", ID: "0001", Title: "T",
		Worktree:  "../proj--0001",
		StartedAt: "2026-07-02T09:00:00Z",
	}
	res, ok, err := computeTaskCost(task, now)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("link があるのに resolved でない")
	}
	if res.Transcripts != 1 {
		t.Errorf("transcripts = %d, want 1", res.Transcripts)
	}
	if res.Usage.input != 1000 || res.Usage.output != 500 {
		t.Errorf("usage = %+v, want in 1000 / out 500", res.Usage)
	}
	// 概算コスト = 1000/1e6*5 + 500/1e6*25 = 0.005 + 0.0125 = 0.0175
	want := 1000.0/1e6*5 + 500.0/1e6*25
	if res.USD < want-1e-9 || res.USD > want+1e-9 {
		t.Errorf("USD = %v, want %v", res.USD, want)
	}
	if !res.Priced {
		t.Error("opus は単価表にあるので Priced=true のはず")
	}
}

func TestComputeTaskCostNoLink(t *testing.T) {
	t.Setenv("AGENT_TASKS_STATE_DIR", t.TempDir())
	// link 無し → resolved=false。
	task := Task{Project: "proj", ID: "0002", Title: "T", Worktree: "../proj--0002"}
	_, ok, err := computeTaskCost(task, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("link が無いので resolved=false のはず")
	}
}

func TestComputeTaskCostUnpricedModel(t *testing.T) {
	state := t.TempDir()
	t.Setenv("AGENT_TASKS_STATE_DIR", state)
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if err := writeSessionLink("proj--0003", "sidY", now); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","timestamp":"2026-07-02T11:00:00.000Z","message":{"id":"m1","model":"claude-future-99","usage":{"input_tokens":1000,"output_tokens":500,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`
	writeTranscript(t, filepath.Join(cfg, "projects"), "proj", "sidY", []string{line})
	task := Task{Project: "proj", ID: "0003", Title: "T", Worktree: "../proj--0003", StartedAt: "2026-07-02T09:00:00Z"}
	res, ok, err := computeTaskCost(task, now)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if res.Priced {
		t.Error("未収載モデルなので Priced=false のはず")
	}
	if len(res.UnpricedModels) != 1 || res.UnpricedModels[0] != "claude-future-99" {
		t.Errorf("UnpricedModels = %v", res.UnpricedModels)
	}
	if res.Usage.input != 1000 { // トークンは数える (コストだけ 0)
		t.Errorf("未収載でもトークンは数える: input = %d", res.Usage.input)
	}
}
