package lsm_tree

import (
	"testing"
)

func Test_SSTWriter(t *testing.T) {
	conf, err := NewConfig("./lsm", WithSSTDataBlockSize(16))
	if err != nil {
		t.Error(err)
		return
	}
	sstWriter, err := NewSSTWriter("test.sst", conf)
	if err != nil {
		t.Error(err)
		return
	}
	defer sstWriter.Close()

	sstWriter.Append([]byte("a"), []byte("b"))
	sstWriter.Append([]byte("ab"), []byte("cd"))
	sstWriter.Append([]byte("e"), []byte("f"))
	sstWriter.Append([]byte("ef"), []byte("gh"))

	_, blockToFilter, index := sstWriter.Finish()
	if len(blockToFilter) != 2 {
		t.Errorf("unexpect filter len: %d", len(blockToFilter))
	}

	if _, ok := blockToFilter[0]; !ok {
		t.Error("miss filter key: 0")
	}

	if _, ok := blockToFilter[16]; !ok {
		t.Error("miss filter key: 19")
	}

	if len(index) != 3 {
		t.Errorf("unexpect index len: %d", len(index))
	}

	if string(index[0].Key) != "`" || index[0].PrevBlockOffset != 0 || index[0].PrevBlockSize != 0 {
		t.Errorf("invalid index0: %+v, key: %s", index[0], index[0].Key)
	}

	if string(index[1].Key) != "e" || index[1].PrevBlockOffset != 0 || index[1].PrevBlockSize != 16 {
		t.Errorf("invalid index1: %+v", index[1])
	}

	if string(index[2].Key) != "ef" || index[2].PrevBlockOffset != 16 || index[1].PrevBlockSize != 16 {
		t.Errorf("invalid index2: %+v", index[2])
	}
}
