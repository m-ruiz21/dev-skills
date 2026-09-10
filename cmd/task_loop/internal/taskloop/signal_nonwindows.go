//go:build !windows

package taskloop

import (
	"os"
	"syscall"
)

func wholeLoopSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
