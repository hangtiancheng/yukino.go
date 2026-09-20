// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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

// Cache abstracts the cache module.
type Cache interface {
	// Enable turns the read-path write cache on for a key (enabled by default).
	Enable(ctx context.Context, key string, delayMillis int64) error
	// Disable turns the read-path write cache off for a key.
	Disable(ctx context.Context, key string, expireSeconds int64) error
	// Get reads the cached value for the key.
	Get(ctx context.Context, key string) (string, error)
	// Del removes the cached value for the key.
	Del(ctx context.Context, key string) error
	// PutWhenEnable writes the cache only if the read-path write cache is enabled (enabled by default).
	PutWhenEnable(ctx context.Context, key, value string, expireSeconds int64) (bool, error)
}

// DB abstracts the database module.
type DB interface {
	// Put writes obj to the database.
	Put(ctx context.Context, obj Object) error
	// Get loads obj from the database by key.
	Get(ctx context.Context, obj Object) error
}

// Object represents a single record passed to read/write operations.
type Object interface {
	// KeyColumn returns the column name backing the key.
	KeyColumn() string
	// Key returns the value of the key column.
	Key() string

	// Write serializes the object to a string.
	Write() (string, error)
	// Read deserializes the string body back into the object.
	Read(body string) error
}

// Logger is the logging surface used by the cache service.
type Logger interface {
	Errorf(format string, v ...any)
	Warnf(format string, v ...any)
	Infof(format string, v ...any)
	Debugf(format string, v ...any)
}
