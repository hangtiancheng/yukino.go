package redis

const (
	DefaultIdleTimeoutSeconds = 10
	DefaultMaxActive          = 100
	DefaultMaxIdle            = 20
)

type ClientOptions struct {
	maxIdle            int
	idleTimeoutSeconds int
	maxActive          int
	wait               bool
	network            string
	address            string
	password           string
}

type ClientOption func(c *ClientOptions)

func WithMaxIdle(maxIdle int) ClientOption {
	return func(c *ClientOptions) {
		c.maxIdle = maxIdle
	}
}

func WithIdleTimeoutSeconds(idleTimeoutSeconds int) ClientOption {
	return func(c *ClientOptions) {
		c.idleTimeoutSeconds = idleTimeoutSeconds
	}
}

func WithMaxActive(maxActive int) ClientOption {
	return func(c *ClientOptions) {
		c.maxActive = maxActive
	}
}

func WithWaitMode() ClientOption {
	return func(c *ClientOptions) {
		c.wait = true
	}
}

func repairClient(c *ClientOptions) {
	if c.maxIdle < 0 {
		c.maxIdle = DefaultMaxIdle
	}

	if c.idleTimeoutSeconds < 0 {
		c.idleTimeoutSeconds = DefaultIdleTimeoutSeconds
	}

	if c.maxActive < 0 {
		c.maxActive = DefaultMaxActive
	}
}
