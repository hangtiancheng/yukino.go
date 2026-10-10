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
		opt(s.opts)
	}

	repair(s.opts)
	return &s
}

func (s *Service) Put(ctx context.Context, obj Object) error {
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

	if err := s.cache.Del(ctx, obj.Key()); err != nil {
		return err
	}

	return s.db.Put(ctx, obj)
}

func (s *Service) Get(ctx context.Context, obj Object) (useCache bool, err error) {
	v, err := s.cache.Get(ctx, obj.Key())
	if err != nil && !errors.Is(err, ErrorCacheMiss) {
		return false, err
	}

	if err == nil {
		if v == NullData {
			return true, ErrorDataNotExist
		}
		return true, obj.Read(v)
	}

	if err = s.db.Get(ctx, obj); err != nil && !errors.Is(err, ErrorDBMiss) {
		return false, err
	}

	if errors.Is(err, ErrorDBMiss) {
		if ok, err := s.cache.PutWhenEnable(ctx, obj.Key(), NullData, s.opts.CacheExpireSeconds()); err != nil {
			s.opts.logger.Errorf("put null data into cache fail, key: %s, err: %v", obj.Key(), err)
		} else {
			s.opts.logger.Infof("put null data into cache resp, key: %s, ok: %t", obj.Key(), ok)
		}

		return false, ErrorDataNotExist
	}

	v, err = obj.Write()
	if err != nil {
		return false, err
	}
	if ok, err := s.cache.PutWhenEnable(ctx, obj.Key(), v, s.opts.CacheExpireSeconds()); err != nil {
		s.opts.logger.Errorf("put data into cache fail, key: %s, data: %v, err: %v", obj.Key(), v, err)
	} else {
		s.opts.logger.Infof("put data into cache resp, key: %s, v: %v, ok: %t", obj.Key(), v, ok)
	}

	return false, nil
}
