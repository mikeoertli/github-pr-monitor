GO ?= go
PREFIX ?= $(HOME)/.local

.PHONY: build completions test check install install-completions demo
build:
	mkdir -p bin
	$(GO) build -o bin/gprm ./cmd/gprm
	ln -sf gprm bin/ghprm
	ln -sf gprm bin/github-pr-monitor
	$(MAKE) completions

# Also callable directly after a build; never invoke the user's installed copy.
completions:
	mkdir -p bin/completions
	./bin/gprm completion bash > bin/completions/gprm.bash
	./bin/gprm completion zsh > bin/completions/_gprm
	./bin/gprm completion fish > bin/completions/gprm.fish
	./bin/gprm completion powershell > bin/completions/gprm.ps1

test:
	$(GO) test -race ./...

check:
	test -z "$$($(GO) fmt ./...)"
	$(GO) vet ./...
	$(GO) test ./...

demo: build
	./bin/gprm --demo

install: build
	mkdir -p "$(PREFIX)/bin"
	install -m 755 bin/gprm "$(PREFIX)/bin/gprm"
	ln -sf gprm "$(PREFIX)/bin/ghprm"
	ln -sf gprm "$(PREFIX)/bin/github-pr-monitor"

install-completions: build
	mkdir -p "$(PREFIX)/share/bash-completion/completions" "$(PREFIX)/share/zsh/site-functions" "$(PREFIX)/share/fish/vendor_completions.d" "$(PREFIX)/share/gprm/completions"
	install -m 644 bin/completions/gprm.bash "$(PREFIX)/share/bash-completion/completions/gprm"
	ln -sf gprm "$(PREFIX)/share/bash-completion/completions/ghprm"
	ln -sf gprm "$(PREFIX)/share/bash-completion/completions/github-pr-monitor"
	install -m 644 bin/completions/_gprm "$(PREFIX)/share/zsh/site-functions/_gprm"
	install -m 644 bin/completions/gprm.fish "$(PREFIX)/share/fish/vendor_completions.d/gprm.fish"
	ln -sf gprm.fish "$(PREFIX)/share/fish/vendor_completions.d/ghprm.fish"
	ln -sf gprm.fish "$(PREFIX)/share/fish/vendor_completions.d/github-pr-monitor.fish"
	install -m 644 bin/completions/gprm.ps1 "$(PREFIX)/share/gprm/completions/gprm.ps1"
