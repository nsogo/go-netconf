package netconf

import (
	"io"
	"log"
)

var debugLog *log.Logger

// SetDebugLogger enables debug logging to the provided writer.
// Call with os.Stderr to print debug output to stderr.
func SetDebugLogger(w io.Writer) {
	debugLog = log.New(w, "[NETCONF DEBUG] ", log.LstdFlags|log.Lmicroseconds)
}

func debugf(format string, args ...interface{}) {
	if debugLog != nil {
		debugLog.Printf(format, args...)
	}
}
