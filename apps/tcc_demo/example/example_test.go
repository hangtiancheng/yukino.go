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

package example

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/tcc_demo"
	"github.com/hangtiancheng/yukino.go/apps/tcc_demo/example/dao"
	"github.com/hangtiancheng/yukino.go/apps/tcc_demo/example/pkg"
)

// TestTCCExample runs the full TCC transaction flow against real MySQL and
// Redis. It is skipped unless the following environment variables are set:
//
//	TCC_TEST_MYSQL_DSN:      MySQL DSN
//	TCC_TEST_REDIS_ADDR:     Redis address, e.g. "127.0.0.1:6379"
//	TCC_TEST_REDIS_PASSWORD: Redis password (optional)
func TestTCCExample(t *testing.T) {
	dsn := os.Getenv("TCC_TEST_MYSQL_DSN")
	redisAddr := os.Getenv("TCC_TEST_REDIS_ADDR")
	if dsn == "" || redisAddr == "" {
		t.Skip("TCC_TEST_MYSQL_DSN and TCC_TEST_REDIS_ADDR are required to run this test")
	}

	redisClient := pkg.NewRedisClient("tcp", redisAddr, os.Getenv("TCC_TEST_REDIS_PASSWORD"))
	mysqlDB, err := pkg.NewDB(dsn)
	if err != nil {
		t.Fatalf("connect mysql: %v", err)
	}

	componentAID := "componentA"
	componentBID := "componentB"
	componentCID := "componentC"

	// Construct TCC components
	componentA := NewMockComponent(componentAID, redisClient)
	componentB := NewMockComponent(componentBID, redisClient)
	componentC := NewMockComponent(componentCID, redisClient)

	// Construct the transaction log storage module
	txRecordDAO := dao.NewTXRecordDAO(mysqlDB)
	txStore := NewMockTXStore(txRecordDAO, redisClient)

	txManager := tcc_demo.NewTXManager(txStore, tcc_demo.WithMonitorTick(time.Second))
	defer txManager.Stop()

	// Register all components
	for _, component := range []tcc_demo.TCCComponent{componentA, componentB, componentC} {
		if err := txManager.Register(component); err != nil {
			t.Fatalf("register component %s: %v", component.ID(), err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	_, success, err := txManager.Transaction(ctx, []*tcc_demo.RequestEntity{
		{ComponentID: componentAID,
			Request: map[string]any{
				"biz_id": componentAID + "_biz",
			},
		},
		{ComponentID: componentBID,
			Request: map[string]any{
				"biz_id": componentBID + "_biz",
			},
		},
		{ComponentID: componentCID,
			Request: map[string]any{
				"biz_id": componentCID + "_biz",
			},
		},
	}...)
	if err != nil {
		t.Fatalf("tx failed: %v", err)
	}
	if !success {
		t.Fatal("tx failed")
	}

	// Leave the monitor one tick to observe the completed transaction.
	<-time.After(2 * time.Second)
}
