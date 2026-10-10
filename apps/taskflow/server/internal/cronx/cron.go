package cronx

import (
	"fmt"
	"time"

	timercron "github.com/hangtiancheng/yukino.go/components/timer/pkg/cron"
)

type Schedule struct {
	expr   string
	parser *timercron.CronParser
}

func Parse(expr string) (*Schedule, error) {
	parser := timercron.NewCronParser()
	if !parser.IsValidCronExpr(expr) {
		if _, err := parser.NextAfter(expr, time.Now()); err != nil {
			return nil, fmt.Errorf("cron expr %q: %w", expr, err)
		}
		return nil, fmt.Errorf("cron expr %q is invalid", expr)
	}
	return &Schedule{expr: expr, parser: parser}, nil
}

func (s *Schedule) Expr() string { return s.expr }

func (s *Schedule) Next(after time.Time) time.Time {
	next, err := s.parser.NextAfter(s.expr, after)
	if err != nil {
		return time.Time{}
	}
	return next
}

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
