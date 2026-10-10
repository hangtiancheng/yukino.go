package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type MysqlCrudInput struct {
	DSN         string `json:"dsn" jsonschema:"required" jsonschema_description:"MySQL DSN, including username/password/host/port/database name, e.g., root:pass@tcp(host:3306)/db"`
	SQL         string `json:"sql" jsonschema:"required" jsonschema_description:"SQL statement to execute"`
	OperateType string `json:"operate_type" jsonschema:"required,enum=query,enum=insert,enum=update,enum=delete" jsonschema_description:"SQL operation type"`
}

func NewMysqlCrudTool() (tool.InvokableTool, error) {
	t, err := utils.InferOptionableTool(
		"mysql_crud",
		"Execute SQL queries against a MySQL database and return results in JSON format. Supports query, insert, update, and delete operations. Results are formatted as JSON for easy parsing.",
		func(ctx context.Context, input *MysqlCrudInput, opts ...tool.Option) (string, error) {
			result, err := execMysqlSql(input)
			if err != nil {
				b, _ := json.Marshal(map[string]any{
					"success": false,
					"error":   err.Error(),
					"message": "Failed to execute SQL against MySQL",
				})
				return string(b), nil
			}
			return result, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("infer mysql_crud tool: %w", err)
	}
	return t, nil
}

func normalizeDsn(dsn string) string {
	if strings.HasPrefix(dsn, "mysql://") {
		u, err := url.Parse(dsn)
		if err != nil || u.User == nil {
			return dsn
		}
		password, _ := u.User.Password()
		db := strings.TrimPrefix(u.Path, "/")
		dsn = fmt.Sprintf("%s:%s@tcp(%s)/%s", u.User.Username(), password, u.Host, db)
	}
	if strings.Contains(dsn, "parseTime=") {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "parseTime=true"
}

func execMysqlSql(input *MysqlCrudInput) (string, error) {
	db, err := gorm.Open(mysql.Open(normalizeDsn(input.DSN)), &gorm.Config{})
	if err != nil {
		return "", fmt.Errorf("open mysql: %w", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	if input.OperateType == "query" {
		var results []map[string]any
		if err := db.Raw(input.SQL).Scan(&results).Error; err != nil {
			return "", fmt.Errorf("query mysql: %w", err)
		}
		b, err := json.Marshal(results)
		if err != nil {
			return "", fmt.Errorf("marshal query result: %w", err)
		}
		return string(b), nil
	}

	if err := db.Exec(input.SQL).Error; err != nil {
		return "", fmt.Errorf("exec mysql: %w", err)
	}
	resp := map[string]any{
		"success": true,
		"message": fmt.Sprintf("Executed %s sql", input.OperateType),
	}
	b, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("marshal exec result: %w", err)
	}
	return string(b), nil
}
