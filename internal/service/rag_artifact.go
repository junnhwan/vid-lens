package service

// RAGVectorManifestEntry is the non-vector metadata frozen from Milvus for
// strict evaluation. EvidenceID is the Milvus primary key (vector_id).
type RAGVectorManifestEntry struct {
	EvidenceID     string
	UserID         int64
	TaskID         int64
	ChunkID        int64
	ChunkIndex     int
	ContentHash    string
	EmbeddingModel string
}
