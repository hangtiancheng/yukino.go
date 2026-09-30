package yukino_cache

// ByteView is an immutable byte view used for cached data.
type ByteView struct {
	b []byte
}

// Len returns the number of bytes in the view.
func (b ByteView) Len() int {
	return len(b.b)
}

// ByteSlice returns a defensive copy of the cached bytes.
func (b ByteView) ByteSlice() []byte {
	return cloneBytes(b.b)
}

// String returns the bytes as a string.
func (b ByteView) String() string {
	return string(b.b)
}

func cloneBytes(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
