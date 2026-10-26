package dao_test

import (
	"context"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/dao"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// newMockDB 创建 sqlmock 驱动的 gorm DB（供本包测试用）。
func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	testx.NoError(t, err)
	gdb, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      db,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{})
	testx.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return gdb, mock
}

// TestUpdateIfStatusAddsStatusWhereCondition 验证 CAS：UpdateIfStatus 必须带
// WHERE status IN (...) 条件（防并发覆盖终态），并返回 RowsAffected==1。
func TestUpdateIfStatusAddsStatusWhereCondition(t *testing.T) {
	gdb, mock := newMockDB(t)
	repo := dao.NewProjectRepositoryImpl(gdb)

	project := &domain.Project{
		ID:           1,
		Status:       domain.ProjectStatusAudit,
		PointsStatus: domain.PointStatusUsed,
		Ext:          `{"async_audit_record_id":42}`,
	}

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `cg_creation_project` SET .* WHERE id = \\? AND status IN \\(\\?\\)").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), uint64(1), domain.ProjectStatusRunning).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	ok, err := repo.UpdateIfStatus(context.Background(), project, domain.ProjectStatusRunning)
	testx.NoError(t, err)
	testx.True(t, ok, "CAS 命中（DB 状态为 running）应返回 true")
	testx.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateIfStatusReturnsFalseWhenCASMiss 验证 CAS 失败：RowsAffected==0 → 返回 false。
func TestUpdateIfStatusReturnsFalseWhenCASMiss(t *testing.T) {
	gdb, mock := newMockDB(t)
	repo := dao.NewProjectRepositoryImpl(gdb)

	project := &domain.Project{ID: 1, Status: domain.ProjectStatusDone}

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `cg_creation_project` SET .* WHERE id = \\? AND status IN \\(\\?\\)").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
								sqlmock.AnyArg(), uint64(1), domain.ProjectStatusAudit).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 状态不匹配，0 行受影响
	mock.ExpectCommit()

	ok, err := repo.UpdateIfStatus(context.Background(), project, domain.ProjectStatusAudit)
	testx.NoError(t, err)
	testx.False(t, ok, "CAS 未命中（DB 状态不是 audit）应返回 false")
	testx.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateUnconditional 验证无条件的 Update 不带 WHERE status（保持原有行为）。
func TestUpdateUnconditional(t *testing.T) {
	gdb, mock := newMockDB(t)
	repo := dao.NewProjectRepositoryImpl(gdb)

	project := &domain.Project{ID: 1, Status: domain.ProjectStatusDone}

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `cg_creation_project` SET .* WHERE id = \\?").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), uint64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	testx.NoError(t, repo.Update(context.Background(), project))
	testx.NoError(t, mock.ExpectationsWereMet())
}
