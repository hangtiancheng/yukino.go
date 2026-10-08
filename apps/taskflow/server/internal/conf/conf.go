package conf

import (
	"fmt"
	"net"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConf    `yaml:"server"`
	Node      NodeConf      `yaml:"node"`
	MySQL     MySQLConf     `yaml:"mysql"`
	Redis     RedisConf     `yaml:"redis"`
	LLM       LLMConf       `yaml:"llm"`
	Scheduler SchedulerConf `yaml:"scheduler"`
	MQ        MQConf        `yaml:"mq"`
	Executor  ExecutorConf  `yaml:"executor"`
	Cache     CacheConf     `yaml:"cache"`
	Reports   ReportsConf   `yaml:"reports"`
	Journal   JournalConf   `yaml:"journal"`
	Consensus ConsensusConf `yaml:"consensus"`
	Sentry    SentryConf    `yaml:"sentry"`
	Telemetry TelemetryConf `yaml:"telemetry"`
}

type ServerConf struct {
	Host               string   `yaml:"host"`
	Port               int      `yaml:"port"`
	CORSAllowAll       bool     `yaml:"cors_allow_all"`
	CORSAllowedOrigins []string `yaml:"cors_allowed_origins"`
	ReadTimeoutS       int      `yaml:"read_timeout_seconds"`
	WriteTimeoutS      int      `yaml:"write_timeout_seconds"`
	APITokenEnv        string   `yaml:"api_token_env"`
	InternalTokenEnv   string   `yaml:"internal_token_env"`
}

func (s ServerConf) APIToken() string      { return os.Getenv(s.APITokenEnv) }
func (s ServerConf) InternalToken() string { return os.Getenv(s.InternalTokenEnv) }

func (s ServerConf) Addr() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Port)) }

// BaseURL is the advertised address used for in-process callbacks (the
// distributed time wheel POSTs to /internal/v1/fire on this host). A wildcard
// bind host (0.0.0.0 / ::) is not connectable, so it is rewritten to loopback;
// the callback always targets this same process.
func (s ServerConf) BaseURL() string {
	host := s.Host
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s", net.JoinHostPort(host, strconv.Itoa(s.Port)))
}

type NodeConf struct {
	ID string `yaml:"id"`
}

type MySQLConf struct {
	Host             string   `yaml:"host"`
	Port             int      `yaml:"port"`
	User             string   `yaml:"user"`
	PasswordEnv      string   `yaml:"password_env"`
	Database         string   `yaml:"database"`
	MaxOpenConns     int      `yaml:"max_open_conns"`
	MaxIdleConns     int      `yaml:"max_idle_conns"`
	ReplicaAddresses []string `yaml:"replica_addresses"`
}

func (m MySQLConf) Password() string { return os.Getenv(m.PasswordEnv) }

func (m MySQLConf) DSN(database string) string {
	return fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8mb4&parseTime=True&loc=UTC&timeout=5s&time_zone=%%27%%2B00%%3A00%%27",
		m.User, m.Password(), net.JoinHostPort(m.Host, strconv.Itoa(m.Port)), database)
}

type RedisConf struct {
	Address             string   `yaml:"address"`
	Password            string   `yaml:"password"`
	DB                  int      `yaml:"db"`
	Mode                string   `yaml:"mode"`
	Addresses           []string `yaml:"addresses"`
	MasterName          string   `yaml:"master_name"`
	SentinelPasswordEnv string   `yaml:"sentinel_password_env"`
}

type LLMConf struct {
	BaseURLEnv            string `yaml:"base_url_env"`
	APIKeyEnv             string `yaml:"api_key_env"`
	ModelEnv              string `yaml:"model_env"`
	DefaultModel          string `yaml:"default_model"`
	MaxToolRounds         int    `yaml:"max_tool_rounds"`
	RequestTimeoutSeconds int    `yaml:"request_timeout_seconds"`
	MaxOutputTokens       int    `yaml:"max_output_tokens"`
	MaxResultRows         int    `yaml:"max_result_rows"`
	AllowSQLWrites        bool   `yaml:"allow_sql_writes"`
	RedisKeyPrefix        string `yaml:"redis_key_prefix"`
}

func (l LLMConf) BaseURL() string { return os.Getenv(l.BaseURLEnv) }
func (l LLMConf) APIKey() string  { return os.Getenv(l.APIKeyEnv) }
func (l LLMConf) Model() string {
	if m := os.Getenv(l.ModelEnv); m != "" {
		return m
	}
	return l.DefaultModel
}

type SchedulerConf struct {
	MigrateStepMinutes    int `yaml:"migrate_step_minutes"`
	MigrateTickSeconds    int `yaml:"migrate_tick_seconds"`
	ClaimTTLHours         int `yaml:"claim_ttl_hours"`
	StuckRunningMinutes   int `yaml:"stuck_running_minutes"`
	OverdueRecoverMinutes int `yaml:"overdue_recover_minutes"`
	RecoverWorkers        int `yaml:"recover_workers"`
}

type MQConf struct {
	ExecTopic   string `yaml:"exec_topic"`
	CondTopic   string `yaml:"cond_topic"`
	ExecGroup   string `yaml:"exec_group"`
	CondGroup   string `yaml:"cond_group"`
	MsgQueueLen int    `yaml:"msg_queue_len"`
	MaxRetry    int    `yaml:"max_retry"`
}

type ExecutorConf struct {
	Workers              int `yaml:"workers"`
	TimeoutSeconds       int `yaml:"timeout_seconds"`
	ReceiveTimeoutMs     int `yaml:"receive_timeout_ms"`
	HandleTimeoutSeconds int `yaml:"handle_timeout_seconds"`
}

type CacheConf struct {
	ReportGroup           string `yaml:"report_group"`
	ReportCacheBytesMiB   int    `yaml:"report_cache_bytes_mib"`
	ReportExpireSeconds   int    `yaml:"report_expire_seconds"`
	DefCacheExpireSeconds int    `yaml:"def_cache_expire_seconds"`
	EtcdEndpoints         string `yaml:"etcd_endpoints"`
	CacheServerAddr       string `yaml:"cache_server_addr"`
}

type ReportsConf struct {
	Dir string `yaml:"dir"`
}

// JournalConf locates the node-local lsm_tree audit ledger. An empty dir
// disables journaling.
type JournalConf struct {
	Dir string `yaml:"dir"`
}

// ConsensusConf configures the embedded raft ledger (single-member
// group per node; see internal/consensus).
type ConsensusConf struct {
	ID uint64 `yaml:"id"`
}

type SentryConf struct {
	DSNEnv      string `yaml:"dsn_env"`
	Environment string `yaml:"environment"`
	Release     string `yaml:"release"`
}

func (s SentryConf) DSN() string { return os.Getenv(s.DSNEnv) }

type TelemetryConf struct {
	Enabled       bool    `yaml:"enabled"`
	Exporter      string  `yaml:"exporter"`
	FilePath      string  `yaml:"file_path"`
	ClientLogPath string  `yaml:"client_log_path"`
	SampleRate    float64 `yaml:"sample_rate"`
}

func (t TelemetryConf) ClientLog() string {
	if t.ClientLogPath == "" {
		return "logs/client-events.jsonl"
	}
	return t.ClientLogPath
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	expandEnv(&root)

	var cfg Config
	if err := root.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", path, err)
	}
	cfg.repair()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	if !validIdentifier(c.MySQL.Database) {
		return fmt.Errorf("invalid mysql database name")
	}
	if c.Redis.Mode == "cluster" && c.Redis.DB != 0 {
		return fmt.Errorf("Redis Cluster requires database 0")
	}
	if c.Redis.Mode != "" && c.Redis.Mode != "standalone" && c.Redis.Mode != "sentinel" && c.Redis.Mode != "cluster" {
		return fmt.Errorf("invalid Redis mode")
	}
	if c.Redis.Mode == "sentinel" && (len(c.Redis.Addresses) == 0 || c.Redis.MasterName == "") {
		return fmt.Errorf("Sentinel requires addresses and master_name")
	}
	if (c.Cache.EtcdEndpoints == "") != (c.Cache.CacheServerAddr == "") {
		return fmt.Errorf("report cache requires both etcd_endpoints and cache_server_addr")
	}
	if c.Executor.TimeoutSeconds <= 0 || c.Executor.HandleTimeoutSeconds <= c.Executor.TimeoutSeconds || c.Scheduler.StuckRunningMinutes*60 <= c.Executor.TimeoutSeconds {
		return fmt.Errorf("executor timeout must be positive and shorter than MQ handle and stuck-running timeouts")
	}
	if c.Scheduler.MigrateTickSeconds <= 0 || c.Scheduler.MigrateStepMinutes <= 0 || c.LLM.MaxToolRounds <= 0 || c.LLM.MaxResultRows <= 0 {
		return fmt.Errorf("scheduler and agent budgets must be positive")
	}
	return nil
}

func validIdentifier(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func (c *Config) repair() {
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Server.Host == "" {
		c.Server.Host = "127.0.0.1"
	}
	if c.Server.ReadTimeoutS == 0 {
		c.Server.ReadTimeoutS = 30
	}
	if c.Server.WriteTimeoutS == 0 {
		c.Server.WriteTimeoutS = 600
	}
	if c.Node.ID == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "node"
		}
		c.Node.ID = fmt.Sprintf("%s-%d", host, os.Getpid())
	}
	if c.MySQL.Host == "" {
		c.MySQL.Host = "127.0.0.1"
	}
	if c.MySQL.Port == 0 {
		c.MySQL.Port = 3306
	}
	if c.MySQL.User == "" {
		c.MySQL.User = "root"
	}
	if c.MySQL.PasswordEnv == "" {
		c.MySQL.PasswordEnv = "MYSQL_PASSWORD"
	}
	if c.MySQL.Database == "" {
		c.MySQL.Database = "taskflow"
	}
	if c.MySQL.MaxOpenConns == 0 {
		c.MySQL.MaxOpenConns = 20
	}
	if c.MySQL.MaxIdleConns == 0 {
		c.MySQL.MaxIdleConns = 5
	}
	if c.Redis.Address == "" {
		c.Redis.Address = "127.0.0.1:6379"
	}
	if c.LLM.BaseURLEnv == "" {
		c.LLM.BaseURLEnv = "OPENAI_BASE_URL"
	}
	if c.LLM.APIKeyEnv == "" {
		c.LLM.APIKeyEnv = "OPENAI_API_KEY"
	}
	if c.LLM.ModelEnv == "" {
		c.LLM.ModelEnv = "OPENAI_MODEL"
	}
	if c.LLM.MaxToolRounds == 0 {
		c.LLM.MaxToolRounds = 8
	}
	if c.LLM.RequestTimeoutSeconds == 0 {
		c.LLM.RequestTimeoutSeconds = 180
	}
	if c.LLM.MaxOutputTokens == 0 {
		c.LLM.MaxOutputTokens = 8192
	}
	if c.LLM.MaxResultRows == 0 {
		c.LLM.MaxResultRows = 100
	}
	if c.LLM.RedisKeyPrefix == "" {
		c.LLM.RedisKeyPrefix = "taskflow:tools:"
	}
	if c.Scheduler.MigrateStepMinutes == 0 {
		c.Scheduler.MigrateStepMinutes = 60
	}
	if c.Scheduler.MigrateTickSeconds == 0 {
		c.Scheduler.MigrateTickSeconds = 30
	}
	if c.Scheduler.ClaimTTLHours == 0 {
		c.Scheduler.ClaimTTLHours = 72
	}
	if c.Scheduler.StuckRunningMinutes == 0 {
		c.Scheduler.StuckRunningMinutes = 30
	}
	if c.Scheduler.OverdueRecoverMinutes == 0 {
		c.Scheduler.OverdueRecoverMinutes = 2
	}
	if c.Scheduler.RecoverWorkers <= 0 || c.Scheduler.RecoverWorkers > 64 {
		c.Scheduler.RecoverWorkers = 8
	}
	if c.MQ.ExecTopic == "" {
		c.MQ.ExecTopic = "taskflow.exec.commands"
	}
	if c.MQ.CondTopic == "" {
		c.MQ.CondTopic = "taskflow.cond.events"
	}
	if c.MQ.ExecGroup == "" {
		c.MQ.ExecGroup = "taskflow-exec"
	}
	if c.MQ.CondGroup == "" {
		c.MQ.CondGroup = "taskflow-cond"
	}
	if c.MQ.MsgQueueLen == 0 {
		c.MQ.MsgQueueLen = 5000
	}
	if c.MQ.MaxRetry == 0 {
		c.MQ.MaxRetry = 3
	}
	if c.Executor.Workers <= 0 || c.Executor.Workers > 64 {
		c.Executor.Workers = 4
	}
	if c.Executor.TimeoutSeconds == 0 {
		c.Executor.TimeoutSeconds = 600
	}
	if c.Executor.ReceiveTimeoutMs == 0 {
		c.Executor.ReceiveTimeoutMs = 1000
	}
	if c.Executor.HandleTimeoutSeconds == 0 {
		c.Executor.HandleTimeoutSeconds = 900
	}
	if c.Cache.ReportGroup == "" {
		c.Cache.ReportGroup = "taskflow.reports"
	}
	if c.Cache.ReportCacheBytesMiB == 0 {
		c.Cache.ReportCacheBytesMiB = 64
	}
	if c.Cache.ReportExpireSeconds == 0 {
		c.Cache.ReportExpireSeconds = 600
	}
	if c.Cache.DefCacheExpireSeconds == 0 {
		c.Cache.DefCacheExpireSeconds = 60
	}
	if c.Reports.Dir == "" {
		c.Reports.Dir = "./reports"
	}
	if c.Journal.Dir == "" {
		c.Journal.Dir = "./data/journal"
	}
	if c.Consensus.ID == 0 {
		c.Consensus.ID = 1
	}
	if c.Sentry.DSNEnv == "" {
		c.Sentry.DSNEnv = "SENTRY_DSN"
	}
	if c.Telemetry.SampleRate <= 0 || c.Telemetry.SampleRate > 1 {
		c.Telemetry.SampleRate = 1
	}
}

func expandEnv(node *yaml.Node) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode, yaml.MappingNode:
		for _, child := range node.Content {
			expandEnv(child)
		}
	case yaml.ScalarNode:
		if node.Tag == "!!str" || node.Tag == "" {
			node.Value = os.Expand(node.Value, os.Getenv)
		}
	}
}
