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

package transport

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/protocol"
)

const BufferSize = 4096

// PacketBuffer stores partial reads and extracts complete frames.
type PacketBuffer struct {
	buf  []byte
	lock sync.Mutex
}

func (pb *PacketBuffer) Write(data []byte) {
	pb.lock.Lock()
	pb.buf = append(pb.buf, data...)
	pb.lock.Unlock()
}

func (pb *PacketBuffer) Read() []byte {
	pb.lock.Lock()
	defer pb.lock.Unlock()

	// Resync: skip garbage until the buffer starts with the magic number.
	for len(pb.buf) >= 2 && binary.BigEndian.Uint16(pb.buf[0:2]) != protocol.Magic {
		pb.buf = pb.buf[1:]
	}

	if len(pb.buf) < 10 {
		return nil
	}

	headerLen := int(protocol.DecodeHeaderLen(pb.buf[2:6]))
	bodyLen := int(protocol.DecodeBodyLen(pb.buf[6:10]))
	totalLen := 10 + headerLen + bodyLen

	if len(pb.buf) < totalLen {
		return nil
	}

	packet := make([]byte, totalLen)
	copy(packet, pb.buf[:totalLen])

	pb.buf = pb.buf[totalLen:]
	return packet
}

type TCPConnection struct {
	conn   net.Conn
	reader *bufio.Reader
	buffer *PacketBuffer

	writeMu sync.Mutex
}

func NewTCPConnection(conn net.Conn) *TCPConnection {
	return &TCPConnection{
		conn:   conn,
		reader: bufio.NewReaderSize(conn, BufferSize),
		buffer: &PacketBuffer{
			buf: make([]byte, 0, BufferSize*2),
		},
	}
}

func (tc *TCPConnection) Read() (*protocol.Message, error) {
	for {
		if packet := tc.buffer.Read(); packet != nil {
			return protocol.Decode(packet)
		}

		tmp := make([]byte, BufferSize)
		n, err := tc.reader.Read(tmp)
		if err != nil {
			if err == io.EOF {
				return nil, err
			}
			return nil, err
		}

		if n > 0 {
			tc.buffer.Write(tmp[:n])
		}
	}
}

func (tc *TCPConnection) Write(msg *protocol.Message) error {
	data, err := protocol.Encode(msg)
	if err != nil {
		return err
	}

	tc.writeMu.Lock()
	defer tc.writeMu.Unlock()

	total := 0
	for total < len(data) {
		n, err := tc.conn.Write(data[total:])
		if err != nil {
			return err
		}
		total += n
	}

	return nil
}

func (tc *TCPConnection) Close() error {
	if tcp, ok := tc.conn.(*net.TCPConn); ok {
		tcp.SetLinger(0)
	}
	return tc.conn.Close()
}

// SetReadDeadline interrupts a blocked Read; used for graceful shutdown.
func (tc *TCPConnection) SetReadDeadline(t time.Time) error {
	return tc.conn.SetReadDeadline(t)
}

func (tc *TCPConnection) RemoteAddr() string {
	return tc.conn.RemoteAddr().String()
}
