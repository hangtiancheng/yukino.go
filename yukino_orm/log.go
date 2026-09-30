package yukino_orm

import (
	"io"
	"log"
	"os"
	"sync"
)

var (
	errorLogger = log.New(os.Stderr, "\033[31m[error]\033[0m ", log.LstdFlags|log.Lshortfile)
	infoLogger  = log.New(os.Stdout, "\033[34m[info ]\033[0m ", log.LstdFlags|log.Lshortfile)
	loggers     = []*log.Logger{errorLogger, infoLogger}
	mu          sync.Mutex
)

// log methods
var (
	Error  = errorLogger.Println
	Errorf = errorLogger.Printf
	Info   = infoLogger.Println
	Infof  = infoLogger.Printf
)

// log levels
const (
	InfoLevel = iota
	ErrorLevel
	Disabled
)

// SetLevel controls log level
func SetLevel(level int) {
	mu.Lock()
	defer mu.Unlock()

	for _, logger := range loggers {
		logger.SetOutput(os.Stdout)
	}
	errorLogger.SetOutput(os.Stderr)

	if ErrorLevel < level {
		errorLogger.SetOutput(io.Discard)
	}
	if InfoLevel < level {
		infoLogger.SetOutput(io.Discard)
	}
}
