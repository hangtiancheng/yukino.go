// Package cronx adapts timer's cron parser (components//timer,
// which wraps the professional robfig/cron v3 library) to the fire-time API
// used by the taskflow engine: a standard 5-field expression (minute, hour,
// day-of-month, month, day-of-week) with "*", lists, ranges, steps and the
// @daily-style descriptors; day-of-week accepts 0-6 with Sunday as 0.
package cronx

import (
	"fmt"
	"time"

	timercron "github.com/hangtiancheng/yukino.go/components/timer/pkg/cron"
)

// Schedule is a validated cron expression. It is safe for concurrent use: the
// underlying timer parser is stateless.
type Schedule struct {
	expr   string
	parser *timercron.CronParser
}

// Parse validates a 5-field cron expression and returns a reusable schedule.
func Parse(expr string) (*Schedule, error) {
	parser := timercron.NewCronParser()
	if !parser.IsValidCronExpr(expr) {
		// Re-run through NextAfter to surface the detailed parse error.
		if _, err := parser.NextAfter(expr, time.Now()); err != nil {
			return nil, fmt.Errorf("cron expr %q: %w", expr, err)
		}
		return nil, fmt.Errorf("cron expr %q is invalid", expr)
	}
	return &Schedule{expr: expr, parser: parser}, nil
}

// Expr returns the original expression text.
func (s *Schedule) Expr() string { return s.expr }

// Next returns the earliest fire time strictly after `after`, searched in
// after's time zone. The zero time is returned when nothing matches within
// the parser's five-year horizon.
func (s *Schedule) Next(after time.Time) time.Time {
	next, err := s.parser.NextAfter(s.expr, after)
	if err != nil {
		return time.Time{}
	}
	return next
}

// NextN returns up to n fire times strictly after `after`.
func (s *Schedule) NextN(after time.Time, n int) []time.Time {
	if n <= 0 {
		return nil
	}
	fires := make([]time.Time, 0, n)
	cursor := after
	for len(fires) < n {
		next := s.Next(cursor)
		if next.IsZero() {
			break
		}
		fires = append(fires, next)
		cursor = next
	}
	return fires
}
