// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package log

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

// TestLoggerConcurrentOutput checks that the logger is safe for concurrent use
// (run with -race) and that concurrent messages are not lost.
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

// TestLoggerLevelFiltering checks the leveled filtering and unknown-level fallback.
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

	// Unknown level names fall back to DebugLevel (zero value): everything logs.
	var all bytes.Buffer
	l2 := NewLogger(Options{Writer: &all, LogLevel: "nonsense"})
	l2.Debugf("still visible")
	if !strings.Contains(all.String(), "still visible") {
		t.Fatalf("unknown level should fall back to debug, got %q", all.String())
	}
}
