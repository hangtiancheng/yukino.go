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

package yukino_cache

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	client_v3 "go.etcd.io/etcd/client/v3"
)

const defaultSvcName = "yukino_cache"

type PeerPicker interface {
	PickPeer(key string) (peer Peer, ok bool, self bool)
	Close() error
}

type Peer interface {
	Get(group string, key string) ([]byte, error)
	Set(ctx context.Context, group string, key string, value []byte) error
	Delete(group string, key string) (bool, error)
	Close() error
}

type ClientPicker struct {
	selfAddr string
	svcName  string
	mu       sync.RWMutex
	consHash *ConsistentHashMap
	clients  map[string]*Client
	etcdCli  *client_v3.Client
	ctx      context.Context
	cancel   context.CancelFunc
}

type PickerOption func(*ClientPicker)

func WithServiceName(name string) PickerOption {
	return func(p *ClientPicker) {
		p.svcName = name
	}
}

func (p *ClientPicker) PrintPeers() {
	p.mu.RLock()
	defer p.mu.RUnlock()

	log.Printf("Discovered peers:")
	for addr := range p.clients {
		log.Printf("- %s", addr)
	}
}

func NewClientPicker(addr string, opts ...PickerOption) (*ClientPicker, error) {
	// Normalize ":port" the same way Register does before writing to etcd,
	// otherwise the local node would treat its own registration as a remote peer.
	if strings.HasPrefix(addr, ":") {
		localIP, err := getLocalIP()
		if err != nil {
			return nil, fmt.Errorf("failed to resolve self address: %v", err)
		}
		addr = localIP + addr
	}

	ctx, cancel := context.WithCancel(context.Background())
	picker := &ClientPicker{
		selfAddr: addr,
		svcName:  defaultSvcName,
		clients:  make(map[string]*Client),
		consHash: NewConsistentHash(),
		ctx:      ctx,
		cancel:   cancel,
	}

	for _, opt := range opts {
		opt(picker)
	}

	cli, err := client_v3.New(client_v3.Config{
		Endpoints:   DefaultRegisterConfig.Endpoints,
		DialTimeout: DefaultRegisterConfig.DialTimeout,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create etcd client: %v", err)
	}
	picker.etcdCli = cli

	// The local node always owns part of the ring so that keys hashing to
	// this node are loaded locally instead of round-tripping through gRPC.
	picker.consHash.Add(addr)

	if err := picker.startServiceDiscovery(); err != nil {
		cancel()
		cli.Close()
		return nil, err
	}

	return picker, nil
}

func (p *ClientPicker) servicePrefix() string {
	return "/services/" + p.svcName + "/"
}

// addrFromEventKey extracts the peer address from an etcd key. DELETE events
// carry an empty value, so the key is the only reliable source.
func (p *ClientPicker) addrFromEventKey(key []byte) string {
	return strings.TrimPrefix(string(key), p.servicePrefix())
}

func (p *ClientPicker) startServiceDiscovery() error {
	if _, err := p.fetchAllServices(); err != nil {
		return err
	}

	go p.watchServiceChanges()
	return nil
}

func (p *ClientPicker) watchServiceChanges() {
	for {
		rev, err := p.fetchAllServices()
		if err != nil {
			log.Printf("[YukinoCache] failed to resync services: %v", err)
		}

		if p.watchOnce(rev) {
			return
		}

		select {
		case <-p.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// watchOnce watches the service prefix until the channel breaks.
// It returns true when the picker is closing.
func (p *ClientPicker) watchOnce(fromRev int64) bool {
	watcher := client_v3.NewWatcher(p.etcdCli)
	defer watcher.Close()

	watchOpts := []client_v3.OpOption{client_v3.WithPrefix()}
	if fromRev > 0 {
		watchOpts = append(watchOpts, client_v3.WithRev(fromRev+1))
	}
	watchChan := watcher.Watch(p.ctx, p.servicePrefix(), watchOpts...)

	for {
		select {
		case <-p.ctx.Done():
			return true
		case resp, ok := <-watchChan:
			if !ok || resp.Canceled {
				return false
			}
			if err := resp.Err(); err != nil {
				log.Printf("[YukinoCache] watch error: %v", err)
				return false
			}
			p.handleWatchEvents(resp.Events)
		}
	}
}

func (p *ClientPicker) handleWatchEvents(events []*client_v3.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, event := range events {
		addr := p.addrFromEventKey(event.Kv.Key)
		if addr == "" || addr == p.selfAddr {
			continue
		}

		switch event.Type {
		case client_v3.EventTypePut:
			if _, exists := p.clients[addr]; !exists {
				p.set(addr)
				log.Printf("New service discovered at %s", addr)
			}
		case client_v3.EventTypeDelete:
			if client, exists := p.clients[addr]; exists {
				client.Close()
				p.remove(addr)
				log.Printf("Service removed at %s", addr)
			}
		}
	}
}

// fetchAllServices reconciles the peer set with etcd and returns the
// revision the snapshot was taken at, so the watch can resume from it.
func (p *ClientPicker) fetchAllServices() (int64, error) {
	ctx, cancel := context.WithTimeout(p.ctx, 3*time.Second)
	defer cancel()

	resp, err := p.etcdCli.Get(ctx, p.servicePrefix(), client_v3.WithPrefix())
	if err != nil {
		return 0, fmt.Errorf("failed to get all services: %v", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	current := make(map[string]struct{}, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		addr := p.addrFromEventKey(kv.Key)
		if addr == "" || addr == p.selfAddr {
			continue
		}
		current[addr] = struct{}{}
		if _, exists := p.clients[addr]; !exists {
			p.set(addr)
			log.Printf("Discovered service at %s", addr)
		}
	}

	for addr, client := range p.clients {
		if _, ok := current[addr]; !ok {
			client.Close()
			p.remove(addr)
			log.Printf("Service removed at %s (resync)", addr)
		}
	}

	return resp.Header.Revision, nil
}

func (p *ClientPicker) set(addr string) {
	if client, err := NewClient(addr, p.svcName, p.etcdCli); err == nil {
		p.consHash.Add(addr)
		p.clients[addr] = client
		log.Printf("Successfully created client for %s", addr)
	} else {
		log.Printf("Failed to create client for %s: %v", addr, err)
	}
}

func (p *ClientPicker) remove(addr string) {
	p.consHash.Remove(addr)
	delete(p.clients, addr)
}

// PickPeer returns the peer owning key. When the local node owns the key it
// returns (nil, true, true) so callers load locally without a network hop.
func (p *ClientPicker) PickPeer(key string) (Peer, bool, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	addr := p.consHash.Get(key)
	if addr == "" {
		return nil, false, false
	}
	if addr == p.selfAddr {
		return nil, true, true
	}
	if client, ok := p.clients[addr]; ok {
		return client, true, false
	}
	return nil, false, false
}

func (p *ClientPicker) Close() error {
	p.cancel()
	p.mu.Lock()
	defer p.mu.Unlock()

	var errs []error
	for addr, client := range p.clients {
		if err := client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close client %s: %v", addr, err))
		}
	}

	if err := p.etcdCli.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close etcd client: %v", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors while closing: %v", errs)
	}
	return nil
}
