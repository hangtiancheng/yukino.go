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
	"fmt"
	"math"

	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/codec"
)

const Magic uint16 = 0x1234

type Message struct {
	Header *Header
	Body   []byte
}

func Encode(msg *Message) ([]byte, error) {

	if msg.Header == nil {
		return nil, fmt.Errorf("header is nil")
	}

	bodyBytes := msg.Body

	if msg.Header.Compression != codec.CompressionNone {
		var err error
		bodyBytes, err = codec.Compress(bodyBytes, msg.Header.Compression)
		if err != nil {
			return nil, err
		}
	}

	headerCodec, err := codec.New(codec.JSON)
	if err != nil {
		return nil, err
	}

	headerBytes, err := headerCodec.Marshal(msg.Header)
	if err != nil {
		return nil, err
	}

	headerLen := uint32(len(headerBytes))
	bodyLen := uint32(len(bodyBytes))

	total := 2 + 4 + 4 + headerLen + bodyLen
	buf := make([]byte, total)

	binary.BigEndian.PutUint16(buf[0:2], Magic)

	binary.BigEndian.PutUint32(buf[2:6], headerLen)

	binary.BigEndian.PutUint32(buf[6:10], bodyLen)

	copy(buf[10:], headerBytes)

	copy(buf[10+headerLen:], bodyBytes)

	return buf, nil
}

func DecodeHeaderLen(data []byte) uint32 {
	if len(data) < 4 {
		return 0
	}
	return binary.BigEndian.Uint32(data)
}

func DecodeBodyLen(data []byte) uint32 {
	if len(data) < 4 {
		return 0
	}
	return binary.BigEndian.Uint32(data)
}

func Decode(data []byte) (*Message, error) {

	if len(data) < 10 {
		return nil, fmt.Errorf("data too short")
	}

	if binary.BigEndian.Uint16(data[0:2]) != Magic {
		return nil, fmt.Errorf("invalid magic number")
	}

	headerLen := binary.BigEndian.Uint32(data[2:6])
	bodyLen := binary.BigEndian.Uint32(data[6:10])

	totalLen := uint64(10) + uint64(headerLen) + uint64(bodyLen)
	if totalLen > math.MaxInt {
		return nil, fmt.Errorf("packet too large")
	}
	if len(data) < int(totalLen) {
		return nil, fmt.Errorf("incomplete packet")
	}

	headerBytes := data[10 : 10+headerLen]

	headerCodec, err := codec.New(codec.JSON)
	if err != nil {
		return nil, err
	}

	var header Header
	if err := headerCodec.Unmarshal(headerBytes, &header); err != nil {
		return nil, err
	}

	bodyBytes := data[10+headerLen : 10+headerLen+bodyLen]

	if header.Compression != codec.CompressionNone {
		bodyBytes, err = codec.Decompress(bodyBytes, header.Compression)
		if err != nil {
			return nil, err
		}
	}

	return &Message{
		Header: &header,
		Body:   bodyBytes,
	}, nil
}
