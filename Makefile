# ビルドするバイナリ名。稼働中の別ビルドと共存させるため名前でパラメータ化する。
# このブランチ (herdr 対応版) は既定を agent-tasks-herdr にして、本体版 (agent-tasks) の
# symlink / skill / 補完 / state dir を一切上書きしないようにする (0105 の「移行の制約」参照)。
# 本体版と同じ名前で入れたいときは `make NAME=agent-tasks ...`。
NAME ?= agent-tasks-herdr
BIN := bin/$(NAME)
PREFIX ?= $(HOME)/.local
# progName をビルド名に合わせて埋め込む。state dir の分離 (session.go) と補完の
# コマンド名/関数名 (completion.go) がこの値に追従する。NAME=agent-tasks なら既定値と同じで無害。
LDFLAGS := -X main.progName=$(NAME)

.PHONY: build install link install-completions clean test fmt vet

build: $(BIN)

$(BIN): $(wildcard *.go) $(wildcard templates/*/*) go.mod
	go build -ldflags "$(LDFLAGS)" -o $(BIN) .

# CLI を PATH へ、skill を ~/.claude/skills へ symlink + 補完を再生成 (ビルドも実行)。
# すべて $(NAME) 名で入れるので、別名ビルドは本体版の symlink/skill/補完を上書きしない。
# 補完は静的に書き出すファイルなので install に含める。機能追加後に make install を一度打てば
# バイナリ + skill + 補完がすべて最新になる (CLI は symlink で常に最新だが、補完だけ古いまま
# 残るのを防ぐ)。
install: build link install-completions

link:
	mkdir -p $(PREFIX)/bin $(HOME)/.claude/skills
	ln -sf  $(CURDIR)/$(BIN)             $(PREFIX)/bin/$(NAME)
	ln -sfn $(CURDIR)/skills/agent-tasks $(HOME)/.claude/skills/$(NAME)

# bash / zsh 補完スクリプトを標準的な場所へ書き出す (ビルドも実行)。ファイル名も $(NAME) 由来に
# するので本体版の補完 (agent-tasks / _agent_tasks) を上書きしない。zsh は関数名規則に合わせて
# ハイフンを _ に (agent-tasks-herdr -> _agent_tasks_herdr)。
install-completions: build
	mkdir -p $(PREFIX)/share/bash-completion/completions $(PREFIX)/share/zsh/site-functions
	$(BIN) completion bash > $(PREFIX)/share/bash-completion/completions/$(NAME)
	$(BIN) completion zsh  > $(PREFIX)/share/zsh/site-functions/_$(subst -,_,$(NAME))

fmt:
	gofmt -w *.go

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -rf bin
