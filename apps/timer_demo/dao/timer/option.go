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

package timer

import (
	"fmt"

	"gorm.io/gorm"
)

type Option func(*gorm.DB) *gorm.DB

func WithID(id uint) Option {
	return func(d *gorm.DB) *gorm.DB {
		return d.Where("id = ?", id)
	}
}

func WithIDs(ids []uint) Option {
	return func(d *gorm.DB) *gorm.DB {
		return d.Where("id IN ?", ids)
	}
}

func WithStatus(status int32) Option {
	return func(d *gorm.DB) *gorm.DB {
		return d.Where("status = ?", status)
	}
}

func WithAsc() Option {
	return func(d *gorm.DB) *gorm.DB {
		return d.Order("created_at ASC")
	}
}

func WithDesc() Option {
	return func(d *gorm.DB) *gorm.DB {
		return d.Order("created_at DESC")
	}
}

func WithApp(app string) Option {
	return func(d *gorm.DB) *gorm.DB {
		return d.Where("app = ?", app)
	}
}

func WithFuzzyName(name string) Option {
	return func(d *gorm.DB) *gorm.DB {
		return d.Where(fmt.Sprintf("name like %q", ("%" + name + "%")))
	}
}

func WithPageLimit(offset, limit int) Option {
	return func(d *gorm.DB) *gorm.DB {
		return d.Offset(offset).Limit(limit)
	}
}
