package summarydoc

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func ptr[T any](value T) *T { return &value }

func fixture() (Document, ValidationContext) {
	doc := Document{SchemaVersion: SchemaVersion, DocumentID: "document-1", SourceID: "source-1", SourceDigest: "digest-1", MediaRevision: "media-1", PresentationMode: "text", Title: "连接池", Overview: "解释配置、限制和适用条件。", Blocks: []Block{{ID: "pool", Order: 1, Title: "配置", BodyMarkdown: "最大连接数为 20。", SourceRefs: []SourceRef{{SourceID: "source-1", CueIDs: []string{"cue-1", "cue-2"}, StartMS: ptr[int64](120000), EndMS: ptr[int64](135000), TimingMethod: "subtitle_cue"}}}}}
	ctx := ValidationContext{SourceID: doc.SourceID, SourceDigest: doc.SourceDigest, MediaRevision: doc.MediaRevision, GenerationID: "generation-1", Cues: map[string]Cue{"cue-1": {StartMS: ptr[int64](120000), EndMS: ptr[int64](126000), TimingMethod: "subtitle_cue"}, "cue-2": {StartMS: ptr[int64](125000), EndMS: ptr[int64](135000), TimingMethod: "subtitle_cue"}}, Figures: map[string]RegisteredFigure{}}
	return doc, ctx
}

func illustrated() (Document, ValidationContext) {
	doc, ctx := fixture()
	doc.PresentationMode = "image_text"
	doc.Blocks[0].Figures = []Figure{{ID: "figure-1", ScreenshotRef: "screenshot-1", CaptureMS: ptr[int64](127400), Caption: "配置界面", Alt: "显示最大连接数为 20", Supports: "最大连接数设置"}}
	ctx.Figures["screenshot-1"] = RegisteredFigure{SourceID: doc.SourceID, SourceDigest: doc.SourceDigest, MediaRevision: doc.MediaRevision, BlockID: "pool", GenerationID: ctx.GenerationID, CaptureMS: 127400, Inspected: true}
	return doc, ctx
}

func TestStrictParse(t *testing.T) {
	doc, _ := fixture()
	data, _ := CanonicalJSON(doc)
	if _, err := Parse(data); err != nil {
		t.Fatal(err)
	}
	bad := []string{
		strings.Replace(string(data), `"title":"连接池"`, `"title":"连接池","title":"覆盖"`, 1),
		strings.Replace(string(data), `"order":1`, `"order":1,"signed_url":"https://private.test"`, 1),
		string(data) + ` {}`,
		strings.Replace(string(data), `"source_id":"source-1"`, `"source_id":"source-1","source_id":"other"`, 2),
	}
	for _, raw := range bad {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Errorf("accepted invalid JSON: %s", raw)
		}
	}
	if _, err := Parse(append(data, 0xff)); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
}

func TestFrozenSourcesRejectInventedTiming(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Document, *ValidationContext)
	}{
		{"other-source", func(d *Document, c *ValidationContext) { d.SourceID = "other" }},
		{"changed-digest", func(d *Document, c *ValidationContext) { d.SourceDigest = "other" }},
		{"invented-cue", func(d *Document, c *ValidationContext) { d.Blocks[0].SourceRefs[0].CueIDs[0] = "missing" }},
		{"estimated-time", func(d *Document, c *ValidationContext) { d.Blocks[0].SourceRefs[0].StartMS = ptr[int64](120001) }},
		{"false-precision", func(d *Document, c *ValidationContext) { d.Blocks[0].SourceRefs[0].TimingMethod = "word_alignment" }},
		{"null-known-time", func(d *Document, c *ValidationContext) { d.Blocks[0].SourceRefs[0].StartMS = nil }},
		{"invented-known-time", func(d *Document, c *ValidationContext) { c.Cues["cue-2"] = Cue{TimingMethod: "unknown"} }},
		{"duplicate-cue", func(d *Document, c *ValidationContext) { d.Blocks[0].SourceRefs[0].CueIDs[1] = "cue-1" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d, c := fixture()
			test.mutate(&d, &c)
			if err := Validate(d, c); err == nil {
				t.Fatal("accepted invalid source")
			}
		})
	}
	d, c := fixture()
	c.Cues["cue-2"] = Cue{TimingMethod: "unknown"}
	d.Blocks[0].SourceRefs[0].StartMS = nil
	d.Blocks[0].SourceRefs[0].EndMS = nil
	d.Blocks[0].SourceRefs[0].TimingMethod = "unknown"
	if err := Validate(d, c); err == nil {
		t.Fatal("mixed known/unknown cue reference accepted")
	}
	c.Cues["cue-1"] = Cue{TimingMethod: "unknown"}
	if err := Validate(d, c); err != nil {
		t.Fatal(err)
	}
}

func TestFiguresBoundToAuthorizedInspection(t *testing.T) {
	for _, field := range []string{"ref", "capture", "source", "digest", "media", "block", "generation", "inspected"} {
		t.Run(field, func(t *testing.T) {
			d, c := illustrated()
			reg := c.Figures["screenshot-1"]
			switch field {
			case "ref":
				d.Blocks[0].Figures[0].ScreenshotRef = "https://private.test/picture"
			case "capture":
				d.Blocks[0].Figures[0].CaptureMS = ptr[int64](127000)
			case "source":
				reg.SourceID = "other"
			case "digest":
				reg.SourceDigest = "other"
			case "media":
				reg.MediaRevision = "other"
			case "block":
				reg.BlockID = "other"
			case "generation":
				reg.GenerationID = "other"
			case "inspected":
				reg.Inspected = false
			}
			c.Figures["screenshot-1"] = reg
			if err := Validate(d, c); err == nil {
				t.Fatal("accepted unauthorized figure")
			}
		})
	}
	d, c := illustrated()
	if err := Validate(d, c); err != nil {
		t.Fatal(err)
	}
}

func TestTreeAndModeValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Document)
	}{
		{"reserved-title", func(d *Document) { d.Blocks[0].ID = "title" }},
		{"reserved-overview", func(d *Document) { d.Blocks[0].ID = "overview" }},
		{"reserved-summary-title", func(d *Document) { d.Blocks[0].ID = "summary-title" }},
		{"reserved-summary-overview", func(d *Document) { d.Blocks[0].ID = "summary-overview" }},
		{"missing-parent", func(d *Document) { d.Blocks[0].ParentID = ptr("missing") }},
		{"self-cycle", func(d *Document) { d.Blocks[0].ParentID = ptr("pool") }},
		{"cycle", func(d *Document) {
			d.Blocks[0].ParentID = ptr("child")
			d.Blocks = append(d.Blocks, Block{ID: "child", ParentID: ptr("pool")})
		}},
		{"negative-order", func(d *Document) { d.Blocks[0].Order = -1 }},
		{"zero-image-text", func(d *Document) { d.PresentationMode = "image_text" }},
		{"zero-keyframes", func(d *Document) { d.PresentationMode = "keyframes" }},
		{"auto-not-resolved", func(d *Document) { d.PresentationMode = "auto" }},
		{"empty-text", func(d *Document) { d.Overview = ""; d.Blocks[0].BodyMarkdown = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d, c := fixture()
			test.mutate(&d)
			if err := Validate(d, c); err == nil {
				t.Fatal("accepted invalid tree/mode")
			}
		})
	}
	d, c := illustrated()
	d.PresentationMode = "keyframes"
	d.Overview = ""
	d.Blocks[0].BodyMarkdown = ""
	if err := Validate(d, c); err != nil {
		t.Fatal(err)
	}
	d, c = illustrated()
	d.PresentationMode = "text"
	if err := Validate(d, c); err == nil {
		t.Fatal("text mode falsely owns figures")
	}
}

func TestMarkdownResourceSafetyAndLiteralCode(t *testing.T) {
	for _, body := range []string{`<img src="https://x.test/image">`, `![image](https://x.test/image)`, `![image][ref]`, `[run](javascript:alert)`, `[run](java&#115;cript:alert)`, `[run][x]` + "\n[x]: data:text/html,test", `<https://x.test/a?X-Amz-Signature=secret>`, `https://x.test/a?token=secret`, `[secret](https://user:pass@x.test/)`} {
		d, c := fixture()
		d.Blocks[0].BodyMarkdown = body
		if err := Validate(d, c); err == nil {
			t.Errorf("accepted unsafe body %q", body)
		}
	}
	for _, body := range []string{"\\`<img src=x>`", "```bad`info\n<img src=x>\n```"} {
		d, c := fixture()
		d.Blocks[0].BodyMarkdown = body
		if err := Validate(d, c); err == nil {
			t.Errorf("code masking accepted rendered HTML %q", body)
		}
	}
	d, c := fixture()
	d.Blocks[0].BodyMarkdown = "示例 ` <img> `。\n```html\n<img src=\"https://x.test/image\">\n```\n[文档](https://x.test/manual)"
	if err := Validate(d, c); err != nil {
		t.Fatal(err)
	}
	d, c = fixture()
	d.Overview = strings.Repeat("字", 12000)
	if err := Validate(d, c); err != nil {
		t.Fatal(err)
	}
	d.Overview += "字"
	if err := Validate(d, c); err == nil {
		t.Fatal("Unicode limit not enforced")
	}
}

func TestCanonicalHashAndProjection(t *testing.T) {
	doc, ctx := illustrated()
	doc.Blocks = append(doc.Blocks, Block{ID: "child", ParentID: ptr("pool"), Order: 0, Title: "条件", BodyMarkdown: "适用条件。", SourceRefs: []SourceRef{}, Figures: []Figure{}})
	if err := Validate(doc, ctx); err != nil {
		t.Fatal(err)
	}
	hash, _ := Digest(doc)
	copy := clone(doc)
	copy.Blocks[0], copy.Blocks[1] = copy.Blocks[1], copy.Blocks[0]
	copy.Blocks[1].SourceRefs[0].CueIDs = []string{"cue-2", "cue-1"}
	if got, _ := Digest(copy); got != hash {
		t.Fatal("array storage order changed content digest")
	}
	copy.Blocks[1].Figures[0].Caption = "新的图注"
	if got, _ := Digest(copy); got == hash {
		t.Fatal("figure caption missing from digest")
	}
	copy = clone(doc)
	copy.Blocks[0].SourceRefs[0].StartMS = ptr[int64](120002)
	if got, _ := Digest(copy); got == hash {
		t.Fatal("time missing from digest")
	}
	data, _ := CanonicalJSON(doc)
	decoded, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := Digest(decoded); got != hash {
		t.Fatal("roundtrip digest changed")
	}
	text, err := Markdown(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"# 连接池", "## 配置", "### 条件", "图：配置界面", "02:07.400", "02:00.000–02:15.000（subtitle_cue）"} {
		if !strings.Contains(text, required) {
			t.Errorf("missing export text %q", required)
		}
	}
	if strings.Contains(text, "screenshot-1") || strings.Contains(text, "https://") {
		t.Fatal("export leaked resource URL/reference")
	}
	if doc.Blocks[0].SourceRefs[0].CueIDs[0] != "cue-1" {
		t.Fatal("canonicalization mutated input")
	}
}

func TestAtomicDocumentEditsAndPreview(t *testing.T) {
	doc, ctx := illustrated()
	doc.Blocks = append(doc.Blocks, Block{ID: "child", ParentID: ptr("pool"), Order: 0, Title: "条件", BodyMarkdown: "适用条件。"})
	before := clone(doc)
	operations := []Operation{{Op: OpUpdateDocumentTitle, Title: ptr("新版连接池")}, {Op: OpUpdateBlock, BlockID: "pool", BodyMarkdown: ptr("最大连接数是 20，需考虑负载。")}, {Op: OpUpdateFigureCaption, FigureID: "figure-1", Caption: ptr("调整后的配置图注")}, {Op: OpMoveBlock, BlockID: "child", Order: ptr(2)}}
	next, preview, err := Apply(doc, operations, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next.Blocks[0].ID != "pool" || next.Blocks[1].ID != "child" || next.Blocks[1].ParentID != nil {
		t.Fatal("IDs or move not preserved")
	}
	if len(preview.Changes) != 4 || len(preview.AffectedFigureIDs) != 1 || preview.ContentHashKind != HashKind || !strings.Contains(preview.Markdown, "调整后的配置图注") {
		t.Fatalf("incomplete preview: %+v", preview)
	}
	if !reflect.DeepEqual(doc, before) {
		t.Fatal("base changed")
	}
	operations = append(operations, Operation{Op: OpMoveBlock, BlockID: "pool", ParentID: ptr("pool"), Order: ptr(1)})
	if _, _, err := Apply(doc, operations, ctx); err == nil {
		t.Fatal("accepted cyclic operation")
	}
	if !reflect.DeepEqual(doc, before) {
		t.Fatal("failed patch mutated base")
	}
	if _, _, err := Apply(doc, []Operation{{Op: OpUpdateBlock, BlockID: "pool", Title: ptr("配置"), Caption: ptr("sneaky")}}, ctx); err == nil {
		t.Fatal("ignored irrelevant operation field")
	}
	if _, _, err := Apply(doc, []Operation{{Op: OpInsertBlock, Block: &Block{ID: "new", Order: 5, BodyMarkdown: "新增", SourceRefs: []SourceRef{{SourceID: "source-1", CueIDs: []string{"invented"}, TimingMethod: "unknown"}}}}}, ctx); err == nil {
		t.Fatal("insert accepted invented source")
	}
	if _, _, err := Apply(doc, []Operation{{Op: OpUpdateDocumentTitle, Title: ptr(doc.Title)}}, ctx); err == nil {
		t.Fatal("accepted no-op")
	}
}

func TestDeleteSubtreeRemovesFiguresAndPatchHashGuard(t *testing.T) {
	doc, ctx := illustrated()
	doc.Blocks = append(doc.Blocks, Block{ID: "child", ParentID: ptr("pool"), Order: 0, BodyMarkdown: "适用条件"})
	hash, _ := Digest(doc)
	patch := Patch{BaseContentHashKind: HashKind, BaseContentHash: hash, Operations: []Operation{{Op: OpDeleteBlock, BlockID: "pool"}}}
	data, _ := json.Marshal(patch)
	decoded, err := ParsePatch(data)
	if err != nil {
		t.Fatal(err)
	}
	next, preview, err := ApplyPatch(doc, decoded, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Blocks) != 0 || next.PresentationMode != "text" || len(preview.AffectedFigureIDs) != 1 {
		t.Fatal("subtree or figure survived deletion")
	}
	patch.BaseContentHashKind = "markdown-v1"
	if _, _, err := ApplyPatch(doc, patch, ctx); err == nil {
		t.Fatal("compared hashes across kinds")
	}
	patch.BaseContentHashKind = HashKind
	patch.BaseContentHash = "stale"
	if _, _, err := ApplyPatch(doc, patch, ctx); err == nil {
		t.Fatal("applied stale patch")
	}
	data = []byte(`{"base_content_hash_kind":"summary-json-v2","base_content_hash":"x","operations":[{"op":"update_document_title","title":"x","url":"https://x.test"}]}`)
	if _, err := ParsePatch(data); err == nil {
		t.Fatal("patch accepted unknown field")
	}
}
