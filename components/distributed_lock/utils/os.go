package utils

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

func GetCurrentProcessID() string {
	return strconv.Itoa(os.Getpid())
}

func GetCurrentGoroutineID() int64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	field := strings.Fields(string(buf[:n]))
	id, _ := strconv.ParseInt(field[1], 10, 64)
	return id
}

func GetProcessAndGoroutineIDStr() string {
	return fmt.Sprintf("%s_%d", GetCurrentProcessID(), GetCurrentGoroutineID())
}
