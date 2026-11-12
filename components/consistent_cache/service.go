package consistent_cache

import (
	"context"
	"errors"
	"time"
)

type Service struct {
	opts  *Options
	cache Cache
	db    DB
}

func NewService(cache Cache, db DB, opts ...Option) *Service {
	s := Service{
		cache: cache,
		db:    db,
		opts:  &Options{},
	}

	for _, opt := range opts {
		if opt != nil {
			opt(s.opts)
		}
	}

	repair(s.opts)
	return &s
}

func (s *Service) Put(ctx context.Context, obj Object) error {
	if s == nil || s.opts == nil {
		return errors.New("cache service is not initialized")
	}
	if ctx == nil || obj == nil {
		return errors.New("context and object are required")
	}
	if s.cache == nil || s.db == nil {
		return errors.New("cache and database are required")
	}
	key := obj.Key()
	if key == "" {
		return errors.New("object key is empty")
	}
	if err := s.cache.Disable(ctx, key, s.opts.disableExpireSeconds); err != nil {
		return err
	}

	defer func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := s.cache.Enable(ctx, key, s.opts.enableDelayMillis); err != nil {
				s.opts.logger.Errorf("enable fail, key: %s, err: %v", key, err)
			}
		}()
	}()

	if err := s.cache.Del(ctx, key); err != nil {
		return err
	}

	return s.db.Put(ctx, obj)
}

func (s *Service) Get(ctx context.Context, obj Object) (useCache bool, err error) {
	if s == nil || s.opts == nil {
		return false, errors.New("cache service is not initialized")
	}
	if ctx == nil || obj == nil {
		return false, errors.New("context and object are required")
	}
	if s.cache == nil || s.db == nil {
		return false, errors.New("cache and database are required")
	}
	key := obj.Key()
	if key == "" {
		return false, errors.New("object key is empty")
	}
	v, err := s.cache.Get(ctx, key)
	if err != nil && !errors.Is(err, ErrorCacheMiss) {
		return false, err
	}

	if err == nil {
		if v == NullData {
			return true, ErrorDataNotExist
		}
		if v == NullData+"\x00" {
			v = NullData
		}
		return true, obj.Read(v)
	}

	if err = s.db.Get(ctx, obj); err != nil && !errors.Is(err, ErrorDBMiss) {
		return false, err
	}

	if errors.Is(err, ErrorDBMiss) {
		if ok, err := s.cache.PutWhenEnable(ctx, key, NullData, s.opts.CacheExpireSeconds()); err != nil {
			s.opts.logger.Errorf("put null data into cache fail, key: %s, err: %v", key, err)
		} else {
			s.opts.logger.Infof("put null data into cache resp, key: %s, ok: %t", key, ok)
		}

		return false, ErrorDataNotExist
	}

	v, err = obj.Write()
	if err != nil {
		return false, err
	}
	if v == NullData {
		v += "\x00"
	}
	if ok, err := s.cache.PutWhenEnable(ctx, key, v, s.opts.CacheExpireSeconds()); err != nil {
		s.opts.logger.Errorf("put data into cache fail, key: %s, data: %v, err: %v", key, v, err)
	} else {
		s.opts.logger.Infof("put data into cache resp, key: %s, v: %v, ok: %t", key, v, ok)
	}

	return false, nil
}
