package consistent_cache

import (
	"context"
	"errors"
)

var (
	ErrorDataNotExist = errors.New("data not exist")
	ErrorCacheMiss    = errors.New("cache miss")
	ErrorDBMiss       = errors.New("db miss")
)

const NullData = "Err_Syntax_Null_Data"

type Cache interface {
	Enable(ctx context.Context, key string, delayMillis int64) error
	Disable(ctx context.Context, key string, expireSeconds int64) error
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, key string) error
	PutWhenEnable(ctx context.Context, key, value string, expireSeconds int64) (bool, error)
}

type DB interface {
	Put(ctx context.Context, obj Object) error
	Get(ctx context.Context, obj Object) error
}

type Object interface {
	KeyColumn() string
	Key() string

	Write() (string, error)
	Read(body string) error
}

type Logger interface {
	Errorf(format string, v ...any)
	Warnf(format string, v ...any)
	Infof(format string, v ...any)
	Debugf(format string, v ...any)
}
