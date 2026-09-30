package consistent_hash

type ConsistentHashOptions struct {
	lockExpireSeconds int
	replicas          int
}

type ConsistentHashOption func(opts *ConsistentHashOptions)

func WithLockExpireSeconds(seconds int) ConsistentHashOption {
	return func(opts *ConsistentHashOptions) {
		opts.lockExpireSeconds = seconds
	}
}

func WithReplicas(replicas int) ConsistentHashOption {
	return func(opts *ConsistentHashOptions) {
		opts.replicas = replicas
	}
}

func repair(opts *ConsistentHashOptions) {
	// Unset means no timeout.
	if opts.lockExpireSeconds <= 0 {
		opts.lockExpireSeconds = 15
	}

	if opts.replicas <= 0 {
		opts.replicas = 5
	}
}
