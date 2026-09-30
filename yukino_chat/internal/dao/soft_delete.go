package dao

import (
	"github.com/hangtiancheng/yukino.go/yukino_orm"
)

func ActiveQuery(model any) *yukino_orm.Query {
	return Engine.Model(model).WhereNull("deleted_at")
}
