package main

import (
	"os"

	"github.com/m-ruiz21/dev-skills/cmd/task_loop/internal/taskloop"
)

func main() {
	os.Exit(taskloop.NewCLI().Run(os.Args[1:], os.Stdout, os.Stderr))
}
