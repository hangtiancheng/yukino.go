package engine

import (
	"fmt"
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/conf"
	"github.com/hangtiancheng/yukino.go/libs/yukino_cache"
)

// OpenReportPeers optionally enables the library's etcd-backed peer discovery.
// The caller closes its group before invoking cleanup.
func OpenReportPeers(cfg conf.CacheConf) (*yukino_cache.ClientPicker, func(), error) {
	if cfg.EtcdEndpoints == "" {
		return nil, func() {}, nil
	}
	endpoints := strings.Split(cfg.EtcdEndpoints, ",")
	for i := range endpoints {
		endpoints[i] = strings.TrimSpace(endpoints[i])
	}
	// The library exposes endpoint configuration for pickers as a process-wide
	// startup setting. Set it before creating any picker or background work.
	yukino_cache.DefaultRegisterConfig.Endpoints = endpoints
	server, err := yukino_cache.NewServer(cfg.CacheServerAddr, "taskflow.reports", yukino_cache.WithEtcdEndpoints(endpoints))
	if err != nil {
		return nil, nil, err
	}
	done := make(chan error, 1)
	go func() { done <- server.Start() }()
	picker, err := yukino_cache.NewClientPicker(cfg.CacheServerAddr, yukino_cache.WithServiceName("taskflow.reports"))
	if err != nil {
		server.Stop()
		<-done
		return nil, nil, err
	}
	select {
	case err := <-done:
		picker.Close()
		server.Stop()
		return nil, nil, fmt.Errorf("cache server: %w", err)
	default:
	}
	return picker, func() { picker.Close(); server.Stop(); <-done }, nil
}
