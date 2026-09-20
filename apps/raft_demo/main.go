// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package main

import "github.com/hangtiancheng/yukino.go/apps/raft_demo/raft"

func main() {
	// Channel for write proposals
	proposeC := make(chan string)
	// Channel for configuration change proposals
	confChangeC := make(chan raft.ConfChange)

	// Create raft proxy and obtain the commit channel
	commitC := newRaftProxy(1, []string{"node1"}, proposeC, confChangeC)
	// Create the key-value store application
	kvStore := newKVStore(proposeC, commitC)

	// Start the HTTP API server
	s := newService(kvStore, proposeC, confChangeC)
	serveHttpApi(8091, s)
}
