package example

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/components/tcc"
	"github.com/hangtiancheng/yukino.go/components/tcc/example/dao"
	"github.com/hangtiancheng/yukino.go/components/tcc/example/pkg"
)

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

	componentA := NewMockComponent(componentAID, redisClient)
	componentB := NewMockComponent(componentBID, redisClient)
	componentC := NewMockComponent(componentCID, redisClient)

	txRecordDAO := dao.NewTXRecordDAO(mysqlDB)
	txStore := NewMockTXStore(txRecordDAO, redisClient)

	txManager := tcc.NewTXManager(txStore, tcc.WithMonitorTick(time.Second))
	defer txManager.Stop()

	for _, component := range []tcc.TCCComponent{componentA, componentB, componentC} {
		if err := txManager.Register(component); err != nil {
			t.Fatalf("register component %s: %v", component.ID(), err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	_, success, err := txManager.Transaction(ctx, []*tcc.RequestEntity{
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

	<-time.After(2 * time.Second)
}
