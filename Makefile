.PHONY: build clean install

build:
	dotnet publish cmd/ralph/Ralph.csproj -c Release -o bin/ --self-contained false

clean:
	rm -rf bin/
	dotnet clean cmd/ralph/Ralph.csproj

install: build
	@echo "Plugin ready. Install in Claude Code with:"
	@echo "  claude --plugin-dir $(shell pwd)"
	@echo ""
	@echo "Or add as a marketplace:"
	@echo "  /plugin marketplace add m-ruiz21/skills"
