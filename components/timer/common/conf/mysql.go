package conf

// MySQLConfig holds database configuration.
type MySQLConfig struct {
	DSN string `yaml:"dsn"`
	// Maximum number of open connections
	MaxOpenConns int `yaml:"maxOpenConns"`
	// Maximum number of idle connections
	MaxIdleConns int `yaml:"maxIdleConns"`
}

type MysqlConfProvider struct {
	conf *MySQLConfig
}

func NewMysqlConfProvider(conf *MySQLConfig) *MysqlConfProvider {
	return &MysqlConfProvider{
		conf: conf,
	}
}

func (m *MysqlConfProvider) Get() *MySQLConfig {
	return m.conf
}

var defaultMysqlConfProvider *MysqlConfProvider

func DefaultMysqlConfProvider() *MysqlConfProvider {
	return defaultMysqlConfProvider
}
