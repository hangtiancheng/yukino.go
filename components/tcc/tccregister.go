package tcc

import (
	"errors"
	"fmt"
	"sync"
)

type registryCenter struct {
	mux        sync.RWMutex
	components map[string]TCCComponent
}

func newRegistryCenter() *registryCenter {
	return &registryCenter{
		components: make(map[string]TCCComponent),
	}
}

func (r *registryCenter) register(component TCCComponent) error {
	if component == nil {
		return errors.New("nil tcc component")
	}
	componentID := component.ID()
	if componentID == "" {
		return errors.New("empty component id")
	}
	r.mux.Lock()
	defer r.mux.Unlock()
	if _, ok := r.components[componentID]; ok {
		return errors.New("repeat component id")
	}
	r.components[componentID] = component
	return nil
}

func (r *registryCenter) getComponents(componentIDs ...string) ([]TCCComponent, error) {
	components := make([]TCCComponent, 0, len(componentIDs))

	r.mux.RLock()
	defer r.mux.RUnlock()

	for _, componentID := range componentIDs {
		component, ok := r.components[componentID]
		if !ok {
			return nil, fmt.Errorf("component id: %s not existed", componentID)
		}
		components = append(components, component)
	}

	return components, nil
}
