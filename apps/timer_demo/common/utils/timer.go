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

package utils

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/consts"
)

func UnionTimerIDUnix(timeID uint, unix int64) string {
	return fmt.Sprintf("%d_%d", timeID, unix)
}

func SplitTimerIDUnix(str string) (uint, int64, error) {
	timerIDUnix := strings.Split(str, "_")
	if len(timerIDUnix) != 2 {
		return 0, 0, fmt.Errorf("invalid timerID unix str: %s", str)
	}

	timerID, err := strconv.ParseUint(timerIDUnix[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid timerID in str: %s, err: %w", str, err)
	}

	unix, err := strconv.ParseInt(timerIDUnix[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid unix in str: %s, err: %w", str, err)
	}
	return uint(timerID), unix, nil
}

func GetTaskBloomFilterKey(timeStr string) string {
	return "task_bloom_" + timeStr
}

func GetBucketCntKey(key string) string {
	return "bucket_cnt_" + key
}

func GetTimeBucketLockKey(t time.Time, bucketID int) string {
	return fmt.Sprintf("time_bucket_lock_%s_%d", t.Format(consts.MinuteFormat), bucketID)
}

func GetMigratorLockKey(t time.Time) string {
	return fmt.Sprintf("migrator_lock_%s", t.Format(consts.HourFormat))
}

func GetMonitorLockKey(t time.Time) string {
	return fmt.Sprintf("monitor_lock_%s", t.Format(consts.MinuteFormat))
}

func GetSliceMsgKey(t time.Time, bucketID int) string {
	return fmt.Sprintf("%s_%d", t.Format(consts.MinuteFormat), bucketID)
}

func GetEnableLockKey(app string) string {
	return fmt.Sprintf("enable_timer_lock_%s", app)
}

func GetCreateLockKey(app string) string {
	return fmt.Sprintf("create_timer_lock_%s", app)
}

func SplitTimeBucket(key string) (time.Time, int, error) {
	timerBucket := strings.Split(key, "_")
	if len(timerBucket) != 2 {
		return time.Time{}, 0, fmt.Errorf("invalid time bucket key: %s", key)
	}

	t, err := time.ParseInLocation(consts.MinuteFormat, timerBucket[0], time.Local)
	if err != nil {
		return t, 0, err
	}

	bucket, err := strconv.Atoi(timerBucket[1])
	return t, bucket, err
}

func GetForwardTwoMigrateStepEnd(cur time.Time, diff time.Duration) time.Time {
	end := cur.Add(diff)
	return time.Date(end.Year(), end.Month(), end.Day(), end.Hour(), 0, 0, 0, time.Local)
}
