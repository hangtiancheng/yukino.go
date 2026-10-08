package memtable

// MemTableConstructor builds a new MemTable instance.
type MemTableConstructor func() MemTable

// MemTable is an ordered in-memory table.
type MemTable interface {
	Put(key, value []byte)         // Put writes a key-value pair
	Get(key []byte) ([]byte, bool) // Get reads the value for a key; the bool flag reports existence
	All() []*KV                    // All returns all key-value pairs in order
	Size() int                     // Size returns the data size in bytes
	EntriesCnt() int               // EntriesCnt returns the number of key-value pairs
}

type KV struct {
	Key, Value []byte
}
