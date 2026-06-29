BIN := bin/agent-tasks
PREFIX ?= $(HOME)/.local

.PHONY: build install link install-completions clean test fmt vet

build: $(BIN)

$(BIN): $(wildcard *.go) $(wildcard templates/*/*) go.mod
	go build -o $(BIN) .

# CLI を PATH へ、skill を ~/.claude/skills へ symlink (ビルドも実行)
install: build link

link:
	mkdir -p $(PREFIX)/bin $(HOME)/.claude/skills
	ln -sf  $(CURDIR)/$(BIN)             $(PREFIX)/bin/agent-tasks
	ln -sfn $(CURDIR)/skills/agent-tasks $(HOME)/.claude/skills/agent-tasks

# bash / zsh 補完スクリプトを標準的な場所へ書き出す (ビルドも実行)
install-completions: build
	mkdir -p $(PREFIX)/share/bash-completion/completions $(PREFIX)/share/zsh/site-functions
	$(BIN) completion bash > $(PREFIX)/share/bash-completion/completions/agent-tasks
	$(BIN) completion zsh  > $(PREFIX)/share/zsh/site-functions/_agent_tasks

fmt:
	gofmt -w *.go

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -rf bin
