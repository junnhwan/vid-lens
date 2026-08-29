package vector

import (
	"testing"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

func TestParseVectorManifestResultSetUsesStableEvidenceMetadata(t *testing.T) {
	rs := client.ResultSet{
		entity.NewColumnVarChar(fieldVectorID, []string{"evidence-1"}),
		entity.NewColumnInt64(fieldUserID, []int64{7}),
		entity.NewColumnInt64(fieldTaskID, []int64{9}),
		entity.NewColumnInt64(fieldChunkID, []int64{101}),
		entity.NewColumnInt64(fieldChunkIndex, []int64{3}),
		entity.NewColumnVarChar(fieldContentHash, []string{"hash-1"}),
		entity.NewColumnVarChar(fieldEmbeddingModel, []string{"embed-v1"}),
	}
	entries, err := parseVectorManifestResultSet(rs)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].EvidenceID != "evidence-1" || entries[0].ChunkID != 101 || entries[0].ChunkIndex != 3 {
		t.Fatalf("entries = %+v", entries)
	}
}
