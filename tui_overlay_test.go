package main

import (
	"slices"
	"strings"
	"testing"
)

func TestFocusedPaneCwd(t *testing.T) {
	cases := []struct {
		name string
		ctx  string
		want string
	}{
		{"focused_pane_cwd 優先", `{"focused_pane_cwd":"/x/rails-app","workspace_cwd":"/x/ws"}`, "/x/rails-app"},
		{"focused 無ければ workspace", `{"workspace_cwd":"/x/ws"}`, "/x/ws"},
		{"どちらも無し", `{"workspace_id":"wA"}`, ""},
		{"壊れた JSON", `{not json`, ""},
		{"空", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", c.ctx)
			if got := focusedPaneCwd(); got != c.want {
				t.Errorf("focusedPaneCwd() = %q, want %q", got, c.want)
			}
		})
	}
}

// コンテキストの focused_pane_cwd を --cwd に渡して pane を開く。placement/size は
// manifest ([[panes]]) 由来なので CLI フラグでは渡さない (0156)。
func TestTuiOverlayPassesFocusedCwd(t *testing.T) {
	calls := stubHerdrRun(t, []byte("{}"), nil)
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", `{"focused_pane_cwd":"/x/rails-app"}`)
	if err := cmdTuiOverlay(nil); err != nil {
		t.Fatalf("cmdTuiOverlay: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("herdrRun 呼び出し回数 = %d, want 1 (%v)", len(*calls), *calls)
	}
	want := []string{"plugin", "pane", "open", "--plugin", "agent-tasks", "--entrypoint", "tui", "--cwd", "/x/rails-app"}
	if !slices.Equal((*calls)[0], want) {
		t.Fatalf("herdrRun args = %v\nwant %v", (*calls)[0], want)
	}
	// placement/size は manifest 由来。CLI フラグに混ざっていないこと (二重管理防止)。
	for _, bad := range []string{"--placement", "--width", "--height"} {
		if slices.Contains((*calls)[0], bad) {
			t.Errorf("%s を CLI で渡している (manifest を単一の情報源にすべき): %v", bad, (*calls)[0])
		}
	}
}

// コンテキストが無ければ --cwd を付けずに開く (フォールバック)。
func TestTuiOverlayNoContextOmitsCwd(t *testing.T) {
	calls := stubHerdrRun(t, []byte("{}"), nil)
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", "")
	if err := cmdTuiOverlay(nil); err != nil {
		t.Fatalf("cmdTuiOverlay: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("herdrRun 呼び出し回数 = %d, want 1", len(*calls))
	}
	args := (*calls)[0]
	if slices.Contains(args, "--cwd") {
		t.Fatalf("コンテキスト無しで --cwd が付いた: %v", args)
	}
	// popup 起動自体は行うこと (entrypoint tui を開く)。
	if !slices.Contains(args, "tui") || !strings.Contains(strings.Join(args, " "), "pane open") {
		t.Fatalf("popup を開く呼び出しになっていない: %v", args)
	}
}
