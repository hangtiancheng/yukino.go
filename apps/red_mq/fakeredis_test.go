package red_mq

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/red_mq/redis"
)

// fakeRedis is a minimal RESP2 redis server, just good enough for the
// stream commands red_mq issues. It lets the producer/consumer paths run
// under go test -race without a real redis.
type fakeRedis struct {
	ln net.Listener

	mu        sync.Mutex
	xaddCnt   int
	xackIDs   []string
	newMsg    *redis.MsgEntity // reply for XREADGROUP with id ">"
	msgOnce   bool             // hand out newMsg only on the first XREADGROUP
	newServed int
}

func newFakeRedis(t *testing.T) *fakeRedis {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake redis listen: %v", err)
	}
	f := &fakeRedis{ln: ln}
	go f.serve()
	t.Cleanup(func() {
		ln.Close()
	})
	return f
}

func (f *fakeRedis) addr() string { return f.ln.Addr().String() }

func (f *fakeRedis) client() *redis.Client {
	return redis.NewClient("tcp", f.addr(), "")
}

// setNewMsg configures the reply for XREADGROUP with id ">". When once is
// true the message is delivered only on the first poll.
func (f *fakeRedis) setNewMsg(msg *redis.MsgEntity, once bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.newMsg = msg
	f.msgOnce = once
	f.newServed = 0
}

func (f *fakeRedis) ackedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.xackIDs...)
}

func (f *fakeRedis) xaddCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.xaddCnt
}

func (f *fakeRedis) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeRedis) handle(conn net.Conn) {
	defer conn.Close()
	rd := bufio.NewReader(conn)
	for {
		args, err := readRespCommand(rd)
		if err != nil {
			return
		}
		if !f.dispatch(conn, args) {
			return
		}
	}
}

// dispatch replies to one command and reports whether the connection is
// still usable.
func (f *fakeRedis) dispatch(conn net.Conn, args []string) bool {
	w := bufio.NewWriter(conn)
	defer w.Flush()

	cmd := strings.ToLower(args[0])
	switch cmd {
	case "hello":
		// Refuse RESP3 so go-redis falls back to plain RESP2.
		fmt.Fprintf(w, "-ERR unknown command 'HELLO'\r\n")
	case "auth", "client", "select":
		fmt.Fprintf(w, "+OK\r\n")
	case "ping":
		fmt.Fprintf(w, "+PONG\r\n")
	case "xadd":
		f.mu.Lock()
		f.xaddCnt++
		id := strconv.Itoa(f.xaddCnt) + "-1"
		f.mu.Unlock()
		writeBulk(w, id)
	case "xack":
		if len(args) > 3 {
			f.mu.Lock()
			f.xackIDs = append(f.xackIDs, args[3:]...)
			f.mu.Unlock()
		}
		fmt.Fprintf(w, ":%d\r\n", len(args)-3)
	case "xreadgroup":
		if args[len(args)-1] == ">" {
			f.mu.Lock()
			msg := f.newMsg
			if msg != nil && f.msgOnce && f.newServed > 0 {
				msg = nil
			} else if msg != nil {
				f.newServed++
			}
			f.mu.Unlock()
			if msg != nil {
				writeStreamReply(w, "test_topic", msg)
				return true
			}
		}
		// Null array: no messages available.
		fmt.Fprintf(w, "*-1\r\n")
	default:
		fmt.Fprintf(w, "-ERR unknown command '%s'\r\n", strings.ToUpper(cmd))
	}
	return true
}

func writeBulk(w *bufio.Writer, s string) {
	fmt.Fprintf(w, "$%d\r\n%s\r\n", len(s), s)
}

// writeStreamReply answers XREADGROUP with a single stream holding one
// message with one field, matching the RESP2 reply shape of real redis:
// streams -> [name, messages] -> per message [id, fields].
func writeStreamReply(w *bufio.Writer, topic string, msg *redis.MsgEntity) {
	fmt.Fprintf(w, "*1\r\n") // streams
	fmt.Fprintf(w, "*2\r\n") // [name, messages]
	writeBulk(w, topic)
	fmt.Fprintf(w, "*1\r\n") // 1 message
	fmt.Fprintf(w, "*2\r\n") // [id, fields]
	writeBulk(w, msg.MsgID)
	fmt.Fprintf(w, "*2\r\n") // field/value pairs
	writeBulk(w, msg.Key)
	writeBulk(w, msg.Val)
}

// readRespCommand parses one RESP2 array command from the wire.
func readRespCommand(rd *bufio.Reader) ([]string, error) {
	head, err := rd.ReadString('\n')
	if err != nil {
		return nil, err
	}
	head = strings.TrimRight(head, "\r\n")
	if !strings.HasPrefix(head, "*") {
		return nil, fmt.Errorf("unexpected line %q", head)
	}
	n, err := strconv.Atoi(head[1:])
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		bulk, err := rd.ReadString('\n')
		if err != nil {
			return nil, err
		}
		bulk = strings.TrimRight(bulk, "\r\n")
		if !strings.HasPrefix(bulk, "$") {
			return nil, fmt.Errorf("unexpected bulk head %q", bulk)
		}
		l, err := strconv.Atoi(bulk[1:])
		if err != nil {
			return nil, err
		}
		if l < 0 {
			args = append(args, "")
			continue
		}
		buf := make([]byte, l+2)
		if _, err := io.ReadFull(rd, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:l]))
	}
	return args, nil
}

// recordMailbox is a DeadLetterMailbox capturing every delivery, optionally
// failing them all.
type recordMailbox struct {
	err   error
	first chan struct{}
	once  sync.Once

	mu   sync.Mutex
	msgs []*redis.MsgEntity
}

func newRecordMailbox(deliverErr error) *recordMailbox {
	return &recordMailbox{err: deliverErr, first: make(chan struct{})}
}

func (m *recordMailbox) Deliver(ctx context.Context, msg *redis.MsgEntity) error {
	m.mu.Lock()
	m.msgs = append(m.msgs, msg)
	m.mu.Unlock()
	m.once.Do(func() { close(m.first) })
	return m.err
}

func (m *recordMailbox) delivered() []*redis.MsgEntity {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*redis.MsgEntity(nil), m.msgs...)
}

// waitForAck polls until the fake redis has recorded an XACK for msgID.
func waitForAck(t *testing.T, f *fakeRedis, msgID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, id := range f.ackedIDs() {
			if id == msgID {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("msg %s was never acked, acked: %v", msgID, f.ackedIDs())
}
