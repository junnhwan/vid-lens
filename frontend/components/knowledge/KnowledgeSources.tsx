'use client'

import Link from 'next/link'
import type { KnowledgeBase } from '@/lib/types'
import type { CiteRef } from '@/components/Citation'
import { formatTimeRange } from '@/components/Citation'
import { Icon } from '@/components/ui/Icon'
import styles from './KnowledgeWorkspace.module.css'

export function KnowledgeSources({ kb, hitIds }: { kb: KnowledgeBase; hitIds: Set<number> }) {
  return <>
    <header className={styles.heading}>
      <div><span className={styles.eyebrow}>Knowledge / Research</span><h1>{kb.name}</h1></div>
      <div className={styles.actions}><Link className="btn btn-sm btn-ghost" href={`/kb/${kb.id}/retrieval`}><Icon name="search" size="sm" />检索测试台</Link><Link className="btn btn-sm btn-ghost" href={`/kb/${kb.id}`}>管理资料</Link></div>
    </header>
    <div className={styles.shelf} aria-label="知识库来源视频">
      {(kb.videos || []).map((video, i) => <Link href={`/video/${video.task_id}`} key={video.task_id} className={styles.source} data-hit={hitIds.has(video.task_id)}>
        <div className={styles.sourceTop}><span>SOURCE {String(i + 1).padStart(2, '0')}</span><Icon name="video" size="sm" /></div>
        <b title={video.title}>{video.title || `视频 ${video.task_id}`}</b><small>{hitIds.has(video.task_id) ? '已找到相关证据' : video.retrievable ? '可用于研究' : '索引尚未就绪'}</small>
      </Link>)}
      {!kb.videos?.length && <p className={styles.note}>添加视频后，即可在这里开展跨视频研究。</p>}
    </div>
  </>
}

export function KnowledgeEvidence({ cites, onOpen }: { cites: CiteRef[]; onOpen: (cite: CiteRef, all: CiteRef[]) => void }) {
  const groups = new Map<number, CiteRef[]>()
  for (const cite of cites) { const id = cite.taskId || 0; groups.set(id, [...(groups.get(id) || []), cite]) }
  return <div className={styles.groups}>
    {Array.from(groups, ([id, items]) => <section key={id}>
      <h3 className={styles.groupTitle}><Icon name="video" size="sm" />{items[0].videoTitle || `视频 ${id}`} · {items.length} 条</h3>
      {items.map(cite => <button key={cite.id} className={styles.evidence} onClick={() => onOpen(cite, cites)}>
        <span className={styles.meta}>{cite.id} · {cite.timeRangeStatus === 'unknown' ? '时间未知' : formatTimeRange(cite.startMS, cite.endMS)} · {cite.modality === 'transcript' ? '转写' : cite.modality === 'visual_ocr' ? '画面文字' : '画面描述'}</span>
        <p>{(cite.anchorQuote || cite.content || '').slice(0, 180)}</p><span className={styles.note}>查看证据与回放 →</span>
      </button>)}
    </section>)}
    {cites.length === 0 && <p className={styles.note}>研究完成后，引用会按来源视频分组显示，方便逐条核对。</p>}
  </div>
}
