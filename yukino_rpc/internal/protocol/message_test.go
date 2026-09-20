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

package protocol

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/codec"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	msg := &Message{
		Header: &Header{
			RequestID:   42,
			ServiceName: "Arith",
			MethodName:  "Add",
			CodecType:   CodecTypeJSON,
			Compression: codec.CompressionGzip,
		},
		Body: []byte("hello"),
	}
	data, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if binary.BigEndian.Uint16(data[:2]) != Magic {
		t.Fatal("encoded magic number mismatch")
	}
	decoded, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if decoded.Header.RequestID != msg.Header.RequestID || decoded.Header.ServiceName != msg.Header.ServiceName {
		t.Fatalf("decoded header = %+v", decoded.Header)
	}
	if string(decoded.Body) != "hello" {
		t.Fatalf("decoded body = %q", decoded.Body)
	}
}

func TestEncodeDecodeErrors(t *testing.T) {
	if _, err := Encode(&Message{}); err == nil {
		t.Fatal("expected nil header error")
	}
	if got := DecodeHeaderLen([]byte{1}); got != 0 {
		t.Fatalf("short header len = %d, want 0", got)
	}
	if got := DecodeBodyLen([]byte{1}); got != 0 {
		t.Fatalf("short body len = %d, want 0", got)
	}
	if _, err := Decode([]byte{1, 2}); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Fatalf("expected short data error, got %v", err)
	}
	data, err := Encode(&Message{Header: &Header{}, Body: []byte("body")})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	data[0] = 0
	if _, err := Decode(data); err == nil || !strings.Contains(err.Error(), "magic") {
		t.Fatalf("expected magic error, got %v", err)
	}
	data, err = Encode(&Message{Header: &Header{}, Body: []byte("body")})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if _, err := Decode(data[:len(data)-1]); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected incomplete packet error, got %v", err)
	}
	data, err = Encode(&Message{Header: &Header{Compression: codec.CompressionGzip}, Body: []byte("body")})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	headerLen := binary.BigEndian.Uint32(data[2:6])
	bodyStart := 10 + int(headerLen)
	data[bodyStart] ^= 0xff
	if _, err := Decode(data); err == nil {
		t.Fatal("expected decompression error")
	}
}
