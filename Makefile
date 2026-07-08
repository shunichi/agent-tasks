# ビルドするバイナリ名。既定は本採用名 agent-tasks。名前でパラメータ化してあるので、稼働中の
# 別ビルドと一時的に共存させたいときだけ `make NAME=agent-tasks-xxx ...` で別名ビルドできる
# (herdr 移行中の共存 (0113/0118) の名残。本採用後は既定の agent-tasks で使う)。
NAME ?= agent-tasks
BIN := bin/$(NAME)
PREFIX ?= $(HOME)/.local
# progName をビルド名に合わせて埋め込む。state dir (session.go) と補完のコマンド名/関数名
# (completion.go) がこの値に追従する。既定 NAME=agent-tasks は var の既定値と同じで無害。
LDFLAGS := -X main.progName=$(NAME)

.PHONY: build install link install-completions clean test fmt vet

build: $(BIN)

$(BIN): $(wildcard *.go) $(wildcard templates/*/*) go.mod
	go build -ldflags "$(LDFLAGS)" -o $(BIN) .

# CLI を PATH へ、skill を ~/.claude/skills へ symlink + 補完を再生成 (ビルドも実行)。
# 補完は静的に書き出すファイルなので install に含める。機能追加後に make install を一度打てば
# バイナリ + skill + 補完がすべて最新になる (CLI は symlink で常に最新だが、補完だけ古いまま
# 残るのを防ぐ)。
install: build link install-completions

link:
	mkdir -p $(PREFIX)/bin $(HOME)/.claude/skills
	ln -sf  $(CURDIR)/$(BIN)             $(PREFIX)/bin/$(NAME)
	ln -sfn $(CURDIR)/skills/agent-tasks $(HOME)/.claude/skills/agent-tasks

# bash / zsh 補完スクリプトを標準的な場所へ書き出す (ビルドも実行)。ファイル名も $(NAME) 由来に
# する (別名ビルド時に本体の補完を上書きしない)。zsh は関数名規則に合わせてハイフンを _ に。
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
