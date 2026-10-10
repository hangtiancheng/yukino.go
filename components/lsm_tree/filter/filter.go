package filter

type Filter interface {
	Add(key []byte)
	Exist(bitmap, key []byte) bool
	Hash() []byte
	Reset()
	KeyLen() int
}
