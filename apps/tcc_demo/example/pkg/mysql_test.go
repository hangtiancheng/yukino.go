package pkg

import (
	"reflect"
	"sync"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/tcc_demo/example/mocks"
	"go.uber.org/mock/gomock"
	"gorm.io/gorm"
)

func Test_NewDB(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockFactory := mocks.NewMockDBFactory(ctrl)
	mockFactory.EXPECT().Open(gomock.Any(), gomock.Any()).Return(&gorm.DB{}, nil).AnyTimes()

	oldFactory := dbFactory
	defer func() { dbFactory = oldFactory }()
	dbFactory = mockFactory

	db, err := NewDB("", &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}

	debounce = sync.Once{}

	defaultDB := GetDB()
	if reflect.TypeFor[*gorm.DB]() != reflect.TypeFor[*gorm.DB]() {
		t.Errorf("expected %T, got %T", db, defaultDB)
	}
}
