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

// newMockDB creates a sqlmock-driven gorm DB (for this package's tests).
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

// TestUpdateIfStatusAddsStatusWhereCondition verifies the CAS: UpdateIfStatus must carry the
// WHERE status IN (...) condition (preventing concurrent overwrites of terminal states) and return RowsAffected==1.
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
	testx.True(t, ok, "a CAS hit (DB status running) should return true")
	testx.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateIfStatusReturnsFalseWhenCASMiss verifies CAS failure: RowsAffected==0 → returns false.
func TestUpdateIfStatusReturnsFalseWhenCASMiss(t *testing.T) {
	gdb, mock := newMockDB(t)
	repo := dao.NewProjectRepositoryImpl(gdb)

	project := &domain.Project{ID: 1, Status: domain.ProjectStatusDone}

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `cg_creation_project` SET .* WHERE id = \\? AND status IN \\(\\?\\)").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
								sqlmock.AnyArg(), uint64(1), domain.ProjectStatusAudit).
		WillReturnResult(sqlmock.NewResult(0, 0)) // status mismatch, 0 rows affected
	mock.ExpectCommit()

	ok, err := repo.UpdateIfStatus(context.Background(), project, domain.ProjectStatusAudit)
	testx.NoError(t, err)
	testx.False(t, ok, "a CAS miss (DB status not audit) should return false")
	testx.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateUnconditional verifies that the unconditional Update carries no WHERE status (preserving the original behavior).
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
