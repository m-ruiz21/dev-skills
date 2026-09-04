.PHONY: build clean install build-task-loop test-task-loop

build: build-task-loop

clean:
	rm -rf bin/

install: build
	@echo "Plugin ready. Install in Claude Code with:"
	@echo "  claude --plugin-dir $(shell pwd)"
	@echo ""
	@echo "Or add as a marketplace:"
	@echo "  /plugin marketplace add m-ruiz21/skills"

# Installs the task-loop CLI into bin/ (added to PATH when the plugin is
# active) by symlinking the source-runner script -- no network access or
# package build step required.
build-task-loop:
	@mkdir -p bin
	@ln -sf ../cmd/task_loop/scripts/task-loop bin/task-loop
	@chmod +x cmd/task_loop/scripts/task-loop

test-task-loop:
	cd cmd/task_loop && PYTHONPATH=src python3 -m unittest discover -s tests -t . -v
