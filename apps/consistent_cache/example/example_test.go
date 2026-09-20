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

package example

import (
	"context"
	"errors"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/consistent_cache"
	"github.com/hangtiancheng/yukino.go/apps/consistent_cache/mysql"
	"github.com/hangtiancheng/yukino.go/apps/consistent_cache/redis"
)

const (
	redisAddress  = "please fill in redis address"
	redisPassword = "please fill in redis password"

	mysqlDSN = "please fill in mysql dsn"
)

// requireInfra skips the test while the redis/mysql endpoints are unconfigured placeholders.
func requireInfra(t *testing.T) {
	t.Helper()
	if strings.Contains(redisAddress, "please fill in") || strings.Contains(mysqlDSN, "please fill in") {
		t.Skip("redis/mysql endpoints not configured; fill in redisAddress/mysqlDSN to run this integration test")
	}
}

func newService() *consistent_cache.Service {
	cache := redis.NewRedisCache(&redis.Config{
		Address:  redisAddress,
		Password: redisPassword,
	})
	db := mysql.NewDB(mysqlDSN)
	return consistent_cache.NewService(cache, db,
		consistent_cache.WithCacheExpireSeconds(120),
		consistent_cache.WithDisableExpireSeconds(1),
	)
}

func Test_consistent_Cache(t *testing.T) {
	requireInfra(t)
	service := consistent_cache.NewService(
		redis.NewRedisCache(&redis.Config{
			Address:  redisAddress,
			Password: redisPassword,
		}),
		mysql.NewDB(mysqlDSN),
		consistent_cache.WithCacheExpireSeconds(120),
		consistent_cache.WithCacheExpireRandomMode(),
		consistent_cache.WithDisableExpireSeconds(1),
	)
	ctx := context.Background()
	exp := Example{
		Key_: "test",
		Data: "test",
	}
	if err := service.Put(ctx, &exp); err != nil {
		t.Error(err)
		return
	}

	expReceiver := Example{
		Key_: "test",
	}
	if _, err := service.Get(ctx, &expReceiver); err != nil {
		t.Error(err)
		return
	}

	t.Logf("read data: %s, ", expReceiver.Data)
}

// Verifies: 1) data correctness 2) cache hit ratio.
func Test_Consistent_Cache_Correct(t *testing.T) {
	requireInfra(t)
	service := newService()
	ctx := context.Background()

	// 100 concurrent writers, with a local backup of every written record.
	prefix := time.Now().String() + "-"
	dataChan := make(chan *Example)
	go func() {
		var wg sync.WaitGroup
		for range 100 {
			wg.Go(func() {
				k := prefix + strconv.Itoa(rand.Intn(100))
				v := prefix + strconv.Itoa(rand.Intn(100))
				data := Example{
					Key_: k,
					Data: v,
				}
				if err := service.Put(ctx, &data); err != nil {
					t.Error(err)
					return
				}
				dataChan <- &data
			})
		}
		wg.Wait()
		close(dataChan)
	}()

	// Collect written data into a local backup map.
	mp := make(map[string]string, 500)
	for data := range dataChan {
		mp[data.Key_] = data.Data
	}

	// Wait for the write-path disable markers to expire.
	<-time.After(time.Second)

	var useCacheCnt int
	var expectUseCacheCnt int
	querySet := make(map[string]struct{}, 100)
	for range 100 {
		k := strconv.Itoa(rand.Intn(100))
		data := Example{
			Key_: prefix + k,
		}
		if _, ok := querySet[prefix+k]; ok {
			expectUseCacheCnt++
		}
		querySet[prefix+k] = struct{}{}

		useCache, err := service.Get(ctx, &data)
		if err != nil && !errors.Is(err, consistent_cache.ErrorDataNotExist) {
			t.Error(err)
			continue
		}

		if useCache {
			useCacheCnt++
		}

		expect, ok := mp[data.Key_]
		if errors.Is(err, consistent_cache.ErrorDataNotExist) != !ok {
			t.Errorf("expected data-not-exist=%v, got %v", !ok, errors.Is(err, consistent_cache.ErrorDataNotExist))
		}
		if !ok {
			continue
		}

		if data.Data != expect {
			t.Errorf("expected data=%s, got %s", expect, data.Data)
		}
	}

	if useCacheCnt != expectUseCacheCnt {
		t.Errorf("expected useCacheCnt=%d, got %d", expectUseCacheCnt, useCacheCnt)
	}
}

// Concurrent read/write. Verifies: 1) disable mechanism works 2) read result correctness.
func Test_Consistent_Cache_Read_Write(t *testing.T) {
	requireInfra(t)
	service := newService()

	ctx := context.Background()

	prefix := time.Now().String()

	var wg sync.WaitGroup
	dataChan := make(chan *Example)

	// Value range for writers.
	startV, endV := 1, 5
	// Multiple writers target the same key with values in [startV, endV].
	go func() {
		for i := startV; i <= endV; i++ {
			i := i // shadow
			wg.Go(func() {
				k := prefix
				v := prefix + strconv.Itoa(i)
				data := Example{
					Key_: k,
					Data: v,
				}
				if err := service.Put(ctx, &data); err != nil {
					t.Error(err)
				}
				dataChan <- &data
			})
		}
	}()

	// Double the readers targeting the same key.
	go func() {
		for i := 0; i < 10*(endV-startV+1); i++ {
			wg.Go(func() {
				data := Example{
					Key_: prefix,
				}
				useCache, err := service.Get(ctx, &data)
				if err != nil && !errors.Is(err, consistent_cache.ErrorDataNotExist) {
					t.Error(err)
					return
				}
				if errors.Is(err, consistent_cache.ErrorDataNotExist) {
					return
				}
				// During concurrent writes the cache is not expected to be used.
				if useCache {
					t.Errorf("expected useCache=false, got true")
				}
				gotData, err := strconv.Atoi(data.Data)
				if err != nil {
					t.Error(err)
					return
				}
				if gotData < startV || gotData > endV {
					t.Errorf("expected gotData in [%d, %d], got %d", startV, endV, gotData)
				}
			})
		}
	}()

	// Collect the written records.
	dataSlice := make([]*Example, 0, 5)
	for i := startV; i <= endV; i++ {
		data := <-dataChan
		dataSlice = append(dataSlice, data)
	}

	// After writes settle, read the final value.
	data := Example{
		Key_: prefix,
	}
	useCache, err := service.Get(ctx, &data)
	if err != nil {
		t.Error(err)
		return
	}
	if useCache {
		t.Errorf("expected useCache=false, got true")
	}
	if data.Data != dataSlice[len(dataSlice)-1].Data {
		t.Errorf("expected data=%s, got %s", dataSlice[len(dataSlice)-1].Data, data.Data)
	}

	wg.Wait()

	// One second later, read twice: first miss, second hit.
	<-time.After(time.Second)
	if useCache, err = service.Get(ctx, &data); err != nil {
		t.Error(err)
		return
	}
	if useCache {
		t.Errorf("expected useCache=false, got true")
	}

	if useCache, err = service.Get(ctx, &data); err != nil {
		t.Error(err)
		return
	}
	if !useCache {
		t.Errorf("expected useCache=true, got false")
	}
	if data.Data != dataSlice[len(dataSlice)-1].Data {
		t.Errorf("expected data=%s, got %s", dataSlice[len(dataSlice)-1].Data, data.Data)
	}
}
