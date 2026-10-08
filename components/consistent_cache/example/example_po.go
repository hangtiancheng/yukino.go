package example

import (
	"encoding/json"
)

type Example struct {
	ID   uint   `json:"id" gorm:"primarykey"`
	Key_ string `json:"key" gorm:"column:key"`
	Data string `json:"data" gorm:"column:data"`
}

// TableName returns the destination table name.
func (e *Example) TableName() string {
	return "example"
}

// KeyColumn returns the column name backing Key.
func (e *Example) KeyColumn() string {
	return "key"
}

// Key returns the value of the key column.
func (e *Example) Key() string {
	return e.Key_
}

func (e *Example) DataColumn() []string {
	return []string{"data"}
}

// Write serializes the object to a string.
func (e *Example) Write() (string, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// Read deserializes the string body back into the object.
func (e *Example) Read(body string) error {
	return json.Unmarshal([]byte(body), e)
}
