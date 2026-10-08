//go:build integration

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/cdc"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/conf"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/dao"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/idem"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/storage"
	"github.com/hangtiancheng/yukino.go/components/red_mq"
	mqredis "github.com/hangtiancheng/yukino.go/components/red_mq/redis"
	"github.com/hangtiancheng/yukino.go/components/redis_lock"
	"github.com/hangtiancheng/yukino.go/components/tcc"
	timerbloom "github.com/hangtiancheng/yukino.go/components/timer/pkg/bloom"
	timerhash "github.com/hangtiancheng/yukino.go/components/timer/pkg/hash"
	timerredis "github.com/hangtiancheng/yukino.go/components/timer/pkg/redis"
	"github.com/redis/go-redis/v9"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func integrationDB(t *testing.T) (*gorm.DB, *redis.Client, *conf.Config) {
	t.Helper()
	if os.Getenv("TASKFLOW_INTEGRATION") != "1" {
		t.Skip("set TASKFLOW_INTEGRATION=1 and MYSQL_PASSWORD")
	}
	path := t.TempDir() + "/conf.yml"
	if err := os.WriteFile(path, []byte("mysql:\n  password_env: MYSQL_PASSWORD\nllm:\n  default_model: test-model\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := conf.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("taskflow_it_%d", time.Now().UnixNano())
	admin, err := gorm.Open(gormmysql.Open(cfg.MySQL.DSN("")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.Exec("CREATE DATABASE `" + name + "`").Error; err != nil {
		t.Fatal(err)
	}
	cfg.MySQL.Database = name
	db, err := gorm.Open(gormmysql.Open(cfg.MySQL.DSN(name)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool, _ := db.DB()
		_ = pool.Close()
		admin.Exec("DROP DATABASE `" + name + "`")
		pool, _ = admin.DB()
		_ = pool.Close()
	})
	if err := po.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(&redis.Options{Addr: cfg.Redis.Address, DB: 15})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var cursor uint64
		for {
			keys, next, _ := client.Scan(context.Background(), cursor, name+"*", 100).Result()
			if len(keys) > 0 {
				client.Del(context.Background(), keys...)
			}
			cursor = next
			if next == 0 {
				break
			}
		}
		_ = client.Close()
	})
	cfg.MQ.ExecTopic = name + ":exec"
	cfg.MQ.CondTopic = name + ":cond"
	cfg.MQ.ExecGroup = name
	cfg.MQ.CondGroup = name
	cfg.Node.ID = name
	return db, client, cfg
}

func TestTransactionalCaptureAndConcurrentRelay(t *testing.T) {
	db, client, cfg := integrationDB(t)
	ctx := context.Background()
	if err := cdc.EnsureCapture(ctx, db, "risk_records"); err != nil {
		t.Fatal(err)
	}
	task := &po.ConditionTask{Name: "Security audit", WatchTable: "risk_records", EventType: po.EventTypeMySQLInsert, Enabled: true, Prompt: "Inspect the captured record."}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if err := tx.Exec("INSERT INTO risk_records (title,content,source,created_at) VALUES ('rollback','<script>alert(1)</script>','direct',UTC_TIMESTAMP())").Error; err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	var count int64
	db.Model(&po.ChangeEvent{}).Count(&count)
	if count != 0 {
		t.Fatalf("rollback produced %d audit events", count)
	}
	for i := 0; i < 20; i++ {
		if err := db.Exec("INSERT INTO risk_records (title,content,source,created_at) VALUES (?, ?, 'direct', UTC_TIMESTAMP())", fmt.Sprintf("Record %d", i), "<img src=x onerror=alert(1)>").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("DELETE FROM risk_records WHERE id = (SELECT id FROM (SELECT MIN(id) AS id FROM risk_records) AS r)").Error; err != nil {
		t.Fatal(err)
	}
	producer := red_mq.NewProducer(mqredis.NewUniversalClient(client), red_mq.WithMsgQueueLen(1000))
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := cdc.NewRelay(db, producer, cfg.MQ.CondTopic).Drain(ctx, 200); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	db.Model(&po.Execution{}).Count(&count)
	if count != 20 {
		t.Fatalf("condition fanout created %d executions; want 20", count)
	}
	db.Model(&po.ChangeEvent{}).Where("operation = ?", "delete").Count(&count)
	if count != 1 {
		t.Fatalf("deleted rows audit count=%d", count)
	}
	db.Model(&po.Outbox{}).Where("published_at IS NULL").Count(&count)
	if count != 0 {
		t.Fatalf("pending outbox=%d", count)
	}
	if n, err := client.XLen(ctx, cfg.MQ.CondTopic).Result(); err != nil || n != 20 {
		t.Fatalf("stream length=%d err=%v", n, err)
	}
	var exec po.Execution
	db.First(&exec)
	if !strings.Contains(exec.TriggerInfo, "onerror") || exec.PromptSnapshot != task.Prompt {
		t.Fatalf("captured record or prompt missing: %+v", exec)
	}
	// A duplicate fire key always resolves the canonical MySQL row ID.
	for i := 0; i < 10; i++ {
		candidate := &po.Execution{FireKey: exec.FireKey, Status: po.StatusPending, FireAt: time.Now(), TaskType: po.TaskTypeCondition}
		_, id, err := dao.New(db).InsertPending(ctx, candidate)
		if err != nil || id != exec.ID {
			t.Fatalf("duplicate resolved ID %d instead of %d: %v", id, exec.ID, err)
		}
	}
	// Database-wide capture also handles spatial columns and tables without a
	// primary key. UUID identity must not depend on an auto-increment ID.
	if err := db.Exec("CREATE TABLE audit_notes (content TEXT, location POINT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := cdc.EnsureDatabaseCapture(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO audit_notes VALUES ('captured', ST_GeomFromText('POINT(1 2)'))").Error; err != nil {
		t.Fatal(err)
	}
	var spatial po.ChangeEvent
	if err := db.Where("table_name = ?", "audit_notes").First(&spatial).Error; err != nil || !strings.Contains(spatial.RecordJSON, "captured") {
		t.Fatalf("capture without primary key failed: %+v %v", spatial, err)
	}
}

func TestDuplicateCommandsRunToolsOnceAndPersistMarkdown(t *testing.T) {
	db, client, cfg := integrationDB(t)
	ctx := context.Background()
	d := dao.New(db)
	var completions atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		completions.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		messages := body["messages"].([]any)
		last := messages[len(messages)-1].(map[string]any)
		if last["role"] == "tool" {
			fmt.Fprint(w, `{"id":"chat-test","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"## Summary\nDatabase inspected.\n## Detailed Analysis\nCounts came from mysql_tool.\n## Conclusion & Recommendations\nContinue monitoring."}}],"usage":{"prompt_tokens":10,"completion_tokens":20}}`)
		} else {
			fmt.Fprint(w, `{"id":"chat-test","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"mysql_tool","arguments":"{\"sql\":\"SELECT COUNT(*) AS table_count FROM information_schema.tables WHERE table_schema = DATABASE()\"}"}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":1}}`)
		}
	}))
	defer provider.Close()
	t.Setenv("TASKFLOW_TEST_LLM_URL", provider.URL+"/v1")
	cfg.LLM.BaseURLEnv = "TASKFLOW_TEST_LLM_URL"
	producer := red_mq.NewProducer(mqredis.NewUniversalClient(client))
	i := idem.New(client, time.Minute)
	manager := tcc.NewTXManager(NewTXStore(db, redis_lock.NewUniversalClient(client)), tcc.WithMonitorTick(time.Hour))
	defer manager.Stop()
	if err := manager.Register(NewExecutionReserveComponent(d, i)); err != nil {
		t.Fatal(err)
	}
	if err := manager.Register(NewMQDispatchComponent(producer, cfg.MQ.ExecTopic)); err != nil {
		t.Fatal(err)
	}
	dispatch := NewDispatcher(d, i, nil, manager, cfg.MQ, nil, nil)
	exec := &po.Execution{TaskType: po.TaskTypeManual, TaskID: 1, TaskName: "Inspection", FireKey: cfg.Node.ID + ":same-fire", FireAt: time.Now().UTC(), Status: po.StatusPending, PromptSnapshot: "Count the tables."}
	_, id, err := d.InsertPending(ctx, exec)
	if err != nil {
		t.Fatal(err)
	}
	exec.ID = id
	var wg sync.WaitGroup
	for n := 0; n < 32; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := dispatch.Dispatch(ctx, exec); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	deadline := time.Now().Add(10 * time.Second)
	var fresh *po.Execution
	for time.Now().Before(deadline) {
		fresh, err = d.GetExecution(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if fresh.Status == po.StatusQueued {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if fresh.Status != po.StatusQueued {
		t.Fatalf("dispatch did not settle: %s", fresh.Status)
	}
	agent := llm.NewAgent(cfg.LLM, db, client)
	executor := NewExecutor(cfg, d, agent, NewReportStore(d, nil, t.TempDir(), cfg.Node.ID), &DefinitionCache{}, mqredis.NewUniversalClient(client), nil, nil, nil)
	command, _ := json.Marshal(ExecCommand{ExecutionID: id, TxID: fresh.TxID, FireKey: fresh.FireKey})
	for n := 0; n < 32; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := executor.handle(ctx, &mqredis.MsgEntity{Val: string(command)}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	fresh, err = d.GetExecution(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Status != po.StatusSucceeded || fresh.ToolCalls != 1 || !strings.Contains(fresh.ReportBody, "## Summary") {
		t.Fatalf("invalid final execution: %+v", fresh)
	}
	if completions.Load() != 2 {
		t.Fatalf("provider called %d times; want one tool round and one completion", completions.Load())
	}
	// Late finalization must never replace the durable terminal report.
	executor.finalize(ctx, fresh, nil, time.Now(), fmt.Errorf("stale worker"), nil)
	after, err := d.GetExecution(ctx, id)
	if err != nil || after.Status != po.StatusSucceeded || after.ReportBody != fresh.ReportBody {
		t.Fatalf("stale finalization overwrote terminal result: %+v %v", after, err)
	}
	// Canceled execution contexts must still persist a terminal failure.
	failed := &po.Execution{TaskType: po.TaskTypeManual, FireKey: cfg.Node.ID + ":timeout", Status: po.StatusRunning, FireAt: time.Now()}
	d.InsertPending(ctx, failed)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	executor.finalize(canceled, failed, nil, time.Now(), context.DeadlineExceeded, nil)
	finished, err := d.GetExecution(ctx, failed.ID)
	if err != nil || finished.Status != po.StatusFailed || finished.ReportBody == "" {
		t.Fatalf("deadline finalization lost: %+v %v", finished, err)
	}
}

func TestOutboxSurvivesRedisOutage(t *testing.T) {
	db, client, cfg := integrationDB(t)
	ctx := context.Background()
	if err := cdc.EnsureCapture(ctx, db, "risk_records"); err != nil {
		t.Fatal(err)
	}
	db.Create(&po.ConditionTask{Name: "Outage audit", WatchTable: "risk_records", EventType: po.EventTypeMySQLInsert, Enabled: true, Prompt: "Audit the captured row."})
	db.Create(&po.RiskRecord{Title: "During outage", Content: "<script>alert(1)</script>"})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	offline := redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1, DialTimeout: 100 * time.Millisecond})
	defer offline.Close()
	failed := cdc.NewRelay(db, red_mq.NewProducer(mqredis.NewUniversalClient(offline)), cfg.MQ.CondTopic)
	if err := failed.Drain(ctx, 10); err == nil {
		t.Fatal("publication should fail while Redis is offline")
	}
	var pending int64
	db.Model(&po.Outbox{}).Where("published_at IS NULL").Count(&pending)
	if pending != 1 {
		t.Fatalf("outbox lost during outage: %d", pending)
	}
	recovered := cdc.NewRelay(db, red_mq.NewProducer(mqredis.NewUniversalClient(client)), cfg.MQ.CondTopic)
	if err := recovered.Drain(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if n, err := client.XLen(ctx, cfg.MQ.CondTopic).Result(); err != nil || n != 1 {
		t.Fatalf("recovered stream length=%d err=%v", n, err)
	}
	db.Model(&po.Execution{}).Count(&pending)
	if pending != 1 {
		t.Fatalf("outage duplicated execution: %d", pending)
	}
}

func TestAbandonedMessageRecovery(t *testing.T) {
	_, client, cfg := integrationDB(t)
	ctx := context.Background()
	if err := client.XGroupCreateMkStream(ctx, cfg.MQ.ExecTopic, cfg.MQ.ExecGroup, "0").Err(); err != nil {
		t.Fatal(err)
	}
	client.XAdd(ctx, &redis.XAddArgs{Stream: cfg.MQ.ExecTopic, Values: map[string]any{"orphan": "durable"}})
	if _, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: cfg.MQ.ExecGroup, Consumer: "crashed", Streams: []string{cfg.MQ.ExecTopic, ">"}, Count: 1}).Result(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	got := make(chan struct{}, 1)
	consumer, err := red_mq.NewConsumer(mqredis.NewUniversalClient(client), cfg.MQ.ExecTopic, cfg.MQ.ExecGroup, "replacement", func(ctx context.Context, msg *mqredis.MsgEntity) error {
		if msg.Key != "orphan" || msg.Val != "durable" {
			return fmt.Errorf("unexpected recovered payload")
		}
		select {
		case got <- struct{}{}:
		default:
		}
		return nil
	}, red_mq.WithAbandonedMessageRecovery(time.Millisecond), red_mq.WithReceiveTimeout(10*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Stop()
	select {
	case <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("abandoned PEL entry was not recovered")
	}
}

func TestBloomRequiresBothBitsAndBoundsMemory(t *testing.T) {
	_, client, cfg := integrationDB(t)
	ctx := context.Background()
	key := cfg.Node.ID + ":bloom"
	h1, h2 := timerhash.NewMurmur3Encryptor(), timerhash.NewMurmur3AltEncryptor()
	filter := timerbloom.NewFilter(timerredis.NewUniversalClient(client), h1, h2)
	value := "event-identity"
	client.SetBit(ctx, key, int64(h1.Encrypt(value)%(1<<24)), 1)
	if seen, err := filter.Exist(ctx, key, value); err != nil || seen {
		t.Fatalf("one bit is not membership: %v %v", seen, err)
	}
	if err := filter.Set(ctx, key, value, 60); err != nil {
		t.Fatal(err)
	}
	if seen, err := filter.Exist(ctx, key, value); err != nil || !seen {
		t.Fatalf("both bits must match: %v %v", seen, err)
	}
	if size, _ := client.StrLen(ctx, key).Result(); size > 2<<20 {
		t.Fatalf("bitmap grew to %d bytes", size)
	}
	if ttl, _ := client.TTL(ctx, key).Result(); ttl <= 0 {
		t.Fatal("bitmap must expire")
	}
}

func TestMirrorReconcilesBeyondFirstPage(t *testing.T) {
	db, client, cfg := integrationDB(t)
	ctx := context.Background()
	rows := make([]po.Execution, 601)
	for i := range rows {
		rows[i] = po.Execution{FireKey: fmt.Sprintf("mirror:%d", i), Status: po.StatusQueued, TaskType: po.TaskTypeManual, FireAt: time.Now()}
	}
	if err := db.CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	key, cursor := cfg.Node.ID+":mirror", cfg.Node.ID+":cursor"
	for i := 0; i < 2; i++ {
		if _, err := storage.SyncExecutionMirror(ctx, db, client, key, cursor, 500); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := client.HLen(ctx, key).Result(); n != 601 {
		t.Fatalf("sync skipped older rows: %d", n)
	}
	if err := db.Model(&rows[0]).Update("status", po.StatusSucceeded).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := storage.SyncExecutionMirror(ctx, db, client, key, cursor, 500); err != nil {
		t.Fatal(err)
	}
	raw, _ := client.HGet(ctx, key, fmt.Sprint(rows[0].ID)).Result()
	if !strings.Contains(raw, po.StatusSucceeded) {
		t.Fatalf("old-row update not reconciled: %s", raw)
	}
}
