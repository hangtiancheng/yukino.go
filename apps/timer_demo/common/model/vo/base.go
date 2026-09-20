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

package vo

import "fmt"

const (
	successCode = 0
)

type Errorer interface {
	Error() error
}

func NewCodeMsg(code int32, msg string) CodeMsg {
	cm := CodeMsg{
		Code: code,
		Msg:  msg,
	}
	cm.Err = cm.Error()
	return cm
}

func NewCodeMsgWithErr(err error) CodeMsg {
	return CodeMsg{Err: err}
}

type CodeMsg struct {
	Code int32  `json:"code"`
	Msg  string `json:"msg"`
	Err  error  `json:"err"`
}

func (c *CodeMsg) Error() error {
	if c.Code == successCode {
		return nil
	}
	return fmt.Errorf("code: %d, msg: %s", c.Code, c.Msg)
}

type PageLimiter struct {
	Index int `json:"pageIndex" form:"pageIndex"`
	Size  int `json:"pageSize" form:"pageSize"`
}

func (p *PageLimiter) Get() (offset, limit int) {
	if p.Index <= 0 {
		p.Index = 1
	}

	if p.Size <= 0 {
		p.Size = 10
	}

	return (p.Index - 1) * p.Size, p.Size
}
