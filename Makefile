.PHONY: build clean install

build:
	go build -o bin/ralph ./cmd/ralph/

clean:
	rm -f bin/ralph

install: build
	@echo "Plugin ready. Install in Claude Code with:"
	@echo "  claude --plugin-dir $(shell pwd)"
	@echo ""
	@echo "Or add as a marketplace:"
	@echo "  /plugin marketplace add m-ruiz21/skills"
