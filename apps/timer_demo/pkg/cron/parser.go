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

package cron

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

type CronParser struct {
}

func NewCronParser() *CronParser {
	return &CronParser{}
}

func (c *CronParser) IsValidCronExpr(expr string) bool {
	_, err := parseCronExpr(expr)
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

	schedule, err := parseCronExpr(expr)
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
	schedule, err := parseCronExpr(expr)
	if err != nil {
		return time.Time{}, err
	}

	next := schedule.Next(after)
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("fail to parse time from cron: %s", expr)
	}
	return next, nil
}

type cronSchedule struct {
	minute      []int
	hour        []int
	dayOfMonth  []int
	month       []int
	dayOfWeek   []int
	domWildCard bool
	dowWildCard bool
}

func (s *cronSchedule) Next(t time.Time) time.Time {
	origin := t
	// A 5-field cron only fires on whole minutes. Start from the next minute
	// boundary strictly after t; otherwise a query landing inside a scheduled
	// minute (e.g. 10:05:30 for "*/5 * * * *") matches on every following
	// second and NextsBetween emits one task per second.
	t = t.Truncate(time.Minute).Add(time.Minute)

	for {
		if t.Year() > origin.Year()+5 {
			return time.Time{}
		}

		if !contains(s.month, int(t.Month())) {
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, 1, 0)
			continue
		}

		dayMatch := s.dayMatches(t)
		if !dayMatch {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).AddDate(0, 0, 1)
			continue
		}

		if !contains(s.hour, t.Hour()) {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location()).Add(time.Hour)
			continue
		}

		if !contains(s.minute, t.Minute()) {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, t.Location()).Add(time.Minute)
			continue
		}

		return t
	}
}

func (s *cronSchedule) dayMatches(t time.Time) bool {
	if s.domWildCard && s.dowWildCard {
		return true
	}
	if s.domWildCard {
		return contains(s.dayOfWeek, int(t.Weekday()))
	}
	if s.dowWildCard {
		return contains(s.dayOfMonth, t.Day())
	}
	return contains(s.dayOfMonth, t.Day()) || contains(s.dayOfWeek, int(t.Weekday()))
}

func contains(slice []int, val int) bool {
	return slices.Contains(slice, val)
}

var (
	minuteRange     = []int{0, 59}
	hourRange       = []int{0, 23}
	dayOfMonthRange = []int{1, 31}
	monthRange      = []int{1, 12}
	dayOfWeekRange  = []int{0, 6}
)

func parseCronExpr(expr string) (*cronSchedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("invalid cron expression: expected 5 fields, got %d: %s", len(fields), expr)
	}

	minute, _, err := parseField(fields[0], minuteRange[0], minuteRange[1])
	if err != nil {
		return nil, fmt.Errorf("invalid minute field %q: %w", fields[0], err)
	}
	hour, _, err := parseField(fields[1], hourRange[0], hourRange[1])
	if err != nil {
		return nil, fmt.Errorf("invalid hour field %q: %w", fields[1], err)
	}
	dayOfMonth, domWild, err := parseField(fields[2], dayOfMonthRange[0], dayOfMonthRange[1])
	if err != nil {
		return nil, fmt.Errorf("invalid day-of-month field %q: %w", fields[2], err)
	}
	month, _, err := parseField(fields[3], monthRange[0], monthRange[1])
	if err != nil {
		return nil, fmt.Errorf("invalid month field %q: %w", fields[3], err)
	}
	dayOfWeek, dowWild, err := parseField(fields[4], dayOfWeekRange[0], dayOfWeekRange[1])
	if err != nil {
		return nil, fmt.Errorf("invalid day-of-week field %q: %w", fields[4], err)
	}

	return &cronSchedule{
		minute:      minute,
		hour:        hour,
		dayOfMonth:  dayOfMonth,
		month:       month,
		dayOfWeek:   dayOfWeek,
		domWildCard: domWild,
		dowWildCard: dowWild,
	}, nil
}

func parseField(field string, min, max int) ([]int, bool, error) {
	if field == "*" {
		return rangeList(min, max, 1), true, nil
	}

	wildCard := false
	hasStep := false
	rangePart := field

	if before, _, ok := strings.Cut(field, "/"); ok {
		hasStep = true
		rangePart = before
	}
	if rangePart == "*" {
		wildCard = true
	}

	var result []int
	seen := make(map[int]bool)

	for part := range strings.SplitSeq(field, ",") {
		vals, err := parsePart(part, min, max)
		if err != nil {
			return nil, false, err
		}
		for _, v := range vals {
			if !seen[v] {
				seen[v] = true
				result = append(result, v)
			}
		}
	}

	if len(result) == 0 {
		return nil, false, fmt.Errorf("empty field value")
	}
	_ = hasStep
	return result, wildCard, nil
}

func parsePart(part string, min, max int) ([]int, error) {
	step := 1
	rangePart := part

	if before, after, ok := strings.Cut(part, "/"); ok {
		rangePart = before
		s, err := strconv.Atoi(after)
		if err != nil || s <= 0 {
			return nil, fmt.Errorf("invalid step value: %s", after)
		}
		step = s
	}

	if rangePart == "*" {
		return rangeList(min, max, step), nil
	}

	if before, after, ok := strings.Cut(rangePart, "-"); ok {
		start, err := strconv.Atoi(before)
		if err != nil {
			return nil, fmt.Errorf("invalid range start: %s", before)
		}
		end, err := strconv.Atoi(after)
		if err != nil {
			return nil, fmt.Errorf("invalid range end: %s", after)
		}
		if start < min || end > max || start > end {
			return nil, fmt.Errorf("range %d-%d out of bounds [%d,%d]", start, end, min, max)
		}
		return rangeList(start, end, step), nil
	}

	val, err := strconv.Atoi(rangePart)
	if err != nil {
		return nil, fmt.Errorf("invalid value: %s", rangePart)
	}
	if val < min || val > max {
		return nil, fmt.Errorf("value %d out of bounds [%d,%d]", val, min, max)
	}

	if step > 1 {
		return rangeList(val, max, step), nil
	}
	return []int{val}, nil
}

func rangeList(start, end, step int) []int {
	var result []int
	for i := start; i <= end; i += step {
		result = append(result, i)
	}
	return result
}
