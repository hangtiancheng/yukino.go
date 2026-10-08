package storage

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/conf"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/redis/go-redis/v9"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Fleet struct {
	primary  *gorm.DB
	redis    redis.UniversalClient
	replicas map[string]*gorm.DB
}

func NewFleet(db *gorm.DB, client redis.UniversalClient, cfg conf.MySQLConf) (*Fleet, error) {
	f := &Fleet{primary: db, redis: client, replicas: make(map[string]*gorm.DB)}
	for _, address := range cfg.ReplicaAddresses {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid replica address: %w", err)
		}
		replica := cfg
		replica.Host = host
		replica.Port, err = strconv.Atoi(port)
		if err != nil {
			return nil, err
		}
		conn, err := gorm.Open(gormmysql.New(gormmysql.Config{DSN: replica.DSN(cfg.Database), SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			return nil, err
		}
		pool, err := conn.DB()
		if err != nil {
			return nil, err
		}
		pool.SetMaxOpenConns(2)
		pool.SetMaxIdleConns(1)
		f.replicas[address] = conn
	}
	return f, nil
}

func (f *Fleet) Close() {
	for _, db := range f.replicas {
		if pool, err := db.DB(); err == nil {
			_ = pool.Close()
		}
	}
}

func (f *Fleet) Status(ctx context.Context) map[string]any {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var pendingChanges, pendingOutbox int64
	f.primary.WithContext(ctx).Model(&po.ChangeEvent{}).Where("processed_at IS NULL").Count(&pendingChanges)
	f.primary.WithContext(ctx).Model(&po.Outbox{}).Where("published_at IS NULL").Count(&pendingOutbox)
	out := map[string]any{"pending_changes": pendingChanges, "pending_outbox": pendingOutbox, "mysql_replicas": []map[string]any{}, "redis": map[string]string{}}
	var capture []map[string]any
	if err := f.primary.WithContext(ctx).Raw("SELECT event_object_table AS table_name, MIN(created) AS installed_at FROM information_schema.triggers WHERE trigger_schema = DATABASE() AND LEFT(trigger_name, 20) = 'taskflow_capture_v1_' GROUP BY event_object_table").Scan(&capture).Error; err == nil {
		out["capture_tables"] = capture
	}
	replicas := make([]map[string]any, 0, len(f.replicas))
	for address, db := range f.replicas {
		row := map[string]any{"address": address, "healthy": false}
		var states []map[string]any
		if err := db.WithContext(ctx).Raw("SHOW REPLICA STATUS").Scan(&states).Error; err != nil {
			row["error"] = "replica unavailable"
		} else if len(states) > 0 {
			state := states[0]
			for _, key := range []string{"Replica_IO_Running", "Replica_SQL_Running", "Seconds_Behind_Source", "Last_IO_Error", "Last_SQL_Error"} {
				row[key] = stringValue(state[key])
			}
			row["healthy"] = row["Replica_IO_Running"] == "Yes" && row["Replica_SQL_Running"] == "Yes"
		}
		replicas = append(replicas, row)
	}
	out["mysql_replicas"] = replicas
	if info, err := f.redis.Info(ctx, "replication").Result(); err == nil {
		out["redis"] = parseInfo(info)
	} else {
		out["redis_error"] = "redis unavailable"
	}
	return out
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return fmt.Sprint(v)
}
func parseInfo(raw string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && !strings.HasPrefix(key, "#") {
			result[key] = value
		}
	}
	return result
}
