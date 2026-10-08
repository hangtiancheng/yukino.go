package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/dao"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/idem"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/telemetry"
	"github.com/hangtiancheng/yukino.go/components/red_mq"
	"github.com/hangtiancheng/yukino.go/components/tcc"
)

const (
	ComponentExecutionReserve = "execution_reserve"
	ComponentMQDispatch       = "mq_dispatch"
)

// ExecCommand is the payload published on the execution topic. TxID binds the
// message to one dispatch transaction: the executor only runs a command whose
// transaction still owns the execution row.
type ExecCommand struct {
	ExecutionID  uint              `json:"execution_id"`
	TxID         string            `json:"tx_id"`
	FireKey      string            `json:"fire_key"`
	TraceCarrier map[string]string `json:"trace_carrier,omitempty"`
}

// ExecutionReserveComponent is TCC participant 1: it flips the execution row
// from pending to reserved (Try), to queued (Confirm), or rolls it back to
// pending and releases the redis idempotency claim (Cancel).
type ExecutionReserveComponent struct {
	dao  *dao.DAO
	idem *idem.Service
}

func NewExecutionReserveComponent(d *dao.DAO, i *idem.Service) *ExecutionReserveComponent {
	return &ExecutionReserveComponent{dao: d, idem: i}
}

func (c *ExecutionReserveComponent) ID() string { return ComponentExecutionReserve }

func (c *ExecutionReserveComponent) Try(ctx context.Context, req *tcc.TCCReq) (*tcc.TCCResp, error) {
	execID, err := uintFromData(req.Data, "execution_id")
	if err != nil {
		return nil, err
	}

	affected, err := c.dao.Transition(ctx, execID, []string{po.StatusPending}, po.StatusReserved, map[string]any{
		"tx_id":      req.TXID,
		"claimed_at": time.Now(),
	})
	if err != nil {
		return nil, err
	}
	if affected == 1 {
		return c.ack(req), nil
	}

	// Not affected: either our own earlier Try (crash before TXUpdate) or
	// another transaction owns the row.
	exec, err := c.dao.GetExecution(ctx, execID)
	if err != nil {
		return nil, err
	}
	if exec.TxID == req.TXID && exec.Status != po.StatusPending && exec.Status != po.StatusCancelled {
		return c.ack(req), nil
	}
	slog.Warn("execution reserve rejected, owned by another dispatch",
		"execution_id", execID, "tx_id", req.TXID, "owner_tx", exec.TxID, "status", exec.Status)
	return &tcc.TCCResp{ComponentID: c.ID(), TXID: req.TXID, ACK: false}, nil
}

func (c *ExecutionReserveComponent) Confirm(ctx context.Context, txID string) (*tcc.TCCResp, error) {
	execID, err := c.execIDForTX(ctx, txID)
	if err != nil {
		return nil, err
	}
	if execID == 0 {
		return &tcc.TCCResp{ComponentID: c.ID(), TXID: txID, ACK: true}, nil
	}

	affected, err := c.dao.TransitionOwned(ctx, execID, txID, []string{po.StatusReserved}, po.StatusQueued, map[string]any{})
	if err != nil {
		return nil, err
	}
	if affected == 1 {
		return &tcc.TCCResp{ComponentID: c.ID(), TXID: txID, ACK: true}, nil
	}

	exec, err := c.dao.GetExecution(ctx, execID)
	if err != nil {
		return nil, err
	}
	switch {
	case exec.TxID == txID && (exec.Status == po.StatusQueued || exec.Status == po.StatusRunning ||
		exec.Status == po.StatusSucceeded || exec.Status == po.StatusFailed):
		return &tcc.TCCResp{ComponentID: c.ID(), TXID: txID, ACK: true}, nil
	default:
		return nil, fmt.Errorf("confirm failed: execution %d in status %s (tx %s)", execID, exec.Status, txID)
	}
}

func (c *ExecutionReserveComponent) Cancel(ctx context.Context, txID string) (*tcc.TCCResp, error) {
	execID, err := c.execIDForTX(ctx, txID)
	if err != nil {
		return nil, err
	}
	if execID == 0 {
		return &tcc.TCCResp{ComponentID: c.ID(), TXID: txID, ACK: true}, nil
	}

	exec, err := c.dao.GetExecution(ctx, execID)
	if err != nil {
		return nil, err
	}
	if exec.TxID != txID {
		// Reservation belongs to a different transaction; nothing to release.
		return &tcc.TCCResp{ComponentID: c.ID(), TXID: txID, ACK: true}, nil
	}
	if exec.Status != po.StatusReserved {
		return &tcc.TCCResp{ComponentID: c.ID(), TXID: txID, ACK: true}, nil
	}

	affected, err := c.dao.TransitionOwned(ctx, execID, txID, []string{po.StatusReserved}, po.StatusPending, map[string]any{
		"tx_id":      "",
		"claimed_at": nil,
	})
	if err != nil {
		return nil, err
	}
	if affected == 1 {
		// Release the redis fast-path claim so a recovery pass can re-dispatch.
		if err := c.idem.ReleaseFire(ctx, exec.FireKey); err != nil {
			slog.Warn("release fire claim on cancel failed", "fire_key", exec.FireKey, "err", err)
		}
	}
	return &tcc.TCCResp{ComponentID: c.ID(), TXID: txID, ACK: true}, nil
}

func (c *ExecutionReserveComponent) execIDForTX(ctx context.Context, txID string) (uint, error) {
	var exec po.Execution
	err := c.dao.DB().WithContext(ctx).
		Select("id", "status", "tx_id", "fire_key").
		Where("tx_id = ?", txID).Order("id DESC").First(&exec).Error
	if err != nil {
		if dao.IsNotFound(err) {
			return 0, nil
		}
		return 0, err
	}
	return exec.ID, nil
}

func (c *ExecutionReserveComponent) ack(req *tcc.TCCReq) *tcc.TCCResp {
	return &tcc.TCCResp{ComponentID: c.ID(), TXID: req.TXID, ACK: true}
}

// MQDispatchComponent is TCC participant 2: Try publishes the execution
// command to red_mq. Confirm is a no-op (the stream entry is durable), and
// Cancel is a no-op because the executor validates ownership by tx id and
// execution status before running anything.
type MQDispatchComponent struct {
	producer *red_mq.Producer
	topic    string
}

func NewMQDispatchComponent(producer *red_mq.Producer, topic string) *MQDispatchComponent {
	return &MQDispatchComponent{producer: producer, topic: topic}
}

func (c *MQDispatchComponent) ID() string { return ComponentMQDispatch }

func (c *MQDispatchComponent) Try(ctx context.Context, req *tcc.TCCReq) (*tcc.TCCResp, error) {
	execID, err := uintFromData(req.Data, "execution_id")
	if err != nil {
		return nil, err
	}
	fireKey, _ := req.Data["fire_key"].(string)

	cmd := ExecCommand{
		ExecutionID:  execID,
		TxID:         req.TXID,
		FireKey:      fireKey,
		TraceCarrier: telemetry.Inject(ctx),
	}
	body, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	if _, err := c.producer.SendMsg(ctx, c.topic, fireKey, string(body)); err != nil {
		return nil, fmt.Errorf("publish exec command: %w", err)
	}
	return &tcc.TCCResp{ComponentID: c.ID(), TXID: req.TXID, ACK: true}, nil
}

func (c *MQDispatchComponent) Confirm(ctx context.Context, txID string) (*tcc.TCCResp, error) {
	return &tcc.TCCResp{ComponentID: c.ID(), TXID: txID, ACK: true}, nil
}

func (c *MQDispatchComponent) Cancel(ctx context.Context, txID string) (*tcc.TCCResp, error) {
	return &tcc.TCCResp{ComponentID: c.ID(), TXID: txID, ACK: true}, nil
}

func uintFromData(data map[string]any, key string) (uint, error) {
	switch v := data[key].(type) {
	case uint:
		return v, nil
	case uint64:
		return uint(v), nil
	case int:
		return uint(v), nil
	case int64:
		return uint(v), nil
	case float64:
		return uint(v), nil
	case string:
		parsed, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid %s: %w", key, err)
		}
		return uint(parsed), nil
	default:
		return 0, fmt.Errorf("missing %s in tcc request data", key)
	}
}
