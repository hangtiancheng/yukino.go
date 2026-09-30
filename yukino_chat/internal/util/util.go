package util

import (
	"crypto/rand"
)

const idAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// GetNowAndLenRandomString returns a random identifier of the given length
// drawn from crypto/rand. A unique index on the uuid field backs this up
// against the (negligible) collision probability.
func GetNowAndLenRandomString(length int) string {
	buf := make([]byte, length)
	// crypto/rand.Read never returns an error; it fills buf entirely.
	_, _ = rand.Read(buf)
	for i := range buf {
		buf[i] = idAlphabet[int(buf[i])%len(idAlphabet)]
	}
	return string(buf)
}
