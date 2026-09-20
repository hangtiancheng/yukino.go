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

package lsm_tree

import (
	"bytes"
	"os"
	"path"
	"testing"
	"time"
)

func Test_LSM_UseCase(t *testing.T) {
	// 1. Build the config.
	conf, _ := NewConfig(t.TempDir(), // directory for sst files
		WithMaxLevel(7),               // 7-level lsm tree
		WithSSTSize(1024*1024),        // level-0 sstable size: 1MB
		WithSSTDataBlockSize(16*1024), // sstable block size: 16KB
		WithSSTNumPerLevel(10),        // 10 sstables per level
	)

	// 2. Create the lsm tree.
	lsmTree, err := NewTree(conf)
	if err != nil {
		t.Error(err)
		return
	}
	defer lsmTree.Close()

	// 3. Write data.
	_ = lsmTree.Put([]byte{1}, []byte{2})

	// 4. Read data.
	v, _, _ := lsmTree.Get([]byte{1})

	t.Log(v)
}

func Test_LSM(t *testing.T) {
	// Build the config.
	conf, err := NewConfig(t.TempDir(), // directory for sst files
		WithMaxLevel(7),              // 7-level lsm tree
		WithSSTSize(32*1024),         // level-0 sstable size: 32KB
		WithSSTDataBlockSize(2*1024), // sstable block size: 2KB
		WithSSTNumPerLevel(4),        // 4 sstables per level
	)
	if err != nil {
		t.Error(err)
		return
	}

	// Create the lsm tree.
	lsmTree, err := NewTree(conf)
	if err != nil {
		t.Error(err)
		return
	}
	defer lsmTree.Close()

	kvs := []struct {
		key []byte
		val []byte
	}{}

	for i := 65; i <= 122; i++ {
		for j := 65; j <= 122; j++ {
			for k := 65; k <= 85; k++ {
				kvs = append(kvs, struct {
					key []byte
					val []byte
				}{
					key: []byte{uint8(i), uint8(j), uint8(k)},
					val: []byte{uint8(i), uint8(j), uint8(k)},
				})
			}
		}
	}

	for i := 0; i < len(kvs); i++ {
		if err = lsmTree.Put(kvs[i].key, kvs[i].val); err != nil {
			t.Error(err)
			return
		}
	}

	for _, kv := range kvs {
		v, ok, err := lsmTree.Get(kv.key)
		if err != nil {
			t.Error(err)
			return
		}
		if !ok {
			t.Errorf("key: %s not exist", kv.key)
			return
		}
		if !bytes.Equal(v, kv.val) {
			t.Errorf("key: %s, expect v: %s, got: %s", kv.key, kv.val, v)
			return
		}
	}

	<-time.After(time.Second)
}

func Test_Tree_getSortedSSTEntries(t *testing.T) {
	if err := os.Mkdir("test", os.ModePerm); err != nil {
		t.Error(err)
		return
	}
	defer func() {
		if err := os.Remove("./test"); err != nil {
			t.Error(err)
		}
	}()

	files := []string{"1_1.sst", "1_2.ab", "10_0.sst", "2_3.sst", "1_5.sst", "10_10.sst", "10_5.sst"}
	for _, file := range files {
		fd, err := os.Create(path.Join("test", file))
		if err != nil {
			t.Error(err)
			return
		}
		defer func() {
			fd.Close()
			if err = os.Remove(path.Join("test", file)); err != nil {
				t.Error(err)
			}
		}()
	}

	tree := Tree{
		conf: &Config{
			Dir: "./test",
		},
	}

	expectEntries := []string{
		"1_1.sst", "1_5.sst", "2_3.sst", "10_0.sst", "10_5.sst", "10_10.sst",
	}

	gotEntries, err := tree.getSortedSSTEntries()
	if err != nil {
		t.Error(err)
		return
	}

	if len(gotEntries) != len(expectEntries) {
		t.Errorf("got len: %d, expect: %d", len(gotEntries), len(expectEntries))
		return
	}

	for i := range gotEntries {
		if gotEntries[i].Name() != expectEntries[i] {
			t.Errorf("index: %d, got entries: %s, expect: %s", i, gotEntries[i].Name(), expectEntries[i])
		}
	}
}

func Test_pathJoin(t *testing.T) {
	if got := path.Join("./", "wal", "1.sst"); got != "wal/1.sst" {
		t.Errorf("path.Join(./, wal, 1.sst), expect: wal/1.sst, got: %s", got)
	}
	if got := path.Join("/root/", "/wal", "1.sst"); got != "/root/wal/1.sst" {
		t.Errorf("path.Join(/root/, /wal, 1.sst), expect: /root/wal/1.sst, got: %s", got)
	}
	if got := path.Join("/root", "/wal", "1.sst"); got != "/root/wal/1.sst" {
		t.Errorf("path.Join(/root, /wal, 1.sst), expect: /root/wal/1.sst, got: %s", got)
	}
	if got := path.Join("/root", "wal", "1.sst"); got != "/root/wal/1.sst" {
		t.Errorf("path.Join(/root, wal, 1.sst), expect: /root/wal/1.sst, got: %s", got)
	}
}
