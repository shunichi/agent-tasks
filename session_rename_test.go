package main

import "testing"

// isClaudeSession は /rename (Claude 固有) を撃ってよいかの判定。fail-safe: 確証がなければ false。
func TestIsClaudeSessionFromEnv(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "uuid-from-env")
	// env が最優先。herdr は参照しないはず (参照したら失敗)。
	orig := herdrRun
	herdrRun = func(args ...string) ([]byte, error) {
		t.Fatalf("herdrRun は呼ばれないはず (CLAUDE_CODE_SESSION_ID 優先): %v", args)
		return nil, nil
	}
	t.Cleanup(func() { herdrRun = orig })

	if !isClaudeSession() {
		t.Error("CLAUDE_CODE_SESSION_ID があれば Claude と判定すべき")
	}
}

func TestIsClaudeSessionFromHerdrClaude(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "") // env を無効化して herdr 経路へ
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	t.Setenv("HERDR_PANE_ID", "w3:p1")
	const js = `{"result":{"agent":{"agent":"claude","agent_status":"working","pane_id":"w3:p1"}}}`
	stubHerdrRun(t, []byte(js), nil)

	if !isClaudeSession() {
		t.Error("herdr の自 pane agent=claude なら Claude と判定すべき")
	}
}

func TestIsClaudeSessionFromHerdrCodex(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/h.sock")
	t.Setenv("HERDR_PANE_ID", "w3:p1")
	const js = `{"result":{"agent":{"agent":"codex","agent_status":"working","pane_id":"w3:p1"}}}`
	stubHerdrRun(t, []byte(js), nil)

	if isClaudeSession() {
		t.Error("herdr の自 pane agent=codex なら非 Claude と判定すべき (/rename を撃たない)")
	}
}

func TestIsClaudeSessionNone(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("HERDR_ENV", "0")
	if isClaudeSession() {
		t.Error("env も herdr も無ければ確証なし = 非 Claude と判定すべき (fail-safe)")
	}
}
