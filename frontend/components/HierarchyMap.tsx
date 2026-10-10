import { useEffect, useRef, useState } from 'react'
import type { Markmap } from 'markmap-view'
import { escapeMapText } from '@/lib/artifacts/view'
import { Icon } from '@/components/ui/Icon'
import './HierarchyMap.css'
import { useMediaQuery } from '@/components/ui/useMediaQuery'

export interface HierarchyNode { id: string; parent_id: string | null; title: string; references?: number }
interface TreeNode { block: HierarchyNode; children: TreeNode[] }
function hierarchy(nodes: HierarchyNode[]): TreeNode[] {
  const rows = new Map(nodes.map(block => [block.id, { block, children: [] as TreeNode[] }]))
  const roots: TreeNode[] = []
  for (const block of nodes) { const row = rows.get(block.id)!; const parent = block.parent_id ? rows.get(block.parent_id) : null; if (parent) parent.children.push(row); else roots.push(row) }
  return roots
}
interface MapTreeNode { content: string; children: MapTreeNode[] }
function hierarchyMap(title: string, nodes: HierarchyNode[]) {
 const visit = (row: TreeNode): MapTreeNode => ({ content: `<button type="button" data-block-id="${escapeMapText(row.block.id)}">${escapeMapText(row.block.title)}</button>`, children: row.children.map(visit) })
 return {content: escapeMapText(title), children: hierarchy(nodes).map(visit)}
}
export function HierarchyMap({ title, nodes, onSelect, selectedBlock, heading = '把零散知识，连成一张图', label = '学习笔记概念导图', compact = false }: { title: string; nodes: HierarchyNode[]; onSelect: (id: string) => void; selectedBlock?: string | null; heading?: string; label?: string; compact?: boolean }) {
  const svg = useRef<SVGSVGElement>(null)
  const map = useRef<Markmap | null>(null)
  const [error, setError] = useState(false)
  const [ready, setReady] = useState(false)
  const [attempt, setAttempt] = useState(0)
  const narrow = useMediaQuery('(max-width: 600px)')
  const [focused, setFocused] = useState(false)
  function select(id: string) { setFocused(true); onSelect(id) }
  useEffect(() => {
    const element = svg.current
    if (!element || !ready) return
    const highlight = () => {
      const paths = new Set<string>()
      const selected = Array.from(element.querySelectorAll<HTMLElement>('[data-block-id]')).find(button => button.dataset.blockId === selectedBlock)
      const path = selected?.closest('.markmap-node')?.getAttribute('data-path')
      // Markmap exposes the same data-path on nodes and their incoming edges.
      for (const node of element.querySelectorAll('.markmap-node')) {
        const key = node.getAttribute('data-path') || ''
        const related = !!path && (key === path || path.startsWith(`${key}.`) || key.startsWith(`${path}.`))
        if (related) paths.add(key)
        node.classList.toggle('map-muted', focused && !!path && !related)
        node.classList.toggle('map-selected', focused && !!selected && node.contains(selected))
      }
      for (const link of element.querySelectorAll('.markmap-link')) {
        const related = paths.has(link.getAttribute('data-path') || '')
        link.classList.toggle('map-muted', focused && !!path && !related)
        link.classList.toggle('map-related', focused && !!path && related)
      }
    }
    highlight()
    const observer = new MutationObserver(highlight)
    observer.observe(element, { childList: true, subtree: true })
    return () => observer.disconnect()
  }, [selectedBlock, focused, ready, nodes])
  useEffect(() => {
    let disposed = false
    let observer: ResizeObserver | undefined
    setReady(false); setError(false)
    void import('markmap-view').then(async ({ Markmap }) => {
      if (disposed || !svg.current) return
      const colors = getComputedStyle(svg.current)
      const palette = [1, 2, 3].map(i => colors.getPropertyValue(`--map-branch-${i}`).trim())
      const instance = Markmap.create(svg.current, { duration: 0, maxWidth: compact ? 150 : 190, spacingHorizontal: compact ? 30 : 55, spacingVertical: compact ? 10 : 16, paddingX: compact ? 8 : 12, initialExpandLevel: nodes.length > 50 ? 2 : 3, color: node => palette[(node.state.depth || 0) % palette.length] })
      map.current = instance
      await instance.setData(hierarchyMap(title, nodes))
      if (disposed) return
      await instance.fit()
      if (disposed || !svg.current) return
      setReady(true)
      observer = new ResizeObserver(() => { if (!disposed) void instance.fit() })
      observer.observe(svg.current)
    }).catch(() => { if (!disposed) { map.current?.destroy(); map.current = null; setError(true) } })
    return () => { disposed = true; observer?.disconnect(); map.current?.destroy(); map.current = null }
  }, [title, nodes, attempt, compact])
  function outline(nodes: TreeNode[]) {
    return <ul>{nodes.map(({ block, children }) => <li key={block.id}><button aria-pressed={focused && selectedBlock === block.id} onClick={() => select(block.id)}>{block.title}<span>{(block.references || 0) ? `${block.references || 0} 条引用` : '无引用'}</span></button>{children.length > 0 && outline(children)}</li>)}</ul>
  }
  return <div className={`study-map${compact ? ' compact' : ''}`}>
    {!compact && <div className="map-heading"><div><p className="product-eyebrow">CONNECTED UNDERSTANDING</p><h2>{heading}</h2><p>{nodes.length} 个节点 · 与正文共用内容 · 点击节点查看依据</p></div></div>}
    <div className="map-canvas">
      {error ? <div className="map-error" role="alert"><Icon name="alert" /><b>导图暂时无法显示</b><p>笔记内容仍可通过下方文字大纲查看，并可选择节点核对引用。</p><button className="btn btn-sm" onClick={() => { setError(false); setAttempt(value => value + 1) }}>重试布局</button></div> : <>
        {!ready && <div className="map-layout-loading" role="status"><span className="skel root" /><span className="skel branch one" /><span className="skel branch two" /><span className="skel branch three" /><p>正在整理导图布局…</p></div>}
        <svg ref={svg} aria-label={label} role="group" onClick={event => { const target = event.target as Element; const id = target.closest('[data-block-id]')?.getAttribute('data-block-id'); if (id) select(id) }} />
        {!compact && <div className="map-controls">{focused && <button aria-label="显示完整导图" onClick={() => setFocused(false)}><Icon name="layers" size="sm" /></button>}<button aria-label="缩小导图" disabled={!ready} onClick={() => void map.current?.rescale(.8)}>−</button><button aria-label="放大导图" disabled={!ready} onClick={() => void map.current?.rescale(1.25)}>+</button><button aria-label="适应画布" disabled={!ready} onClick={() => void map.current?.fit()}><Icon name="refresh" size="sm" /></button></div>}
      </>}
    </div>
    {!compact && <p className="map-caption">{nodes.length > 50 ? '大图默认收起章节，点击连接点展开；也可在文字大纲中选择任意节点。' : '结构来自当前正文。引用只说明来源关联，内容与关系仍需回到视频核对。'}</p>}
    {(!compact || error) && <details className="map-outline" open={error || narrow}><summary>文字大纲 · 键盘也可选择节点</summary>{outline(hierarchy(nodes))}</details>}
  </div>
}
