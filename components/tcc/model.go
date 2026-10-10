package tcc

import (
	"time"
)

type RequestEntity struct {
	ComponentID string         `json:"componentName"`
	Request     map[string]any `json:"request"`
}

type ComponentEntities []*ComponentEntity

func (c ComponentEntities) ToComponents() []TCCComponent {
	components := make([]TCCComponent, 0, len(c))
	for _, entity := range c {
		components = append(components, entity.Component)
	}
	return components
}

type ComponentEntity struct {
	Request   map[string]any
	Component TCCComponent
}

type TXStatus string

const (
	TXHanging    TXStatus = "hanging"
	TXSuccessful TXStatus = "successful"
	TXFailure    TXStatus = "failure"
)

func (t TXStatus) String() string {
	return string(t)
}

type ComponentTryStatus string

func (c ComponentTryStatus) String() string {
	return string(c)
}

const (
	TryHanging    ComponentTryStatus = "hanging"
	TrySuccessful ComponentTryStatus = "successful"
	TryFailure    ComponentTryStatus = "failure"
)

type ComponentTryEntity struct {
	ComponentID string
	TryStatus   ComponentTryStatus
}

type Transaction struct {
	TXID       string `json:"txID"`
	Components []*ComponentTryEntity
	Status     TXStatus  `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (t *Transaction) getStatus(createdBefore time.Time) TXStatus {
	var hangingExist bool
	for _, component := range t.Components {
		if component.TryStatus == TryFailure {
			return TXFailure
		}
		hangingExist = hangingExist || (component.TryStatus != TrySuccessful)
	}

	if hangingExist && t.CreatedAt.Before(createdBefore) {
		return TXFailure
	}

	if hangingExist {
		return TXHanging
	}

	return TXSuccessful
}
