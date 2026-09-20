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

// Package consts defines shared constants used across the yukino_agent application.
package consts

const (
	// RedisKeyPrefix is the key prefix for document hashes in Redis.
	// Aligned with the Next.js keyPrefix "biz:" so the two backends share the
	// same keyspace when needed (DEC-1=A: Go takes over and flushes/rebuilds).
	RedisKeyPrefix = "biz:"

	// RedisIndexName is the RediSearch index name used for vector search.
	RedisIndexName = "idx:biz"

	// RedisContentField stores the document content (indexed as TEXT).
	RedisContentField = "content"

	// RedisVectorField stores the content embedding (indexed as VECTOR).
	// Aligned with the Next.js vector field name "vector" (the Eino redis
	// indexer default is "vector_content"; we override it via DocumentToHashes
	// so both backends use the same field).
	RedisVectorField = "vector"

	// RedisSourceField stores the document source path (indexed as TAG, used for dedup).
	RedisSourceField = "_source"

	// MaxContentLength caps the stored content length (in bytes) to match the
	// Next.js MAX_CONTENT_LENGTH = 8192. Redis TEXT has no built-in limit, so we
	// truncate here before storing/embedding.
	MaxContentLength = 8192
)
