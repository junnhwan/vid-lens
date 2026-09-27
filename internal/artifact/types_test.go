package artifact

import "testing"

func TestBodyRejectsUntrustedGraphAndReferences(t *testing.T) {
	valid := Body{SchemaVersion: 1, Kind: "study", Title: "课程", Blocks: []Block{{BlockID: "a", Type: "concept", Title: "概念", Content: "解释", ClaimOrigin: "source", EvidenceRefs: []Ref{{EvidenceID: "known", Relation: "supports"}}}}, Warnings: []string{}}
	for _, tc := range []struct {
		name   string
		mutate func(*Body)
	}{
		{"foreign evidence", func(b *Body) { b.Blocks[0].EvidenceRefs = []Ref{{EvidenceID: "other", Relation: "supports"}} }},
		{"cycle", func(b *Body) { p := "a"; b.Blocks[0].ParentID = &p }},
		{"duplicate id", func(b *Body) { b.Blocks = append(b.Blocks, b.Blocks[0]) }},
		{"unsupported schema", func(b *Body) { b.SchemaVersion = 2 }},
		{"uncited source", func(b *Body) { b.Blocks[0].EvidenceRefs = []Ref{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b Body
			if err := Decode([]byte(JSON(valid)), &b); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&b)
			if b.Validate(map[string]bool{"known": true}) == nil {
				t.Fatal("accepted invalid body")
			}
		})
	}
	if err := valid.Validate(map[string]bool{"known": true}); err != nil {
		t.Fatal(err)
	}
	if Decode([]byte(JSON(valid)+` {}`), new(Body)) == nil {
		t.Fatal("accepted trailing JSON")
	}
}
