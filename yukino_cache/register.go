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
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	client_v3 "go.etcd.io/etcd/client/v3"
)

type RegisterConfig struct {
	Endpoints   []string
	DialTimeout time.Duration
}

var DefaultRegisterConfig = &RegisterConfig{
	Endpoints:   []string{"localhost:2379"},
	DialTimeout: 5 * time.Second,
}

// Register registers svcName/addr into etcd using DefaultRegisterConfig and
// keeps the lease alive until stopCh is closed.
func Register(svcName, addr string, stopCh <-chan error) error {
	return registerWithConfig(svcName, addr, stopCh, DefaultRegisterConfig)
}

func registerWithConfig(svcName, addr string, stopCh <-chan error, cfg *RegisterConfig) error {
	if addr == "" {
		return errors.New("empty address")
	}

	cli, err := client_v3.New(client_v3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: cfg.DialTimeout,
	})
	if err != nil {
		return fmt.Errorf("failed to create etcd client: %v", err)
	}

	if addr[0] == ':' {
		localIP, err := getLocalIP()
		if err != nil {
			cli.Close()
			return fmt.Errorf("failed to get local IP: %v", err)
		}
		addr = fmt.Sprintf("%s%s", localIP, addr)
	}

	keepAliveCh, leaseID, err := registerLease(cli, svcName, addr)
	if err != nil {
		cli.Close()
		return err
	}

	go func() {
		defer cli.Close()
		for {
			select {
			case <-stopCh:
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				cli.Revoke(ctx, leaseID)
				cancel()
				return
			case _, ok := <-keepAliveCh:
				if !ok {
					// Lease renewal broke (etcd hiccup, network partition).
					// Re-register with backoff so the node rejoins the cluster.
					log.Print("[YukinoCache] keep alive channel closed; re-registering")
					var err error
					keepAliveCh, leaseID, err = reRegister(cli, svcName, addr, stopCh)
					if err != nil {
						return
					}
				}
			}
		}
	}()

	log.Printf("Service registered: %s at %s", svcName, addr)
	return nil
}

func registerLease(cli *client_v3.Client, svcName, addr string) (<-chan *client_v3.LeaseKeepAliveResponse, client_v3.LeaseID, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lease, err := cli.Grant(ctx, 10)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create lease: %v", err)
	}

	key := fmt.Sprintf("/services/%s/%s", svcName, addr)
	if _, err = cli.Put(ctx, key, addr, client_v3.WithLease(lease.ID)); err != nil {
		return nil, 0, fmt.Errorf("failed to put key-value to etcd: %v", err)
	}

	keepAliveCh, err := cli.KeepAlive(context.Background(), lease.ID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to keep lease alive: %v", err)
	}
	return keepAliveCh, lease.ID, nil
}

func reRegister(cli *client_v3.Client, svcName, addr string, stopCh <-chan error) (<-chan *client_v3.LeaseKeepAliveResponse, client_v3.LeaseID, error) {
	backoff := time.Second
	for {
		select {
		case <-stopCh:
			return nil, 0, errors.New("registration stopped")
		case <-time.After(backoff):
		}

		keepAliveCh, leaseID, err := registerLease(cli, svcName, addr)
		if err == nil {
			log.Printf("Service re-registered: %s at %s", svcName, addr)
			return keepAliveCh, leaseID, nil
		}
		log.Printf("[YukinoCache] re-registration failed: %v", err)

		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func getLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String(), nil
			}
		}
	}

	return "", fmt.Errorf("no valid local IP found")
}
