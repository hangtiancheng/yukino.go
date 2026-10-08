package conf

// RedisConfig holds cache configuration.
type RedisConfig struct {
	Network            string `yaml:"network"`
	Address            string `yaml:"address"`
	Password           string `yaml:"password"`
	MaxIdle            int    `yaml:"maxIdle"`
	IdleTimeoutSeconds int    `yaml:"idleTimeout"`
	// Maximum number of active connections in the pool.
	MaxActive int `yaml:"maxActive"`
	// Whether new requests wait or fail immediately when the pool is full.
	Wait bool `yaml:"wait"`
}

type RedisConfigProvider struct {
	conf *RedisConfig
}

func NewRedisConfigProvider(conf *RedisConfig) *RedisConfigProvider {
	return &RedisConfigProvider{
		conf: conf,
	}
}

func (r *RedisConfigProvider) Get() *RedisConfig {
	return r.conf
}

var defaultRedisConfProvider *RedisConfigProvider

func DefaultRedisConfigProvider() *RedisConfigProvider {
	return defaultRedisConfProvider
}
