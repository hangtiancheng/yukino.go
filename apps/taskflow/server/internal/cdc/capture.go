package cdc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"
)

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

func ValidTable(table string) bool {
	table = strings.ToLower(table)
	return identifier.MatchString(table) && !strings.HasPrefix(strings.ToLower(table), "taskflow_") &&
		table != "condition_tasks" && table != "scheduled_tasks" && table != "tcc_tx_records" && table != "mq_dead_letters"
}

func ValidWatchTable(table string) bool {
	return ValidTable(table) && strings.ToLower(table) != "executions"
}

func EnsureCapture(ctx context.Context, db *gorm.DB, table string) error {
	if !ValidTable(table) {
		return fmt.Errorf("unsupported watch table %q", table)
	}
	return db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
		var locked int
		if err := conn.Raw("SELECT GET_LOCK(?, 10)", "taskflow:capture:"+table).Scan(&locked).Error; err != nil {
			return err
		}
		if locked != 1 {
			return fmt.Errorf("capture migration lock unavailable")
		}
		defer conn.Exec("SELECT RELEASE_LOCK(?)", "taskflow:capture:"+table)
		var columns []struct {
			ColumnName string
			DataType   string
		}
		if err := conn.Raw(`SELECT COLUMN_NAME, DATA_TYPE, COLUMN_KEY FROM information_schema.columns
			WHERE table_schema = DATABASE() AND table_name = ? ORDER BY ORDINAL_POSITION`, table).Scan(&columns).Error; err != nil {
			return err
		}
		if len(columns) == 0 {
			return fmt.Errorf("watch table %q does not exist", table)
		}
		for _, operation := range []string{"INSERT", "DELETE"} {
			alias := "NEW"
			if operation == "DELETE" {
				alias = "OLD"
			}
			pairs := make([]string, 0, len(columns))
			for _, c := range columns {
				value := alias + ".`" + strings.ReplaceAll(c.ColumnName, "`", "``") + "`"
				switch c.DataType {
				case "binary", "varbinary", "blob", "tinyblob", "mediumblob", "longblob", "bit", "geometry", "point", "linestring", "polygon", "multipoint", "multilinestring", "multipolygon", "geometrycollection":
					value = "HEX(" + value + ")"
				}
				pairs = append(pairs, "'"+strings.ReplaceAll(c.ColumnName, "'", "''")+"', "+value)
			}
			sum := sha256.Sum256([]byte(table + ":" + operation))
			name := "taskflow_capture_v1_" + hex.EncodeToString(sum[:8])
			var count int64
			if err := conn.Raw("SELECT COUNT(*) FROM information_schema.triggers WHERE trigger_schema = DATABASE() AND trigger_name = ?", name).Scan(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				continue
			}
			stmt := fmt.Sprintf("CREATE TRIGGER `%s` AFTER %s ON `%s` FOR EACH ROW INSERT INTO taskflow_changes (event_key, table_name, operation, record_json, occurred_at) VALUES (UUID(), '%s', '%s', JSON_OBJECT(%s), UTC_TIMESTAMP(6))", name, operation, table, table, strings.ToLower(operation), strings.Join(pairs, ", "))
			if err := conn.Exec(stmt).Error; err != nil {
				return fmt.Errorf("install %s capture on %s: %w", operation, table, err)
			}
		}
		return nil
	})
}

func EnsureDatabaseCapture(ctx context.Context, db *gorm.DB) error {
	var tables []string
	if err := db.WithContext(ctx).Raw("SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'").Scan(&tables).Error; err != nil {
		return err
	}
	for _, table := range tables {
		if ValidTable(table) {
			if err := EnsureCapture(ctx, db, table); err != nil {
				return err
			}
		}
	}
	return nil
}
