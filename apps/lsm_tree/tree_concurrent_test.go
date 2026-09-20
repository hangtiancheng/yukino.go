package lsm_tree

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/lsm_tree/wal"
)

// waitGoroutinesBack polls until the goroutine count drops back to baseline so
// asynchronous cleanup goroutines (compaction exit, node destroy) finish before
// the tree directory is reused.
func waitGoroutinesBack(baseline int) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func Test_Tree_levelBinarySearch(t *testing.T) {
	// Three sorted nodes covering ("`", "c"], ("d", "g"], ("h", "k"].
	// startKey is the index separator, one byte below the real first key,
	// so it is exclusive while endKey is inclusive.
	tree := Tree{nodes: make([][]*Node, 1)}
	tree.nodes[0] = []*Node{
		{startKey: []byte("`"), endKey: []byte("c")},
		{startKey: []byte("d"), endKey: []byte("g")},
		{startKey: []byte("h"), endKey: []byte("k")},
	}

	cases := []struct {
		key  string
		want int // expected node index; -1 = not found
	}{
		{"a", 0}, {"b", 0}, {"c", 0},
		{"d", -1},
		{"e", 1}, {"f", 1}, {"g", 1},
		{"h", -1},
		{"i", 2}, {"j", 2}, {"k", 2},
		{"0", -1}, {"z", -1},
	}

	for _, c := range cases {
		node, ok := tree.levelBinarySearch(0, []byte(c.key), 0, len(tree.nodes[0])-1)
		if c.want == -1 {
			if ok {
				t.Errorf("key: %s, expect not found, got node: %+v", c.key, node)
			}
			continue
		}
		if !ok {
			t.Errorf("key: %s, expect node: %d, got: not found", c.key, c.want)
			continue
		}
		if node != tree.nodes[0][c.want] {
			t.Errorf("key: %s, expect node: %d, got node: %+v", c.key, c.want, node)
		}
	}
}

func Test_Tree_levelBinarySearch_BoundaryKey(t *testing.T) {
	// Regression: the next node's startKey may equal the previous node's
	// endKey (separator "c" = first key "d" minus one). A key equal to the
	// shared boundary belongs to the LEFT node, whose endKey is inclusive.
	tree := Tree{nodes: make([][]*Node, 1)}
	tree.nodes[0] = []*Node{
		{startKey: []byte("`"), endKey: []byte("c")},
		{startKey: []byte("c"), endKey: []byte("g")},
	}

	node, ok := tree.levelBinarySearch(0, []byte("c"), 0, len(tree.nodes[0])-1)
	if !ok || node != tree.nodes[0][0] {
		t.Errorf("boundary key: c, expect node 0, got ok: %v, node: %+v", ok, node)
	}

	node, ok = tree.levelBinarySearch(0, []byte("d"), 0, len(tree.nodes[0])-1)
	if !ok || node != tree.nodes[0][1] {
		t.Errorf("first key of next node: d, expect node 1, got ok: %v, node: %+v", ok, node)
	}
}

func Test_Tree_ConcurrentPutGet(t *testing.T) {
	conf, err := NewConfig(t.TempDir(),
		WithMaxLevel(4),           // 4-level lsm tree
		WithSSTSize(4*1024),       // tiny sst size to force memtable rotations
		WithSSTDataBlockSize(256), // tiny block size to force multi-block sstables
		WithSSTNumPerLevel(2),     // few sstables per level to force compactions
	)
	if err != nil {
		t.Fatal(err)
	}

	lsmTree, err := NewTree(conf)
	if err != nil {
		t.Fatal(err)
	}
	defer lsmTree.Close()

	const (
		writers   = 4
		readers   = 4
		perWriter = 800
	)

	key := func(w, i int) []byte {
		return []byte(fmt.Sprintf("key-%d-%06d", w, i))
	}
	value := func(w, i int) []byte {
		return []byte(fmt.Sprintf("val-%d-%06d", w, i))
	}

	// Writers rotate memtables, flush sstables and trigger compactions.
	var writerWG sync.WaitGroup
	for w := 0; w < writers; w++ {
		writerWG.Add(1)
		go func(w int) {
			defer writerWG.Done()
			for i := 0; i < perWriter; i++ {
				if err := lsmTree.Put(key(w, i), value(w, i)); err != nil {
					t.Error(err)
					return
				}
			}
		}(w)
	}

	// Readers race with writes, flushes and compactions.
	stop := make(chan struct{})
	var readerWG sync.WaitGroup
	for r := 0; r < readers; r++ {
		readerWG.Add(1)
		go func(r int) {
			defer readerWG.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}

				w := r % writers
				i := rand.Intn(perWriter)
				got, ok, err := lsmTree.Get(key(w, i))
				if err != nil {
					t.Error(err)
					return
				}
				// The key may not be written yet; a found value must be the latest one.
				if ok && !bytes.Equal(got, value(w, i)) {
					t.Errorf("key: %s, expect value: %s, got: %s", key(w, i), value(w, i), got)
					return
				}
			}
		}(r)
	}

	writerWG.Wait()
	close(stop)
	readerWG.Wait()

	// Every written key must be readable with its exact value once all writes return.
	for w := 0; w < writers; w++ {
		for i := 0; i < perWriter; i++ {
			got, ok, err := lsmTree.Get(key(w, i))
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Fatalf("key: %s not found", key(w, i))
			}
			if !bytes.Equal(got, value(w, i)) {
				t.Fatalf("key: %s, expect value: %s, got: %s", key(w, i), value(w, i), got)
			}
		}
	}
}

func Test_Tree_ConcurrentClose(t *testing.T) {
	conf, err := NewConfig(t.TempDir(),
		WithMaxLevel(4),
		WithSSTSize(128), // tiny sst size: nearly every write rotates the memtable
		WithSSTDataBlockSize(64),
		WithSSTNumPerLevel(2),
	)
	if err != nil {
		t.Fatal(err)
	}

	lsmTree, err := NewTree(conf)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup

	// Close the tree from multiple goroutines at once; only the first run must execute.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lsmTree.Close()
		}()
	}

	// Keep writing while the tree closes: Put may fail, but must not panic.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = lsmTree.Put([]byte(fmt.Sprintf("key-%06d", i)), []byte(fmt.Sprintf("val-%06d", i)))
		}
	}()

	wg.Wait()

	// Reads after close must not panic either.
	_, _, _ = lsmTree.Get([]byte("key-000001"))
	_, _, _ = lsmTree.Get([]byte("missing"))
}

func Test_Tree_CloseNoGoroutineLeak(t *testing.T) {
	baseline := runtime.NumGoroutine()

	conf, err := NewConfig(t.TempDir(),
		WithMaxLevel(4),
		WithSSTSize(256), // tiny sst size: many memtable rotations and queued senders
		WithSSTDataBlockSize(64),
		WithSSTNumPerLevel(2),
	)
	if err != nil {
		t.Fatal(err)
	}

	lsmTree, err := NewTree(conf)
	if err != nil {
		t.Fatal(err)
	}

	// Force memtable rotations so the compaction hand-off goroutines queue up.
	for i := 0; i < 500; i++ {
		if err := lsmTree.Put([]byte(fmt.Sprintf("key-%06d", i)), bytes.Repeat([]byte("v"), 64)); err != nil {
			t.Fatal(err)
		}
	}

	lsmTree.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline+1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("goroutine leak after close, baseline: %d, got: %d", baseline, runtime.NumGoroutine())
}

func Test_Tree_RestoreWals(t *testing.T) {
	dir := t.TempDir()
	conf, err := NewConfig(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Hand-write two wal files: 0.wal is restored as a read-only memtable,
	// 1.wal becomes the active memtable.
	writeWal := func(name string, kvs ...[2]string) {
		walWriter, err := wal.NewWALWriter(path.Join(dir, "walfile", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, kv := range kvs {
			if err := walWriter.Write([]byte(kv[0]), []byte(kv[1])); err != nil {
				t.Fatal(err)
			}
		}
		walWriter.Close()
	}
	writeWal("0.wal", [2]string{"k1", "v1"}, [2]string{"k2", "v2"})
	writeWal("1.wal", [2]string{"k3", "v3"})

	lsmTree, err := NewTree(conf)
	if err != nil {
		t.Fatal(err)
	}
	defer lsmTree.Close()

	// k1/k2 were restored into a read-only memtable (possibly already flushed).
	for _, key := range []string{"k1", "k2", "k3"} {
		got, ok, err := lsmTree.Get([]byte(key))
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("key: %s not found after restore", key)
		}
		if expect := "v" + key[1:]; string(got) != expect {
			t.Fatalf("key: %s, expect value: %s, got: %s", key, expect, got)
		}
	}

	// New writes must land in a wal file continuing the index sequence.
	if err := lsmTree.Put([]byte("k4"), []byte("v4")); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := lsmTree.Get([]byte("k4")); err != nil || !ok || string(got) != "v4" {
		t.Fatalf("key: k4, got: %s, ok: %v, err: %v", got, ok, err)
	}
}

func Test_Tree_RestoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	conf, err := NewConfig(dir,
		WithMaxLevel(4),
		WithSSTSize(2*1024), // tiny sst size to rotate the memtable many times
		WithSSTDataBlockSize(256),
		WithSSTNumPerLevel(4),
	)
	if err != nil {
		t.Fatal(err)
	}

	const total = 400
	baseline := runtime.NumGoroutine()

	// First tree: write enough data to rotate the memtable several times.
	tree1, err := NewTree(conf)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < total; i++ {
		key := []byte(fmt.Sprintf("k%04d", i))
		value := []byte(fmt.Sprintf("v%04d-%s", i, strings.Repeat("x", 128)))
		if err := tree1.Put(key, value); err != nil {
			t.Fatal(err)
		}
	}
	tree1.Close()

	// Wait for node-destroy goroutines before reusing the directory.
	waitGoroutinesBack(baseline)

	// Second tree: restore sstables and wal files from the directory.
	tree2, err := NewTree(conf)
	if err != nil {
		t.Fatal(err)
	}
	defer tree2.Close()

	for i := 0; i < total; i++ {
		key := []byte(fmt.Sprintf("k%04d", i))
		expect := []byte(fmt.Sprintf("v%04d-%s", i, strings.Repeat("x", 128)))
		got, ok, err := tree2.Get(key)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("key: %s not found after restore", key)
		}
		if !bytes.Equal(got, expect) {
			t.Fatalf("key: %s, expect value: %s, got: %s", key, expect, got)
		}
	}
}

func Test_Tree_Put_WalRotateError(t *testing.T) {
	dir := t.TempDir()
	conf, err := NewConfig(dir, WithMaxLevel(4), WithSSTSize(64))
	if err != nil {
		t.Fatal(err)
	}

	lsmTree, err := NewTree(conf)
	if err != nil {
		t.Fatal(err)
	}

	// Remove the wal directory so creating the next wal file fails.
	if err := os.Rename(path.Join(dir, "walfile"), path.Join(dir, "walfile-bak")); err != nil {
		t.Fatal(err)
	}

	// A big write exceeds the sst size threshold and rotates the memtable;
	// failing to create the new wal file must fail the Put instead of leaving
	// a nil wal writer that panics on the next Put.
	if err := lsmTree.Put([]byte("key"), bytes.Repeat([]byte("v"), 4096)); err == nil {
		t.Error("expect Put to fail when the wal file cannot be created")
	}

	// A subsequent Put must not panic either.
	_ = lsmTree.Put([]byte("key2"), []byte("v2"))

	lsmTree.Close()
}

func Test_Tree_NewTree_WalDirError(t *testing.T) {
	dir := t.TempDir()
	conf, err := NewConfig(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Replace the wal directory with a regular file so reading it fails.
	if err := os.Remove(path.Join(dir, "walfile")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path.Join(dir, "walfile"), []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err = NewTree(conf); err == nil {
		t.Error("expect NewTree to fail when the wal directory cannot be read")
	}
}
