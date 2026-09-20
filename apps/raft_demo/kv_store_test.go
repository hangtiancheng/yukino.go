package main

import "testing"

// TestKVStoreReadCommitSkipsMalformed verifies that the commit-apply loop
// skips nil and non-JSON payloads instead of applying zero-valued entries.
func TestKVStoreReadCommitSkipsMalformed(t *testing.T) {
	k := &kvStore{core: make(map[string]string)}

	good := `{"key":"a","val":"1"}`
	bad := "not-json"
	commitC := make(chan *string)
	done := make(chan struct{})

	go func() {
		k.readCommit(commitC)
		close(done)
	}()

	commitC <- &good
	commitC <- &bad
	commitC <- nil
	close(commitC)
	<-done // readCommit returned, so all entries were processed

	k.RLock()
	defer k.RUnlock()

	if k.core["a"] != "1" {
		t.Fatalf(`core["a"] = %q, want "1"`, k.core["a"])
	}
	if _, ok := k.core[""]; ok {
		t.Fatal("malformed entry must be skipped, not applied under the empty key")
	}
}
