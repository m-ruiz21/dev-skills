//go:build windows

package taskloop

import "os"

func wholeLoopSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
