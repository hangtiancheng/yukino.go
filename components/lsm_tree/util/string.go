package util

func SharedPrefixLen(a, b []byte) int {
	var i int
	for ; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			break
		}
	}
	return i
}

func GetSeparatorBetween(a, b []byte) []byte {
	if len(b) == 0 {
		return b
	}

	if len(a) == 0 {
		separator := make([]byte, len(b))
		copy(separator, b)
		return append(separator[:len(b)-1], separator[len(b)-1]-1)
	}

	return a
}
