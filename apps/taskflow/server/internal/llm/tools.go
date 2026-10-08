package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	ToolMySQL = "mysql_tool"
	ToolRedis = "redis_tool"
)

type ToolCallLog struct {
	Round    int             `json:"round"`
	Name     string          `json:"name"`
	Args     json.RawMessage `json:"args"`
	Result   string          `json:"result"`
	Duration time.Duration   `json:"-"`
	Error    bool            `json:"error"`
}

type ToolRuntime struct {
	DB          *gorm.DB
	Redis       redis.UniversalClient
	MaxRows     int
	AllowWrites bool
	KeyPrefix   string
}

func (r *ToolRuntime) Definitions() []openai.ChatCompletionToolParam {
	sqlDescription := "Execute ONE read-only SQL statement against MySQL. Allowed: SELECT / SHOW / DESCRIBE / EXPLAIN. Results are bounded. DDL, admin and locking statements are rejected."
	if r.AllowWrites {
		sqlDescription += " Business INSERT / UPDATE / DELETE are also enabled; control-table mutations are forbidden."
	}
	return []openai.ChatCompletionToolParam{
		{
			Function: shared.FunctionDefinitionParam{
				Name:        ToolMySQL,
				Description: openai.String(sqlDescription),
				Parameters: shared.FunctionParameters{
					"type": "object",
					"properties": map[string]any{
						"sql": map[string]any{
							"type":        "string",
							"description": "A single SQL statement to execute.",
						},
					},
					"required":             []string{"sql"},
					"additionalProperties": false,
				},
			},
		},
		{
			Function: shared.FunctionDefinitionParam{
				Name:        ToolRedis,
				Description: openai.String("Operate on redis. Supported operations: get, set, del, exists, ttl, incr, decr, hget, hset, hgetall, keys. Keys must start with " + r.KeyPrefix + "; listings and values are bounded."),
				Parameters: shared.FunctionParameters{
					"type": "object",
					"properties": map[string]any{
						"operation": map[string]any{
							"type": "string",
							"enum": []string{"get", "set", "del", "exists", "ttl", "incr", "decr", "hget", "hset", "hgetall", "keys"},
						},
						"key":         map[string]any{"type": "string"},
						"value":       map[string]any{"type": "string"},
						"field":       map[string]any{"type": "string", "description": "hash field for hget/hset"},
						"ttl_seconds": map[string]any{"type": "integer", "description": "optional TTL for set"},
						"pattern":     map[string]any{"type": "string", "description": "glob pattern for keys"},
					},
					"required":             []string{"operation"},
					"additionalProperties": false,
				},
			},
		},
	}
}

// Execute dispatches a model-issued tool call and returns the JSON text that
// goes back to the conversation.
func (r *ToolRuntime) Execute(ctx context.Context, name string, argsJSON string) (string, bool) {
	switch name {
	case ToolMySQL:
		return r.execMySQL(ctx, argsJSON)
	case ToolRedis:
		return r.execRedis(ctx, argsJSON)
	default:
		return jsonErr(fmt.Sprintf("unknown tool %q", name)), true
	}
}

var forbiddenKeywords = []string{
	"drop", "truncate", "alter", "create", "grant", "revoke", "rename",
	"lock", "unlock", "flush", "reset", "shutdown", "kill", "set",
	"use", "load", "load_file", "call", "install", "uninstall", "handler",
	"optimize", "repair", "analyze", "purge", "change", "start", "stop",
	"outfile", "dumpfile",
}

var forbiddenKeywordSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(forbiddenKeywords))
	for _, kw := range forbiddenKeywords {
		m[kw] = struct{}{}
	}
	return m
}()

func (r *ToolRuntime) execMySQL(ctx context.Context, argsJSON string) (string, bool) {
	var args struct {
		SQL string `json:"sql"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonErr("invalid arguments: " + err.Error()), true
	}
	sql := strings.TrimSpace(args.SQL)
	if sql == "" {
		return jsonErr("empty sql"), true
	}
	if len(sql) > 64<<10 {
		return jsonErr("SQL exceeds 65536 bytes"), true
	}
	firstWord, err := validateSQL(sql, r.AllowWrites)
	if err != nil {
		return jsonErr(err.Error()), true
	}

	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	switch firstWord {
	case "insert", "update", "delete":
		res := r.DB.WithContext(queryCtx).Exec(sql)
		if res.Error != nil {
			return jsonErr(res.Error.Error()), true
		}
		return jsonOK(map[string]any{"affected_rows": res.RowsAffected}), false
	default:
		cursor, err := r.DB.WithContext(queryCtx).Raw(sql).Rows()
		if err != nil {
			return jsonErr(err.Error()), true
		}
		defer cursor.Close()
		columns, err := cursor.Columns()
		if err != nil {
			return jsonErr(err.Error()), true
		}
		limit := r.MaxRows
		if limit <= 0 {
			limit = 100
		}
		rows := make([]map[string]any, 0, limit)
		capped := false
		bytesRead := 0
		for cursor.Next() {
			if len(rows) >= limit || bytesRead >= 1<<20 {
				capped = true
				break
			}
			values := make([]any, len(columns))
			dest := make([]any, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			if err := cursor.Scan(dest...); err != nil {
				return jsonErr(err.Error()), true
			}
			row := make(map[string]any, len(columns))
			for i, name := range columns {
				if v, ok := values[i].([]byte); ok {
					values[i] = string(v)
				}
				if v, ok := values[i].(string); ok && len(v) > 64<<10 {
					values[i] = strings.ToValidUTF8(v[:64<<10], "")
					capped = true
				}
				row[name] = values[i]
			}
			encoded, err := json.Marshal(row)
			if err != nil {
				return jsonErr(err.Error()), true
			}
			if bytesRead+len(encoded) > 1<<20 {
				capped = true
				break
			}
			bytesRead += len(encoded)
			rows = append(rows, row)
		}
		if err := cursor.Err(); err != nil {
			return jsonErr(err.Error()), true
		}
		return jsonOK(map[string]any{
			"columns":      columns,
			"rows":         rows,
			"row_count":    len(rows),
			"truncated":    capped,
			"executed_sql": sql,
		}), false
	}
}

var allowedRedisOps = map[string]struct{}{
	"get": {}, "set": {}, "del": {}, "exists": {}, "ttl": {}, "incr": {},
	"decr": {}, "hget": {}, "hset": {}, "hgetall": {}, "keys": {},
}

func (r *ToolRuntime) execRedis(ctx context.Context, argsJSON string) (string, bool) {
	var args struct {
		Operation  string `json:"operation"`
		Key        string `json:"key"`
		Value      string `json:"value"`
		Field      string `json:"field"`
		TTLSeconds int64  `json:"ttl_seconds"`
		Pattern    string `json:"pattern"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonErr("invalid arguments: " + err.Error()), true
	}

	op := strings.ToLower(strings.TrimSpace(args.Operation))
	if _, ok := allowedRedisOps[op]; !ok {
		return jsonErr(fmt.Sprintf("unsupported redis operation %q", op)), true
	}
	if op != "keys" && args.Key == "" {
		return jsonErr("key is required"), true
	}
	if op != "keys" && (r.KeyPrefix == "" || !strings.HasPrefix(args.Key, r.KeyPrefix)) {
		return jsonErr("key must start with " + r.KeyPrefix), true
	}
	if len(args.Value) > 64<<10 || len(args.Field) > 512 || len(args.Key) > 512 {
		return jsonErr("Redis argument exceeds size limit"), true
	}
	if args.TTLSeconds < 0 || args.TTLSeconds > 30*24*60*60 {
		return jsonErr("TTL must be between 0 and 2592000 seconds"), true
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	switch op {
	case "get":
		exists, err := r.Redis.Exists(cmdCtx, args.Key).Result()
		if err != nil {
			return jsonErr(err.Error()), true
		}
		if exists == 0 {
			return jsonOK(map[string]any{"exists": false}), false
		}
		val, err := r.Redis.GetRange(cmdCtx, args.Key, 0, (64<<10)-1).Result()
		if err == redis.Nil {
			return jsonOK(map[string]any{"exists": false}), false
		}
		if err != nil {
			return jsonErr(err.Error()), true
		}
		return jsonOK(map[string]any{"exists": true, "value": val, "truncated": len(val) >= 64<<10}), false
	case "set":
		if err := r.Redis.Set(cmdCtx, args.Key, args.Value, time.Duration(args.TTLSeconds)*time.Second).Err(); err != nil {
			return jsonErr(err.Error()), true
		}
		return jsonOK(map[string]any{"ok": true}), false
	case "del":
		n, err := r.Redis.Del(cmdCtx, args.Key).Result()
		if err != nil {
			return jsonErr(err.Error()), true
		}
		return jsonOK(map[string]any{"deleted": n}), false
	case "exists":
		n, err := r.Redis.Exists(cmdCtx, args.Key).Result()
		if err != nil {
			return jsonErr(err.Error()), true
		}
		return jsonOK(map[string]any{"exists": n > 0}), false
	case "ttl":
		d, err := r.Redis.TTL(cmdCtx, args.Key).Result()
		if err != nil {
			return jsonErr(err.Error()), true
		}
		return jsonOK(map[string]any{"ttl_seconds": int64(d.Seconds())}), false
	case "incr":
		n, err := r.Redis.Incr(cmdCtx, args.Key).Result()
		if err != nil {
			return jsonErr(err.Error()), true
		}
		return jsonOK(map[string]any{"value": n}), false
	case "decr":
		n, err := r.Redis.Decr(cmdCtx, args.Key).Result()
		if err != nil {
			return jsonErr(err.Error()), true
		}
		return jsonOK(map[string]any{"value": n}), false
	case "hget":
		val, err := r.Redis.HGet(cmdCtx, args.Key, args.Field).Result()
		if err == redis.Nil {
			return jsonOK(map[string]any{"exists": false}), false
		}
		if err != nil {
			return jsonErr(err.Error()), true
		}
		if len(val) > 64<<10 {
			val = val[:64<<10]
		}
		return jsonOK(map[string]any{"exists": true, "value": val, "truncated": len(val) >= 64<<10}), false
	case "hset":
		n, err := r.Redis.HSet(cmdCtx, args.Key, args.Field, args.Value).Result()
		if err != nil {
			return jsonErr(err.Error()), true
		}
		return jsonOK(map[string]any{"added_fields": n}), false
	case "hgetall":
		// HSCAN avoids loading an unbounded hash. Compact encodings may return
		// more than COUNT; the returned fields and values are capped as well.
		fields := make(map[string]string)
		var cursor uint64
		truncated := false
		for scan := 0; scan < 100; scan++ {
			batch, next, err := r.Redis.HScan(cmdCtx, args.Key, cursor, "*", 100).Result()
			if err != nil {
				return jsonErr(err.Error()), true
			}
			for i := 0; i+1 < len(batch); i += 2 {
				if len(fields) >= 100 {
					truncated = true
					break
				}
				value := batch[i+1]
				if len(value) > 1024 {
					value = value[:1024]
					truncated = true
				}
				fields[batch[i]] = value
			}
			cursor = next
			if cursor == 0 || len(fields) >= 100 {
				truncated = truncated || cursor != 0
				break
			}
			truncated = scan == 99
		}
		return jsonOK(map[string]any{"fields": fields, "truncated": truncated}), false

	case "keys":
		pattern := args.Pattern
		if pattern == "" {
			pattern = r.KeyPrefix + "*"
		}
		if r.KeyPrefix == "" || !strings.HasPrefix(pattern, r.KeyPrefix) {
			return jsonErr("pattern must start with " + r.KeyPrefix), true
		}
		keys := make([]string, 0, 100)
		var cursor uint64
		truncated := false
		for scan := 0; scan < 100; scan++ {
			batch, next, err := r.Redis.Scan(cmdCtx, cursor, pattern, 100).Result()
			if err != nil {
				return jsonErr(err.Error()), true
			}
			keys = append(keys, batch...)
			cursor = next
			if len(keys) > 100 {
				keys = keys[:100]
				truncated = true
				break
			}
			if cursor == 0 {
				break
			}
			truncated = scan == 99
		}
		return jsonOK(map[string]any{"keys": keys, "truncated": truncated}), false
	}
	return jsonErr("unreachable"), true
}

func jsonOK(v any) string {
	body, err := json.Marshal(map[string]any{"ok": true, "data": v})
	if err != nil {
		return `{"ok":false,"error":"marshal failed"}`
	}
	return string(body)
}

func jsonErr(msg string) string {
	body, _ := json.Marshal(map[string]any{"ok": false, "error": msg})
	return string(body)
}
