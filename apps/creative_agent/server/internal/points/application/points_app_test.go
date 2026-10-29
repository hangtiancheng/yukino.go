package application_test

import (
	"context"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/application"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/domain"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// fakeRepo is a minimal implementation for verifying the orchestration logic.
type fakeRepo struct {
	balance int
	err     error
}

func (f *fakeRepo) GetBalance(ctx context.Context, userID int64) (int, error) {
	return f.balance, f.err
}
func (f *fakeRepo) InsertConsume(ctx context.Context, flow *domain.PointsFlow) (uint64, error) {
	flow.FlowType = domain.FlowTypeConsume
	return 1, nil
}
func (f *fakeRepo) InsertRefund(ctx context.Context, flow *domain.PointsFlow) (uint64, error) {
	flow.FlowType = domain.FlowTypeRefund
	return 1, nil
}
func (f *fakeRepo) RefundByOrderID(ctx context.Context, orderID string, userID int64, title string) error {
	return nil
}
func (f *fakeRepo) ListByProject(ctx context.Context, projectID string) ([]*domain.PointsFlow, error) {
	return nil, nil
}

func TestDeductSetsConsumeFlow(t *testing.T) {
	r := &recordingRepo{}
	app := application.NewPointsApp(r)
	id, err := app.Deduct(context.Background(), 1, "p1", "p1_1", "mix_video", 10)
	testx.NoError(t, err)
	testx.Equal(t, uint64(1), id)
	testx.Equal(t, int64(1), r.flow.UserID)
	testx.Equal(t, "p1", r.flow.ProjectID)
	testx.Equal(t, "p1_1", r.flow.OrderID)
	testx.Equal(t, 10, r.flow.Amount)
	testx.Equal(t, "度加AI-AI成片", r.flow.SceneDesc)
}

func TestConsumeTitle(t *testing.T) {
	testx.Equal(t, "度加AI-AI成片", domain.ConsumeTitle("mix_video"))
	testx.Equal(t, "度加AI-AI成片失败退回", domain.BackTitle("mix_video"))
	testx.Equal(t, "度加AI-AI成片", domain.ConsumeTitle("unknown_type"))
	testx.Equal(t, "度加AI-AI成片失败退回", domain.BackTitle("unknown_type"))
	testx.Equal(t, "度加AI-图片生成", domain.ConsumeTitle("miaotu"))
	testx.Equal(t, "度加AI-视频生成失败退回", domain.BackTitle("miaoying"))
}

// recordingRepo records the ledger entries received by InsertConsume, for verifying orchestration parameters.
type recordingRepo struct {
	fakeRepo
	flow      *domain.PointsFlow
	sceneDesc string
}

func (r *recordingRepo) InsertConsume(ctx context.Context, flow *domain.PointsFlow) (uint64, error) {
	r.flow = flow
	r.sceneDesc = flow.SceneDesc
	return 1, nil
}

// TestDeductPassesSceneDesc verifies that the deduction entry title comes from ConsumeTitle.
func TestDeductPassesSceneDesc(t *testing.T) {
	r := &recordingRepo{}
	app := application.NewPointsApp(r)
	_, err := app.Deduct(context.Background(), 1, "p1", "p1_1", "miaoying", 8)
	testx.NoError(t, err)
	testx.Equal(t, "度加AI-视频生成", r.sceneDesc)
}

// TestRefundCallsRepoWithBackTitle verifies that refunds pass orderID/userID through and the title comes from BackTitle.
func TestRefundCallsRepoWithBackTitle(t *testing.T) {
	rr := &refundRecordingRepo{}
	app := application.NewPointsApp(rr)
	err := app.Refund(context.Background(), "p1_1", 1, "mix_video")
	testx.NoError(t, err)
	testx.Equal(t, "p1_1", rr.orderID)
	testx.Equal(t, int64(1), rr.userID)
	testx.Equal(t, "度加AI-AI成片失败退回", rr.title)
}

// refundRecordingRepo records the parameters received by RefundByOrderID, for verifying refund orchestration.
type refundRecordingRepo struct {
	fakeRepo
	orderID string
	userID  int64
	title   string
}

func (r *refundRecordingRepo) RefundByOrderID(ctx context.Context, orderID string, userID int64, title string) error {
	r.orderID = orderID
	r.userID = userID
	r.title = title
	return nil
}

// TestGetBalance verifies that the balance passes the repository result through.
func TestGetBalance(t *testing.T) {
	app := application.NewPointsApp(&fakeRepo{balance: 42})
	bal, err := app.GetBalance(context.Background(), 1)
	testx.NoError(t, err)
	testx.Equal(t, 42, bal)
}
