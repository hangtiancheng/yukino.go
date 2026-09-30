package tcc_demo

import (
	"time"
)

type RequestEntity struct {
	// Component name
	ComponentID string `json:"componentName"`
	// Component request parameters
	Request map[string]any `json:"request"`
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

// Transaction status
type TXStatus string

const (
	// Transaction in progress
	TXHanging TXStatus = "hanging"
	// Transaction succeeded
	TXSuccessful TXStatus = "successful"
	// Transaction failed
	TXFailure TXStatus = "failure"
)

func (t TXStatus) String() string {
	return string(t)
}

type ComponentTryStatus string

func (c ComponentTryStatus) String() string {
	return string(c)
}

const (
	TryHanging ComponentTryStatus = "hanging"
	// Try succeeded
	TrySuccessful ComponentTryStatus = "successful"
	// Try failed
	TryFailure ComponentTryStatus = "failure"
)

type ComponentTryEntity struct {
	ComponentID string
	TryStatus   ComponentTryStatus
}

// Transaction
type Transaction struct {
	TXID       string `json:"txID"`
	Components []*ComponentTryEntity
	Status     TXStatus  `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (t *Transaction) getStatus(createdBefore time.Time) TXStatus {
	// 1. If any component failed, the transaction is failed
	var hangingExist bool
	for _, component := range t.Components {
		if component.TryStatus == TryFailure {
			return TXFailure
		}
		hangingExist = hangingExist || (component.TryStatus != TrySuccessful)
	}

	// 2. If any component is hanging and the transaction has timed out, mark as failed
	if hangingExist && t.CreatedAt.Before(createdBefore) {
		return TXFailure
	}

	// 3. If any component is still hanging, the transaction remains hanging
	if hangingExist {
		return TXHanging
	}

	// 4. All components succeeded
	return TXSuccessful
}
