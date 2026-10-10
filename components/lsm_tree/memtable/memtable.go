package memtable

type MemTableConstructor func() MemTable

type MemTable interface {
	Put(key, value []byte)
	Get(key []byte) ([]byte, bool)
	All() []*KV
	Size() int
	EntriesCnt() int
}

type KV struct {
	Key, Value []byte
}
