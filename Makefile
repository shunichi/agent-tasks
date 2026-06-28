BIN := bin/agent-tasks
PREFIX ?= $(HOME)/.local

.PHONY: build install link clean test fmt vet

build: $(BIN)

$(BIN): $(wildcard *.go) go.mod
	go build -o $(BIN) .

# CLI を PATH へ、skill を ~/.claude/skills へ symlink (ビルドも実行)
install: build link

link:
	mkdir -p $(PREFIX)/bin $(HOME)/.claude/skills
	ln -sf  $(CURDIR)/$(BIN)             $(PREFIX)/bin/agent-tasks
	ln -sfn $(CURDIR)/skills/agent-tasks $(HOME)/.claude/skills/agent-tasks

fmt:
	gofmt -w *.go

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -rf bin
