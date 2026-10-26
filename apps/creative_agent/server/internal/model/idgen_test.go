package model_test

import (
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

func TestNextID_GeneratesUniqueIDs(t *testing.T) {
	testx.NoError(t, model.InitSnowflake(1))

	ids := make(map[int64]bool)
	for i := 0; i < 100; i++ {
		id := model.NextID()
		testx.False(t, ids[id], "duplicate ID %d", id)
		ids[id] = true
		testx.Greater(t, id, int64(0), "ID must be positive")
	}
}

func TestNextIDString_ReturnsNonEmpty(t *testing.T) {
	s := model.NextIDString()
	testx.NotEmpty(t, s)
	testx.Greater(t, len(s), 0)
}
