package filter

// Filter helps an sstable quickly determine whether a key may exist in a block.
type Filter interface {
	Add(key []byte)                // Add inserts a key into the filter
	Exist(bitmap, key []byte) bool // Exist reports whether the key may exist
	Hash() []byte                  // Hash builds the filter bitmap
	Reset()                        // Reset clears the filter
	KeyLen() int                   // KeyLen returns the number of keys in the filter
}
