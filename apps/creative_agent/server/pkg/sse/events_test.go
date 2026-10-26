package sse

import (
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

func TestEventReconnectConstant(t *testing.T) {
	testx.Equal(t, "reconnect", EventReconnect)
}
