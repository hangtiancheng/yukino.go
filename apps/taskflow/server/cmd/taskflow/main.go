// Command taskflow boots the monolithic conditional-task / scheduled-task
// service: gorm+mysql persistence, redis-backed distributed components from
// components/ (time wheel, locks, streams MQ, consistent cache/hash, TCC),
// yukino_cache report caching, the openai-go agent, sentry-go and otel
// telemetry, all behind a yukino_http API.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/cdc"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/conf"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/consensus"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/cronx"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/dao"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/engine"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/idem"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/journal"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/obsx"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/server"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/storage"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/telemetry"
	tcc "github.com/hangtiancheng/yukino.go/components/tcc"

	"github.com/hangtiancheng/yukino.go/components/consistent_cache"
	ccmysql "github.com/hangtiancheng/yukino.go/components/consistent_cache/mysql"
	ccredis "github.com/hangtiancheng/yukino.go/components/consistent_cache/redis"
	"github.com/hangtiancheng/yukino.go/components/consistent_hash"
	chredis "github.com/hangtiancheng/yukino.go/components/consistent_hash/redis"
	"github.com/hangtiancheng/yukino.go/components/red_mq"
	mqredis "github.com/hangtiancheng/yukino.go/components/red_mq/redis"
	"github.com/hangtiancheng/yukino.go/components/redis_lock"
	"github.com/hangtiancheng/yukino.go/components/time_wheel"
	time_wheel_http "github.com/hangtiancheng/yukino.go/components/time_wheel/pkg/http"
	twredis "github.com/hangtiancheng/yukino.go/components/time_wheel/pkg/redis"

	"github.com/hangtiancheng/yukino.go/libs/yukino_cache"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func main() {
	configPath := flag.String("config", "conf.yml", "path to conf.yml")
	flag.Parse()

	if err := run(*configPath); err != nil {
		slog.Error("taskflow exited with error", "err", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := conf.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	slog.Info("taskflow starting",
		"node", cfg.Node.ID,
		"addr", cfg.Server.Addr(),
		"llm_model", cfg.LLM.Model(),
		"llm_base_url", cfg.LLM.BaseURL(),
	)

	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	telMgr, err := telemetry.Setup(ctx, cfg.Telemetry, cfg.Node.ID)
	if err != nil {
		return fmt.Errorf("telemetry setup: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = telMgr.Shutdown(shutdownCtx)
	}()

	if err := obsx.Setup(cfg.Sentry, cfg.Node.ID); err != nil {
		return err
	}
	defer obsx.Flush(2 * time.Second)

	db, err := openMySQL(cfg)
	if err != nil {
		return err
	}
	if err := migrate(ctx, db); err != nil {
		return err
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}
	if err := cdc.EnsureDatabaseCapture(ctx, db); err != nil {
		return err
	}

	redisClient := storage.OpenRedis(cfg.Redis)
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping: %w", err)
	}

	d := dao.New(db)
	watched, err := d.EnabledConditionTasks(ctx, "")
	if err != nil {
		return err
	}
	for _, task := range watched {
		if err := cdc.EnsureCapture(ctx, db, task.WatchTable); err != nil {
			return fmt.Errorf("watch table %s: %w", task.WatchTable, err)
		}
	}
	fleet, err := storage.NewFleet(db, redisClient, cfg.MySQL)
	if err != nil {
		return err
	}
	defer fleet.Close()
	idemSvc := idem.New(redisClient, time.Duration(cfg.Scheduler.ClaimTTLHours)*time.Hour)
	bloomFilter := idem.NewSharedBloomFilter(redisClient, time.Duration(cfg.Scheduler.ClaimTTLHours)*time.Hour)
	agent := llm.NewAgent(cfg.LLM, db, redisClient)

	journalStore, err := journal.Open(cfg.Journal.Dir, cfg.Node.ID)
	if err != nil {
		return fmt.Errorf("open lsm journal: %w", err)
	}
	defer journalStore.Close()

	ledger := consensus.NewLedger(cfg.Consensus.ID)
	defer ledger.Stop()

	peers, closePeers, err := engine.OpenReportPeers(cfg.Cache)
	if err != nil {
		return fmt.Errorf("report cache peers: %w", err)
	}
	defer closePeers()

	reportGroup := yukino_cache.NewGroup(
		cfg.Cache.ReportGroup,
		int64(cfg.Cache.ReportCacheBytesMiB)<<20,
		yukino_cache.GetterFunc(func(loadCtx context.Context, key string) ([]byte, error) {
			var id uint
			if _, err := fmt.Sscanf(key, "exec:%d", &id); err != nil {
				return nil, fmt.Errorf("bad report key %q", key)
			}
			exec, err := d.GetExecution(loadCtx, id)
			if err != nil {
				return nil, err
			}
			if exec.ReportBody == "" {
				return nil, errors.New("report not ready")
			}
			return []byte(exec.ReportBody), nil
		}),
		yukino_cache.WithExpiration(time.Duration(cfg.Cache.ReportExpireSeconds)*time.Second),
	)
	defer reportGroup.Close()
	if peers != nil {
		reportGroup.RegisterPeers(peers)
	}

	defCacheDB := ccmysql.NewWithGorm(db)
	defCacheRedis := ccredis.NewUniversalClient(redisClient)
	defs := engine.NewDefinitionCache(
		d,
		ccredis.NewCache(defCacheRedis),
		ccredis.NewCache(defCacheRedis),
		defCacheDB,
		consistent_cache.WithCacheExpireSeconds(int64(cfg.Cache.DefCacheExpireSeconds)),
		consistent_cache.WithCacheExpireRandomMode(),
		consistent_cache.WithDisableExpireSeconds(1),
	)

	lockClient := redis_lock.NewUniversalClient(redisClient)
	mqClient := mqredis.NewUniversalClient(redisClient)
	wheelClient := twredis.NewUniversalClient(redisClient)
	hashClient := chredis.NewUniversalClient(redisClient)

	producer := red_mq.NewProducer(mqClient, red_mq.WithMsgQueueLen(cfg.MQ.MsgQueueLen))
	wheel := time_wheel.NewRTimeWheel(wheelClient, time_wheel_http.NewClient())
	defer wheel.Stop()

	hashRing := chredis.NewRedisHashRing("taskflow:nodes", hashClient)
	ring := consistent_hash.NewConsistentHash(hashRing, consistent_hash.NewFnvHasher(), engine.RingMigrator)
	registry := engine.NewSingletonRegistry(ring, hashRing, redisClient, cfg.Node.ID)
	if err := registry.Register(ctx); err != nil {
		return fmt.Errorf("register node in ring: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		registry.Deregister(cleanup)
	}()

	txStore := engine.NewTXStore(db, lockClient)
	txManager := tcc.NewTXManager(txStore,
		tcc.WithTimeout(30*time.Second),
		tcc.WithMonitorTick(5*time.Second),
	)
	defer txManager.Stop()

	dispatcher := engine.NewDispatcher(d, idemSvc, bloomFilter, txManager, cfg.MQ, journalStore, ledger)
	reports := engine.NewReportStore(d, reportGroup, cfg.Reports.Dir, cfg.Node.ID)
	mailbox := engine.NewDeadLetterMailbox(d)

	if err := txManager.Register(engine.NewExecutionReserveComponent(d, idemSvc)); err != nil {
		return err
	}
	if err := txManager.Register(engine.NewMQDispatchComponent(producer, cfg.MQ.ExecTopic)); err != nil {
		return err
	}

	executor := engine.NewExecutor(cfg, d, agent, reports, defs, mqClient, mailbox, journalStore, ledger)
	condition := engine.NewConditionPipeline(cfg, d, idemSvc, producer, dispatcher, defs, mqClient, mailbox)
	relay := cdc.NewRelay(db, producer, cfg.MQ.CondTopic)
	monitor := engine.NewMonitor(cfg, d, dispatcher, registry, redisClient, lockClient, producer, defs, cfg.Scheduler.RecoverWorkers)
	migrator := engine.NewMigrator(cfg, d, wheel, registry, lockClient, defs)

	if err := seedDemoTasks(d); err != nil {
		slog.Warn("seed demo tasks failed", "err", err)
	}

	svc, err := server.NewService(server.Deps{
		Cfg:           cfg,
		DAO:           d,
		Defs:          defs,
		Dispatcher:    dispatcher,
		Condition:     condition,
		Monitor:       monitor,
		Registry:      registry,
		Reports:       reports,
		Journal:       journalStore,
		Ledger:        ledger,
		StorageStatus: fleet.Status,
		Ready: func(checkCtx context.Context) error {
			sqlDB, err := db.DB()
			if err != nil {
				return err
			}
			if err := sqlDB.PingContext(checkCtx); err != nil {
				return err
			}
			return redisClient.Ping(checkCtx).Err()
		},
	})
	if err != nil {
		return err
	}
	defer svc.Close()

	app := svc.Build()
	httpServer := &http.Server{Addr: cfg.Server.Addr(), Handler: app, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: time.Duration(cfg.Server.ReadTimeoutS) * time.Second, WriteTimeout: time.Duration(cfg.Server.WriteTimeoutS) * time.Second, IdleTimeout: 60 * time.Second}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(cleanup)
	}()
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server crashed", "err", err)
			stopSignals()
		}
	}()

	migrator.Start()
	defer migrator.Stop()
	monitor.Start()
	defer monitor.Stop()
	defer executor.Stop()
	if err := executor.Start(cfg.Executor.Workers); err != nil {
		return err
	}
	defer condition.Stop()
	if err := condition.Start(cfg.Executor.Workers); err != nil {
		return err
	}
	relay.Start(ctx)
	defer relay.Stop()

	slog.Info("taskflow started", "api", "http://"+cfg.Server.Addr()+"/api/v1/health")
	<-ctx.Done()
	slog.Info("shutting down")
	relay.Stop()

	executor.Stop()
	condition.Stop()
	migrator.Stop()
	monitor.Stop()
	wheel.Stop()
	txManager.Stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	registry.Deregister(shutdownCtx)

	return nil
}

func migrate(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
		var acquired int
		if err := conn.Raw("SELECT GET_LOCK('taskflow:schema:migration', 30)").Scan(&acquired).Error; err != nil {
			return err
		}
		if acquired != 1 {
			return fmt.Errorf("schema migration lock unavailable")
		}
		defer conn.Exec("SELECT RELEASE_LOCK('taskflow:schema:migration')")
		return po.AutoMigrate(conn)
	})
}

func openMySQL(cfg *conf.Config) (*gorm.DB, error) {
	serverDSN := cfg.MySQL.DSN("")

	serverDB, err := gorm.Open(gormmysql.Open(serverDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, fmt.Errorf("connect mysql server: %w", err)
	}
	if pool, err := serverDB.DB(); err == nil {
		defer pool.Close()
	}
	if err := serverDB.Exec("CREATE DATABASE IF NOT EXISTS `" + cfg.MySQL.Database + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci").Error; err != nil {
		return nil, fmt.Errorf("create database %s: %w", cfg.MySQL.Database, err)
	}
	if sqlDB, err := serverDB.DB(); err == nil {
		_ = sqlDB.Close()
	}

	db, err := gorm.Open(gormmysql.Open(cfg.MySQL.DSN(cfg.MySQL.Database)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("connect mysql database %s: %w", cfg.MySQL.Database, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(cfg.MySQL.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MySQL.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	return db, nil
}

// seedDemoTasks inserts the two examples through their unique name indexes.
// Existing definitions remain under operator control.
func seedDemoTasks(d *dao.DAO) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := seedScheduledDemo(ctx, d); err != nil {
		return err
	}
	return seedConditionDemo(ctx, d)
}

const (
	demoScheduledCron = "0 10 * * *"
	demoScheduledTZ   = "Asia/Shanghai"
	demoScheduledName = "Daily MySQL Data Inspection"
	demoConditionTbl  = "risk_records"
	demoConditionName = "Insert Record Security Audit (XSS Risk)"
)

func seedScheduledDemo(ctx context.Context, d *dao.DAO) error {
	loc, _ := time.LoadLocation(demoScheduledTZ)
	schedule, err := cronx.Parse(demoScheduledCron)
	if err != nil {
		return err
	}
	next := schedule.Next(time.Now().In(loc))
	row := &po.ScheduledTask{Name: demoScheduledName, Description: "Daily table count and transactional insert/delete comparison.", CronExpr: demoScheduledCron, Timezone: demoScheduledTZ, Prompt: engine.ScheduledTaskSeedPrompt, Enabled: true, NextFireAt: &next}
	return d.DB().WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error
}

func seedConditionDemo(ctx context.Context, d *dao.DAO) error {
	row := &po.ConditionTask{Name: demoConditionName, Description: "Analyze inserted records for persistent security risks.", EventType: po.EventTypeMySQLInsert, WatchTable: demoConditionTbl, Prompt: engine.ConditionTaskSeedPrompt, Enabled: true}
	return d.DB().WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error
}
