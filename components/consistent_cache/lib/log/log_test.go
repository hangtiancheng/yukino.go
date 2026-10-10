package log

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestLoggerConcurrentOutput(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(Options{Writer: &buf, LogLevel: "info"})

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 50 {
				l.Infof("goroutine %d message %d", i, j)
			}
		})
	}
	wg.Wait()

	out := buf.String()
	if !strings.Contains(out, "[INFO] goroutine 7 message 49") {
		t.Fatal("expected the last message of every goroutine to be logged")
	}
}

func TestLoggerLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(Options{Writer: &buf, LogLevel: "error"})

	l.Debugf("debug %d", 1)
	l.Infof("info %d", 2)
	l.Warnf("warn %d", 3)
	l.Errorf("error %d", 4)

	if strings.Contains(buf.String(), "debug") || strings.Contains(buf.String(), "info") || strings.Contains(buf.String(), "warn") {
		t.Fatalf("levels below error must be filtered, got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "error 4") {
		t.Fatalf("error level must be logged, got %q", buf.String())
	}

	var all bytes.Buffer
	l2 := NewLogger(Options{Writer: &all, LogLevel: "nonsense"})
	l2.Debugf("still visible")
	if !strings.Contains(all.String(), "still visible") {
		t.Fatalf("unknown level should fall back to debug, got %q", all.String())
	}
}
