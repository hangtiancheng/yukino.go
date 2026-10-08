// Package cron wraps the professional cron library robfig/cron (v3) with the
// API surface the timer service needs: validation, next-fire computation and
// window expansion. The standard 5-field cron dialect and the @daily-style
// descriptors are supported; day-of-week accepts 0-6 with Sunday as 0.
package cron

import (
	"fmt"
	"time"

	robcron "github.com/robfig/cron/v3"
)

type CronParser struct{}

func NewCronParser() *CronParser {
	return &CronParser{}
}

// IsValidCronExpr reports whether expr parses as a standard cron schedule.
func (c *CronParser) IsValidCronExpr(expr string) bool {
	_, err := robcron.ParseStandard(expr)
	return err == nil
}

// NextFromNow returns the first fire time strictly after now.
func (c *CronParser) NextFromNow(expr string) (time.Time, error) {
	return c.NextAfter(expr, time.Now())
}

// NextsBefore returns every fire time in [now, end).
func (c *CronParser) NextsBefore(expr string, end time.Time) ([]time.Time, error) {
	return c.NextsBetween(expr, time.Now(), end)
}

// NextsBetween returns every fire time in [start, end).
func (c *CronParser) NextsBetween(expr string, start, end time.Time) ([]time.Time, error) {
	if end.Before(start) {
		return nil, fmt.Errorf("end can not earlier than start, start: %v, end: %v", start, end)
	}

	schedule, err := robcron.ParseStandard(expr)
	if err != nil {
		return nil, err
	}

	var nexts []time.Time
	cur := start
	for cur.Before(end) {
		next := schedule.Next(cur)
		if next.IsZero() {
			return nil, fmt.Errorf("fail to parse time from cron: %s", expr)
		}
		if !next.Before(end) {
			break
		}
		nexts = append(nexts, next)
		cur = next
	}
	return nexts, nil
}

// NextAfter returns the earliest fire time strictly after `after`. The zero
// time plus an error is returned when nothing matches within the library's
// search horizon (five years).
func (c *CronParser) NextAfter(expr string, after time.Time) (time.Time, error) {
	schedule, err := robcron.ParseStandard(expr)
	if err != nil {
		return time.Time{}, err
	}

	next := schedule.Next(after)
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("fail to parse time from cron: %s", expr)
	}
	return next, nil
}
