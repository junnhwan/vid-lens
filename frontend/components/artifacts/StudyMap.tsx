import { useMemo } from 'react'
import type { StudyBody } from '@/lib/artifacts/schema'
import { HierarchyMap } from '@/components/HierarchyMap'

export function StudyMap({ body, onSelect, selectedBlock }: { body: StudyBody; onSelect: (id: string) => void; selectedBlock?: string | null }) {
 const nodes = useMemo(() => body.blocks.map(block => ({ id: block.block_id, parent_id: block.parent_id, title: block.title, references: block.evidence_refs.length })), [body])
 return <HierarchyMap title={body.title} nodes={nodes} onSelect={onSelect} selectedBlock={selectedBlock} />
}
