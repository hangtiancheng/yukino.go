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

package registry

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	client_v3 "go.etcd.io/etcd/client/v3"
)

type Instance struct {
	Addr string
}

type Registry struct {
	client *client_v3.Client
	prefix string

	mu       sync.RWMutex
	services map[string]map[string]Instance // service -> addr -> Instance

	ctx    context.Context
	cancel context.CancelFunc
}

func NewRegistry(endpoints []string) (*Registry, error) {
	cli, err := client_v3.New(client_v3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Registry{
		client:   cli,
		prefix:   "/github.com/hangtiancheng/yukino.go/yukino_rpc/services/",
		services: make(map[string]map[string]Instance),
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

func (r *Registry) Register(service string, ins Instance, ttl int64) error {
	leaseResp, err := r.client.Grant(r.ctx, ttl)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("%s%s/%s", r.prefix, service, ins.Addr)

	_, err = r.client.Put(r.ctx, key, ins.Addr, client_v3.WithLease(leaseResp.ID))
	if err != nil {
		return err
	}

	ch, err := r.client.KeepAlive(r.ctx, leaseResp.ID)
	if err != nil {
		return err
	}

	go func() {
		for {
			_, ok := <-ch
			if !ok {
				return
			}
		}
	}()

	return nil
}

func (r *Registry) Discover(service string) ([]Instance, error) {
	r.mu.RLock()
	if _, ok := r.services[service]; ok {
		instances := r.copyInstances(service)
		r.mu.RUnlock()
		return instances, nil
	}
	r.mu.RUnlock()

	if err := r.initService(service); err != nil {
		return nil, err
	}

	return r.copyInstances(service), nil
}

func (r *Registry) initService(service string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.services[service]; ok {
		return nil
	}

	key := fmt.Sprintf("%s%s/", r.prefix, service)

	resp, err := r.client.Get(r.ctx, key, client_v3.WithPrefix())
	if err != nil {
		return err
	}

	r.services[service] = make(map[string]Instance)

	for _, kv := range resp.Kvs {
		addr := string(kv.Value)
		r.services[service][addr] = Instance{Addr: addr}
	}

	go r.watch(service)

	return nil
}

func (r *Registry) watch(service string) {
	key := fmt.Sprintf("%s%s/", r.prefix, service)

	for {
		select {
		case <-r.ctx.Done():
			return
		default:
		}
		watchChan := r.client.Watch(r.ctx, key, client_v3.WithPrefix())

		for watchResp := range watchChan {
			for _, event := range watchResp.Events {
				switch event.Type {

				case client_v3.EventTypePut:
					addr := string(event.Kv.Value)
					r.mu.Lock()
					r.services[service][addr] = Instance{Addr: addr}
					r.mu.Unlock()

				case client_v3.EventTypeDelete:
					deletedKey := string(event.Kv.Key)
					addr := strings.TrimPrefix(deletedKey, r.prefix+service+"/")
					r.mu.Lock()
					delete(r.services[service], addr)
					r.mu.Unlock()
				}
			}
		}

		select {
		case <-r.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (r *Registry) copyInstances(service string) []Instance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var instances []Instance
	for _, ins := range r.services[service] {
		instances = append(instances, ins)
	}
	return instances
}

func (r *Registry) Close() error {
	if r.cancel != nil {
		r.cancel()
	}
	if r.client == nil {
		return nil
	}
	return r.client.Close()
}
