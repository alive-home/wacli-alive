.DEFAULT_GOAL := help

.PHONY: help build test fmt lint check

help:
	@printf '%s\n' \
		'Available targets:' \
		'  help            Print available targets (default).' \
		'  build           Build the CLI into dist/wacli.' \
		'  test            Run the full Go, FTS, cross-build, and docs test suite.' \
		'  fmt             Check Go formatting.' \
		'  lint            Run vet and dead-code checks.' \
		'  check           Run every local gate.'

build:
	GOWORK=off bun run --silent build

test:
	GOWORK=off bun run --silent test

fmt:
	GOWORK=off bun run --silent format:check

lint:
	GOWORK=off bun run --silent lint
	@test "$$(GOWORK=off go env GOVERSION)" = go1.27.3
	GOWORK=off bun run --silent lint:deadcode

check: fmt lint test build
	GOWORK=off bun run --silent docs:site
	git diff --check
