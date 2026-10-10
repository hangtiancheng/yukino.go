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

	"github.com/hangtiancheng/yukino.go/components/lsm_tree/wal"
)

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
	tree := Tree{nodes: make([][]*Node, 1)}
	tree.nodes[0] = []*Node{
		{startKey: []byte("`"), endKey: []byte("c")},
		{startKey: []byte("d"), endKey: []byte("g")},
		{startKey: []byte("h"), endKey: []byte("k")},
	}

	cases := []struct {
		key  string
		want int
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
		WithMaxLevel(4),
		WithSSTSize(4*1024),
		WithSSTDataBlockSize(256),
		WithSSTNumPerLevel(2),
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
		WithSSTSize(128),
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

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lsmTree.Close()
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = lsmTree.Put([]byte(fmt.Sprintf("key-%06d", i)), []byte(fmt.Sprintf("val-%06d", i)))
		}
	}()

	wg.Wait()

	_, _, _ = lsmTree.Get([]byte("key-000001"))
	_, _, _ = lsmTree.Get([]byte("missing"))
}

func Test_Tree_CloseNoGoroutineLeak(t *testing.T) {
	baseline := runtime.NumGoroutine()

	conf, err := NewConfig(t.TempDir(),
		WithMaxLevel(4),
		WithSSTSize(256),
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
		WithSSTSize(2*1024),
		WithSSTDataBlockSize(256),
		WithSSTNumPerLevel(4),
	)
	if err != nil {
		t.Fatal(err)
	}

	const total = 400
	baseline := runtime.NumGoroutine()

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

	waitGoroutinesBack(baseline)

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

	if err := os.Rename(path.Join(dir, "walfile"), path.Join(dir, "walfile-bak")); err != nil {
		t.Fatal(err)
	}

	if err := lsmTree.Put([]byte("key"), bytes.Repeat([]byte("v"), 4096)); err == nil {
		t.Error("expect Put to fail when the wal file cannot be created")
	}

	_ = lsmTree.Put([]byte("key2"), []byte("v2"))

	lsmTree.Close()
}

func Test_Tree_NewTree_WalDirError(t *testing.T) {
	dir := t.TempDir()
	conf, err := NewConfig(dir)
	if err != nil {
		t.Fatal(err)
	}

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
