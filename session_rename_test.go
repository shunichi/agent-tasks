package main

import (
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

// stubHerdrRunSeq は herdrRun を差し替え、呼び出しごとに errs[i] を返す (足りなければ nil = 成功)。
// フォールバック経路 (1 回目が失敗し 2 回目が別コマンド) を検証するために順序つきで応答する。
func stubHerdrRunSeq(t *testing.T, errs ...error) *[][]string {
	t.Helper()
	orig := herdrRun
	var calls [][]string
	herdrRun = func(args ...string) ([]byte, error) {
		i := len(calls)
		calls = append(calls, args)
		if i < len(errs) {
			return nil, errs[i]
		}
		return nil, nil
	}
	t.Cleanup(func() { herdrRun = orig })
	return &calls
}

func TestHerdrSendPromptUsesAgentPrompt(t *testing.T) {
	calls := stubHerdrRunSeq(t)

	if err := herdrSendPrompt("w3:p1", "/rename task 0001: X"); err != nil {
		t.Fatalf("herdrSendPrompt: %v", err)
	}
	want := [][]string{{"agent", "prompt", "w3:p1", "/rename task 0001: X"}}
	if !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls:\n got %v\nwant %v", *calls, want)
	}
}

func TestHerdrSendPromptFallsBackWhenAgentUndetected(t *testing.T) {
	// agent 検出が効いていない pane では agent prompt が対象を拒む。打ち込み自体は
	// pane 層でできるので pane run に落ちる (0131 までの経路)。
	for _, code := range []string{"agent_not_found", "agent_not_ready"} {
		t.Run(code, func(t *testing.T) {
			calls := stubHerdrRunSeq(t, &herdrCLIError{Sub: "agent prompt", Code: code})

			if err := herdrSendPrompt("w3:p1", "/rename X"); err != nil {
				t.Fatalf("herdrSendPrompt: フォールバックで成功すべき: %v", err)
			}
			want := [][]string{
				{"agent", "prompt", "w3:p1", "/rename X"},
				{"pane", "run", "w3:p1", "/rename X"},
			}
			if !reflect.DeepEqual(*calls, want) {
				t.Errorf("calls:\n got %v\nwant %v", *calls, want)
			}
		})
	}
}

func TestHerdrSendPromptDoesNotFallBackOnSubmitFailure(t *testing.T) {
	// 送出途中の失敗で pane run に落とすと、既に入っていた 1 行に重ねて二重送信になりうる。
	// そのためフォールバックせずエラーを返す。
	sent := &herdrCLIError{Sub: "agent prompt", Code: "agent_prompt_failed"}
	calls := stubHerdrRunSeq(t, sent)

	err := herdrSendPrompt("w3:p1", "/rename X")
	if !errors.Is(err, error(sent)) {
		t.Errorf("err: got %v want %v", err, sent)
	}
	if len(*calls) != 1 {
		t.Errorf("二重送信になりうるので 1 回で止めるべき: %v", *calls)
	}
}

func TestHerdrSendPromptPropagatesNonCLIError(t *testing.T) {
	// herdr 自体が無い等 (JSON エラーですらない) はフォールバックしても無意味なので伝播する。
	calls := stubHerdrRunSeq(t, exec.ErrNotFound)

	if err := herdrSendPrompt("w3:p1", "/rename X"); err == nil {
		t.Error("エラーを伝播すべき")
	}
	if len(*calls) != 1 {
		t.Errorf("フォールバックしないので 1 回: %v", *calls)
	}
}

func TestSessionRenameName(t *testing.T) {
	got := sessionRenameName(Task{ID: "0042", Title: "タスク名"})
	if want := "task 0042: タスク名"; got != want {
		t.Errorf("sessionRenameName: got %q want %q", got, want)
	}
}
