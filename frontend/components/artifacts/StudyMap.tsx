import { useEffect, useRef, useState } from 'react'
import type { Markmap } from 'markmap-view'
import type { StudyBody } from '@/lib/artifacts/schema'
import { blockTree, mapTree, type BlockNode } from '@/lib/artifacts/view'
import { Icon } from '@/components/ui/Icon'
import { useMediaQuery } from '@/components/ui/useMediaQuery'

export function StudyMap({ body, onSelect }: { body: StudyBody; onSelect: (id: string) => void }) {
  const svg = useRef<SVGSVGElement>(null)
  const map = useRef<Markmap | null>(null)
  const [error, setError] = useState(false)
  const [ready, setReady] = useState(false)
  const narrow = useMediaQuery('(max-width: 600px)')
  useEffect(() => {
    let disposed = false
    let observer: ResizeObserver | undefined
    setReady(false); setError(false)
    void import('markmap-view').then(async ({ Markmap }) => {
      if (disposed || !svg.current) return
      const colors = getComputedStyle(svg.current)
      const palette = [1, 2, 3].map(i => colors.getPropertyValue(`--map-branch-${i}`).trim())
      const instance = Markmap.create(svg.current, { duration: 0, maxWidth: 190, spacingHorizontal: 55, spacingVertical: 16, paddingX: 12, initialExpandLevel: body.blocks.length > 50 ? 2 : 3, color: node => palette[(node.state.depth || 0) % palette.length] })
      map.current = instance
      await instance.setData(mapTree(body))
      if (disposed) return
      await instance.fit()
      if (disposed || !svg.current) return
      setReady(true)
      observer = new ResizeObserver(() => { if (!disposed) void instance.fit() })
      observer.observe(svg.current)
    }).catch(() => { if (!disposed) setError(true) })
    return () => { disposed = true; observer?.disconnect(); map.current?.destroy(); map.current = null }
  }, [body])
  function outline(nodes: BlockNode[]) {
    return <ul>{nodes.map(({ block, children }) => <li key={block.block_id}><button onClick={() => onSelect(block.block_id)}>{block.title}<span>{block.evidence_refs.length ? `${block.evidence_refs.length} 条引用` : '无引用'}</span></button>{children.length > 0 && outline(children)}</li>)}</ul>
  }
  return <div className="study-map">
    <div className="map-heading"><div><p className="product-eyebrow">CONNECTED UNDERSTANDING</p><h2>把零散知识，连成一张图</h2><p>{body.blocks.length} 个节点 · 与笔记共用内容 · 点击节点查看依据</p></div></div>
    <div className="map-canvas">
      {error ? <p role="alert">导图暂时无法显示，可以使用下方文字大纲。</p> : <>
        {!ready && <p className="map-loading" role="status">正在整理导图布局…</p>}
        <svg ref={svg} aria-label="学习笔记概念导图" role="group" onClick={event => { const target = event.target as Element; const id = target.closest('[data-block-id]')?.getAttribute('data-block-id'); if (id) onSelect(id) }} />
        <div className="map-controls"><button aria-label="缩小导图" disabled={!ready} onClick={() => void map.current?.rescale(.8)}>−</button><button aria-label="放大导图" disabled={!ready} onClick={() => void map.current?.rescale(1.25)}>+</button><button aria-label="适应画布" disabled={!ready} onClick={() => void map.current?.fit()}><Icon name="refresh" size="sm" /></button></div>
      </>}
    </div>
    <p className="map-caption">{body.blocks.length > 50 ? '大图默认收起章节，点击连接点展开；也可在文字大纲中选择任意节点。' : '结构来自笔记。引用只说明来源关联，内容与关系仍需回到视频核对。'}</p>
    <details className="map-outline" open={error || narrow}><summary>文字大纲 · 键盘也可选择节点</summary>{outline(blockTree(body))}</details>
  </div>
}
