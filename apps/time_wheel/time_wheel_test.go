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

package time_wheel

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	time_wheel_http "github.com/hangtiancheng/yukino.go/apps/time_wheel/pkg/http"
	"github.com/hangtiancheng/yukino.go/apps/time_wheel/pkg/redis"
)

func Test_timeWheel(t *testing.T) {
	timeWheel := NewTimeWheel(10, 100*time.Millisecond)
	defer timeWheel.Stop()

	fired := make(chan string, 4)
	timeWheel.AddTask("test1", func() { fired <- "test1" }, time.Now().Add(300*time.Millisecond))
	// Re-adding "test2" must replace the pending 2s task with the 500ms one.
	timeWheel.AddTask("test2", func() { fired <- "test2" }, time.Now().Add(2*time.Second))
	timeWheel.AddTask("test2", func() { fired <- "test2" }, time.Now().Add(500*time.Millisecond))

	deadline := time.After(3 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case key := <-fired:
			want := "test1"
			if i == 1 {
				want = "test2"
			}
			if key != want {
				t.Fatalf("fired task %d = %s, want %s", i+1, key, want)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for firing %d", i+1)
		}
	}

	// The superseded 2s "test2" task must never fire.
	select {
	case key := <-fired:
		t.Fatalf("replaced task fired unexpectedly: %s", key)
	case <-time.After(2 * time.Second):
	}
}

func Test_TimeWheel_RemoveTask(t *testing.T) {
	timeWheel := NewTimeWheel(10, 50*time.Millisecond)
	defer timeWheel.Stop()

	fired := make(chan string, 2)
	timeWheel.AddTask("doomed", func() { fired <- "doomed" }, time.Now().Add(200*time.Millisecond))
	timeWheel.RemoveTask("doomed")
	// Removing a key that was never added must be a no-op.
	timeWheel.RemoveTask("never-added")

	select {
	case key := <-fired:
		t.Fatalf("task %s fired after removal", key)
	case <-time.After(1 * time.Second):
	}
}

func Test_TimeWheel_PastExecuteAt(t *testing.T) {
	timeWheel := NewTimeWheel(10, 50*time.Millisecond)
	defer timeWheel.Stop()

	fired := make(chan string, 2)
	// A past-due deadline must not panic the wheel; the task fires on the
	// next pass over its slot.
	timeWheel.AddTask("past", func() { fired <- "past" }, time.Now().Add(-time.Hour))

	select {
	case key := <-fired:
		if key != "past" {
			t.Fatalf("fired task = %s, want past", key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("past-due task did not fire; the wheel likely panicked")
	}

	// The wheel must still be alive and scheduling afterwards.
	timeWheel.AddTask("after", func() { fired <- "after" }, time.Now().Add(100*time.Millisecond))
	select {
	case key := <-fired:
		if key != "after" {
			t.Fatalf("fired task = %s, want after", key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wheel stopped working after a past-due task")
	}
}

func Test_TimeWheel_ConcurrentAddRemove(t *testing.T) {
	timeWheel := NewTimeWheel(10, 10*time.Millisecond)
	defer timeWheel.Stop()

	const workers = 8
	const ops = 200
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				key := fmt.Sprintf("k%d", (w*7+i)%16)
				if i%3 == 2 {
					timeWheel.RemoveTask(key)
					continue
				}
				// Some deadlines fall in the past once the send is processed.
				timeWheel.AddTask(key, func() {}, time.Now().Add(time.Duration(i%7)*10*time.Millisecond))
			}
		}(w)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent AddTask/RemoveTask deadlocked")
	}
}

func Test_TimeWheel_StopThenAddDoesNotBlock(t *testing.T) {
	timeWheel := NewTimeWheel(10, 10*time.Millisecond)
	timeWheel.Stop()
	timeWheel.Stop() // double Stop must be a no-op

	done := make(chan struct{})
	go func() {
		defer close(done)
		// After Stop these must drop the task instead of blocking forever.
		timeWheel.AddTask("x", func() {}, time.Now().Add(time.Second))
		timeWheel.RemoveTask("x")
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("AddTask/RemoveTask blocked after Stop")
	}
}

func Test_TimeWheel_StopReleasesGoroutines(t *testing.T) {
	time.Sleep(200 * time.Millisecond) // let goroutines from earlier tests settle
	before := runtime.NumGoroutine()

	for i := 0; i < 5; i++ {
		timeWheel := NewTimeWheel(10, 10*time.Millisecond)
		timeWheel.AddTask(fmt.Sprintf("k%d", i), func() {}, time.Now().Add(time.Hour))
		timeWheel.Stop()
	}

	// Every driver goroutine must exit after Stop.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("goroutines leaked after Stop: before=%d, after=%d", before, runtime.NumGoroutine())
}

const (
	// redis server info
	network  = "tcp"
	address  = "please fill in redis address"
	password = "please fill in redis password"
)

var (
	// scheduled task callback info
	callbackURL    = "please fill in callback url"
	callbackMethod = "POST"
	callbackReq    any
	callbackHeader map[string]string
)

func Test_redis_timeWheel(t *testing.T) {
	if address == "please fill in redis address" || callbackURL == "please fill in callback url" {
		t.Skip("fill in the redis/callback constants at the top of this file to run this integration test")
	}

	rTimeWheel := NewRTimeWheel(
		redis.NewClient(network, address, password),
		time_wheel_http.NewClient(),
	)
	defer rTimeWheel.Stop()

	ctx := context.Background()
	if err := rTimeWheel.AddTask(ctx, "test1", &RTaskElement{
		CallbackURL: callbackURL,
		Method:      callbackMethod,
		Req:         callbackReq,
		Header:      callbackHeader,
	}, time.Now().Add(time.Second)); err != nil {
		t.Error(err)
		return
	}

	if err := rTimeWheel.AddTask(ctx, "test2", &RTaskElement{
		CallbackURL: callbackURL,
		Method:      callbackMethod,
		Req:         callbackReq,
		Header:      callbackHeader,
	}, time.Now().Add(4*time.Second)); err != nil {
		t.Error(err)
		return
	}

	if err := rTimeWheel.RemoveTask(ctx, "test2", time.Now().Add(4*time.Second)); err != nil {
		t.Error(err)
		return
	}

	<-time.After(5 * time.Second)
	t.Log("ok")
}
