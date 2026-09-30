package os

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// GetCurrentProcessAndGoroutineIDStr returns a "pid_goroutineID" identifier for the current goroutine.
func GetCurrentProcessAndGoroutineIDStr() string {
	pid := GetCurrentProcessID()
	goroutineID := GetCurrentGoroutineID()
	return fmt.Sprintf("%d_%s", pid, goroutineID)
}

// GetCurrentGoroutineID returns the current goroutine ID extracted from the runtime stack.
func GetCurrentGoroutineID() string {
	buf := make([]byte, 128)
	buf = buf[:runtime.Stack(buf, false)]
	stackInfo := string(buf)
	return strings.TrimSpace(strings.Split(strings.Split(stackInfo, "[running]")[0], "goroutine")[1])
}

// GetCurrentProcessID returns the current OS process ID.
func GetCurrentProcessID() int {
	return os.Getpid()
}
