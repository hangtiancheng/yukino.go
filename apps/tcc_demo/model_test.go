package tcc_demo

import (
	"testing"
	"time"
)

func Test_transaction_getStatus(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name   string
		tx     *Transaction
		before time.Time
		want   TXStatus
	}{
		{
			name: "all_components_successful",
			tx: &Transaction{
				CreatedAt: now,
				Components: []*ComponentTryEntity{
					{TryStatus: TrySuccessful},
					{TryStatus: TrySuccessful},
				},
			},
			before: now.Add(-time.Second),
			want:   TXSuccessful,
		},
		{
			name: "any_component_failure",
			tx: &Transaction{
				CreatedAt: now,
				Components: []*ComponentTryEntity{
					{TryStatus: TrySuccessful},
					{TryStatus: TryFailure},
				},
			},
			before: now.Add(-time.Second),
			want:   TXFailure,
		},
		{
			name: "hanging_and_fresh",
			tx: &Transaction{
				CreatedAt: now,
				Components: []*ComponentTryEntity{
					{TryStatus: TrySuccessful},
					{TryStatus: TryHanging},
				},
			},
			before: now.Add(-time.Second),
			want:   TXHanging,
		},
		{
			name: "hanging_and_timeout",
			tx: &Transaction{
				CreatedAt: now.Add(-time.Minute),
				Components: []*ComponentTryEntity{
					{TryStatus: TrySuccessful},
					{TryStatus: TryHanging},
				},
			},
			before: now,
			want:   TXFailure,
		},
		{
			name:   "no_components",
			tx:     &Transaction{CreatedAt: now},
			before: now.Add(-time.Second),
			want:   TXSuccessful,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tx.getStatus(tt.before); got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
		})
	}
}
