# Editable knowledge canvas — R3 contract

Contract version: 1 (2026-09-28). This extends the [artifact API](artifact-api-contract.md) and [R1 editing contract](artifact-editing-contract.md). Routes use `/api/v1`, require the existing owner-scoped JWT, and keep demo accounts read-only.

## Content authority

The immutable artifact version remains the only authority for note text, block hierarchy, references, and semantic relations. Notes, Markmap, and the editable canvas read the same blocks. A canvas node stores only a stable `block_id` and presentation state; dragging a node does not change `parent_id` or any factual claim. Hiding and collapsing affect visibility, not content deletion. A card rename joins the shared note draft and creates a content version only through its normal save. Agent content edits use the R1 edit-run, patch, proposal/apply, diff, and undo path.

Old `study` bodies with `schema_version: 1` remain readable. A semantic relation upgrades the body to schema 2 and adds an optional `relations` array; it does not duplicate block text:

```json
{
  "schema_version": 2,
  "kind": "study",
  "title": "Example",
  "blocks": [
    {"block_id":"concept-a","parent_id":null,"type":"concept","title":"A","content":"First concept","claim_origin":"user","evidence_refs":[]},
    {"block_id":"concept-b","parent_id":null,"type":"concept","title":"B","content":"Second concept","claim_origin":"user","evidence_refs":[]}
  ],
  "relations": [
    {
      "id": "stable-relation-id",
      "source_block_id": "concept-a",
      "target_block_id": "concept-b",
      "type": "related_to",
      "origin": "user",
      "evidence_refs": []
    }
  ],
  "warnings": []
}
```

Types are `related_to`, `depends_on`, and `contrasts_with`; origins are `user` and `synthesis`. Both endpoints must exist and differ. Relations have unique IDs; symmetric types reject reversed duplicates, and directed dependencies reject cycles. A synthesis relation requires a frozen-manifest evidence reference. The body permits at most 300 relations within the existing 512 KiB body limit. Go validates the manifest reference set and structural rules; frontend Zod checks the structural rules before submission. Deleting or merging blocks removes or remaps their relations; export and answer import preserve the schema.

R3 extends R1 patches with `add_relation`, `remove_relation`, and `group_siblings`. Relation edits are content revisions with the same frozen target, selected-block scope, head CAS, evidence checks, stable operation identity, and three-way undo rules as other patch operations. Grouping inserts a parent above existing siblings while keeping their text, evidence, and IDs. A model cannot supply HTML, JavaScript, CSS, SQL, or React Flow data for execution.

## Independent layout

The layout is an owner-scoped, append-only projection keyed by artifact, content version, and `view_id: "knowledge"`. Each save advances its own `revision` without changing the artifact head. The saved shape is:

```json
{
  "direction": "RIGHT",
  "algorithm": "elk-layered-v1",
  "nodes": {
    "concept-a": {
      "position": {"x": 0, "y": 0},
      "width": 254,
      "height": 132,
      "pinned": true,
      "hidden": false,
      "collapsed": false,
      "style": "auto"
    }
  },
  "viewport": {"x": 0, "y": 0, "zoom": 1}
}
```

Directions are `RIGHT` and `DOWN`. Style is a bounded token (`auto|section|concept|operation|command|note`); coordinates, dimensions, viewport, node IDs, node count, and total JSON size are validated. Arbitrary frontend code and style strings are not accepted. A new content version inherits layout entries only for surviving stable block IDs; its layout revision starts at zero until saved. Pinned nodes retain coordinates during local or full automatic layout.

| Route | Request | Result |
| --- | --- | --- |
| `GET /artifacts/:id/canvas-layout?version_id=...` | Optional positive `revision` retrieves an earlier layout revision. | `{content_version_id,view_id,revision,layout}`. A missing current layout returns revision 0 and a default or migrated layout. |
| `PATCH /artifacts/:id/canvas-layout` | `{content_version_id,expected_revision,layout}` plus `Idempotency-Key`. | The new layout revision. A stale revision or content head returns `version_conflict` (409); a key replay with a different payload returns `idempotency_conflict` (409). |
| `POST /artifacts/:id/canvas-layout/suggest` | `{instruction,content_version_id,expected_head_version,expected_layout_revision,selected_block_id}`. | A bounded plan `{direction,scope,density,summary}`; this call does not save a layout. |

The AI planner returns only direction, all/selected scope, and comfortable/compact density. The client calculates positions with ELK, checks for collisions, and rechecks the content version, body identity, and layout revision before saving. The service also rechecks head and layout revision after the model returns. A late result or stale second tab therefore cannot silently overwrite newer content or layout. Failed layout computation or save leaves the prior layout available.

## Compatibility and verification

The canvas is a third view alongside notes and Markmap. Its components and ELK engine load only when needed; the outline remains a keyboard-accessible reading fallback. Content changes still create immutable versions, while layout changes never count as evidence. Implementation limits and acceptance evidence are tracked privately in `docs-private/agent-editing-canvas-acceptance-2026-09-28.md` and `docs-private/r3-knowledge-canvas-implementation-2026-09-28.md`.
