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
