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
	"time"
)

// Service coordinates the cache and database to provide consistent reads and writes.
type Service struct {
	opts  *Options
	cache Cache
	db    DB
}

// NewService builds a Service. Both cache and db are supplied by the caller.
func NewService(cache Cache, db DB, opts ...Option) *Service {
	s := Service{
		cache: cache,
		db:    db,
		opts:  &Options{},
	}

	for _, opt := range opts {
		opt(s.opts)
	}

	repair(s.opts)
	return &s
}

// Put writes obj through the cache-consistency flow.
func (s *Service) Put(ctx context.Context, obj Object) error {
	// 1. Disable the read-path write cache for this key.
	if err := s.cache.Disable(ctx, obj.Key(), s.opts.disableExpireSeconds); err != nil {
		return err
	}

	defer func() {
		key := obj.Key()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := s.cache.Enable(ctx, key, s.opts.enableDelayMillis); err != nil {
				s.opts.logger.Errorf("enable fail, key: %s, err: %v", key, err)
			}
		}()
	}()

	// 2. Invalidate the cached value for this key.
	if err := s.cache.Del(ctx, obj.Key()); err != nil {
		return err
	}

	// 3. Persist to the database.
	return s.db.Put(ctx, obj)
}

// Get reads obj through the cache-consistency flow. useCache reports whether the value came from cache.
func (s *Service) Get(ctx context.Context, obj Object) (useCache bool, err error) {
	// 1. Try the cache first.
	v, err := s.cache.Get(ctx, obj.Key())
	// 2. Propagate non-cache-miss errors.
	if err != nil && !errors.Is(err, ErrorCacheMiss) {
		return false, err
	}

	// 3. Cache hit.
	if err == nil {
		// 3.1. NullData is the anti-penetration sentinel for missing rows.
		if v == NullData {
			return true, ErrorDataNotExist
		}
		// 3.2. Real data.
		return true, obj.Read(v)
	}

	// 4. Cache miss: fall back to the database.
	if err = s.db.Get(ctx, obj); err != nil && !errors.Is(err, ErrorDBMiss) {
		return false, err
	}

	// 5. Database miss: write NullData to the cache to prevent cache penetration.
	if errors.Is(err, ErrorDBMiss) {
		if ok, err := s.cache.PutWhenEnable(ctx, obj.Key(), NullData, s.opts.CacheExpireSeconds()); err != nil {
			s.opts.logger.Errorf("put null data into cache fail, key: %s, err: %v", obj.Key(), err)
		} else {
			s.opts.logger.Infof("put null data into cache resp, key: %s, ok: %t", obj.Key(), ok)
		}

		return false, ErrorDataNotExist
	}

	// 6. Database hit: backfill the cache.
	v, err = obj.Write()
	if err != nil {
		return false, err
	}
	if ok, err := s.cache.PutWhenEnable(ctx, obj.Key(), v, s.opts.CacheExpireSeconds()); err != nil {
		s.opts.logger.Errorf("put data into cache fail, key: %s, data: %v, err: %v", obj.Key(), v, err)
	} else {
		s.opts.logger.Infof("put data into cache resp, key: %s, v: %v, ok: %t", obj.Key(), v, ok)
	}

	// 7. Return the freshly loaded value.
	return false, nil
}
