// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
