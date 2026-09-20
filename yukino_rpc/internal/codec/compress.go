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

package codec

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"sync"
)

type CompressionType byte

const (
	CompressionNone CompressionType = iota
	CompressionGzip
)

type compressor interface {
	compress([]byte) ([]byte, error)
	decompress([]byte) ([]byte, error)
}

type GzipCompressor struct{}

func (g *GzipCompressor) compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, err := w.Write(data)
	if err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (g *GzipCompressor) decompress(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()

	return io.ReadAll(r)
}

var (
	gzipCompressor = &GzipCompressor{}
	compressorMu   sync.RWMutex
	compressors    = make(map[CompressionType]compressor)
)

func RegisterCompressor(t CompressionType, c compressor) {
	compressorMu.Lock()
	defer compressorMu.Unlock()
	compressors[t] = c
}

func GetCompressor(t CompressionType) compressor {
	compressorMu.RLock()
	defer compressorMu.RUnlock()
	return compressors[t]
}

func init() {
	RegisterCompressor(CompressionGzip, gzipCompressor)
}

func Compress(data []byte, t CompressionType) ([]byte, error) {
	c := GetCompressor(t)
	if c == nil {
		return nil, errors.New("compressor not found")
	}
	return c.compress(data)
}

func Decompress(data []byte, t CompressionType) ([]byte, error) {
	c := GetCompressor(t)
	if c == nil {
		return nil, errors.New("compressor not found")
	}
	return c.decompress(data)
}
