package runtime

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

func GetCurrentProcessAndGoroutineIDStr() string {
	pid := GetCurrentProcessID()
	goroutineID := GetCurrentGoroutineID()
	return fmt.Sprintf("%d_%s", pid, goroutineID)
}

func GetCurrentGoroutineID() string {
	buf := make([]byte, 128)
	buf = buf[:runtime.Stack(buf, false)]
	stackInfo := string(buf)
	parts := strings.Split(strings.Split(stackInfo, "[running]")[0], "goroutine")
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func GetCurrentProcessID() int {
	return os.Getpid()
}
