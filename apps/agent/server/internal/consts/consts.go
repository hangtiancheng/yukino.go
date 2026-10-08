// Package consts defines shared constants used across the yukino_agent application.
package consts

const (
	// MilvusIDField is the primary key field storing the document id (VarChar).
	MilvusIDField = "id"

	// MilvusVectorField stores the content embedding (FloatVector, COSINE).
	MilvusVectorField = "vector"

	// MilvusContentField stores the document content (VarChar).
	MilvusContentField = "content"

	// MilvusMetadataField stores the JSON-serialized document metadata.
	MilvusMetadataField = "metadata"

	// MilvusSourceKey is the metadata key holding the document source file
	// name, used to deduplicate chunks when a file is re-indexed.
	MilvusSourceKey = "_source"

	// MaxContentLength caps the stored content length (in bytes) and is also
	// the max_length of the Milvus content VarChar field.
	MaxContentLength = 8192
)
