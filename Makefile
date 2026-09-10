GO ?= go
override TASK_LOOP_TOOLCHAIN := go1.23.12
TASK_LOOP_MODULE := cmd/task_loop
TASK_LOOP_PACKAGE := bin/task-loop
TASK_LOOP_GO := GOTOOLCHAIN=$(TASK_LOOP_TOOLCHAIN) "$(GO)"

.PHONY: build clean install build-task-loop package-task-loop verify-task-loop-package \
	verify-task-loop-reproducible verify-task-loop-tracked \
	smoke-task-loop format-task-loop check-task-loop-format test-task-loop vet-task-loop \
	check-task-loop

build: package-task-loop

clean:
	rm -rf $(TASK_LOOP_MODULE)/build

install: build
	@echo "Plugin ready. Install in Claude Code with:"
	@echo "  claude --plugin-dir $(shell pwd)"
	@echo ""
	@echo "Or add as a marketplace:"
	@echo "  /plugin marketplace add m-ruiz21/skills"

# Kept as a compatibility alias for existing contributor workflows.
build-task-loop: package-task-loop

package-task-loop:
	cd $(TASK_LOOP_MODULE) && $(TASK_LOOP_GO) run ./cmd/package-task-loop \
		-go "$(GO)" -output ../../$(TASK_LOOP_PACKAGE)

verify-task-loop-package:
	cd $(TASK_LOOP_MODULE) && $(TASK_LOOP_GO) run ./cmd/package-task-loop \
		-output ../../$(TASK_LOOP_PACKAGE) -verify

verify-task-loop-reproducible:
	cd $(TASK_LOOP_MODULE) && $(TASK_LOOP_GO) run ./cmd/package-task-loop \
		-go "$(GO)" -output ../../$(TASK_LOOP_PACKAGE) -reproducible

verify-task-loop-tracked:
	cd $(TASK_LOOP_MODULE) && $(TASK_LOOP_GO) run ./cmd/package-task-loop \
		-output ../../$(TASK_LOOP_PACKAGE) -tracked

smoke-task-loop:
	cd $(TASK_LOOP_MODULE) && $(TASK_LOOP_GO) run ./cmd/package-task-loop \
		-output ../../$(TASK_LOOP_PACKAGE) -smoke

format-task-loop:
	cd $(TASK_LOOP_MODULE) && $(TASK_LOOP_GO) fmt ./...

check-task-loop-format:
	@unformatted="$$(find $(TASK_LOOP_MODULE) -name '*.go' -type f -exec gofmt -l {} +)"; \
		test -z "$$unformatted" || \
		(echo "Run 'make format-task-loop' to format these files:"; echo "$$unformatted"; exit 1)

test-task-loop:
	cd $(TASK_LOOP_MODULE) && $(TASK_LOOP_GO) test ./...

vet-task-loop:
	cd $(TASK_LOOP_MODULE) && $(TASK_LOOP_GO) vet ./...

check-task-loop: check-task-loop-format test-task-loop vet-task-loop \
	package-task-loop verify-task-loop-package verify-task-loop-reproducible smoke-task-loop
