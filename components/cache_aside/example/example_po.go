package example

import (
	"encoding/json"
)

type Example struct {
	ID   uint   `json:"id" gorm:"primarykey"`
	Key_ string `json:"key" gorm:"column:key"`
	Data string `json:"data" gorm:"column:data"`
}

func (e *Example) TableName() string {
	return "example"
}

func (e *Example) KeyColumn() string {
	return "key"
}

func (e *Example) Key() string {
	return e.Key_
}

func (e *Example) DataColumn() []string {
	return []string{"data"}
}

func (e *Example) Write() (string, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (e *Example) Read(body string) error {
	return json.Unmarshal([]byte(body), e)
}
