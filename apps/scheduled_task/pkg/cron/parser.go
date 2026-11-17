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

func (c *CronParser) IsValidCronExpr(expr string) bool {
	_, err := robcron.ParseStandard(expr)
	return err == nil
}

func (c *CronParser) NextFromNow(expr string) (time.Time, error) {
	return c.NextAfter(expr, time.Now())
}

func (c *CronParser) NextsBefore(expr string, end time.Time) ([]time.Time, error) {
	return c.NextsBetween(expr, time.Now(), end)
}

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
