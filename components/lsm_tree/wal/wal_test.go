package wal

import (
	"bytes"
	"testing"

	"github.com/hangtiancheng/yukino.go/components/lsm_tree/memtable"
)

func Test_WAL(t *testing.T) {
	walWriter, err := NewWALWriter("./test.wal")
	if err != nil {
		t.Error(err)
		return
	}
	defer walWriter.Close()

	skiplist := memtable.NewSkiplist()

	kvs := make([]*memtable.KV, 0, 100)
	for i := range 100 {
		kvs = append(kvs, &memtable.KV{
			Key:   []byte{'a' + uint8(i)},
			Value: []byte{'b' + uint8(i)},
		})
	}

	for _, kv := range kvs {
		skiplist.Put(kv.Key, kv.Value)
		if err = walWriter.Write(kv.Key, kv.Value); err != nil {
			t.Error(err)
			return
		}
	}

	walReader, err := NewWALReader("./test.wal")
	if err != nil {
		t.Error(err)
		return
	}
	defer walReader.Close()

	restoredSkiplist := memtable.NewSkiplist()
	walReader.RestoreToMemtable(restoredSkiplist)

	originKVs := skiplist.All()
	restoredKVs := restoredSkiplist.All()

	if len(originKVs) != len(restoredKVs) {
		t.Errorf("not equal len, got: %d, expect: %d", len(restoredKVs), len(originKVs))
		return
	}

	for i := range originKVs {
		if !bytes.Equal(originKVs[i].Key, restoredKVs[i].Key) {
			t.Errorf("not equal, index: %d, got key: %s, expect: %s", i, restoredKVs[i].Key, originKVs[i].Key)
		}
		if !bytes.Equal(originKVs[i].Value, restoredKVs[i].Value) {
			t.Errorf("not equal, index: %d, got val: %s, expect: %s", i, restoredKVs[i].Value, originKVs[i].Value)
		}
	}
}
