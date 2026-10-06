import type { RAGIndexResult } from '@/lib/types'
import { Icon } from '@/components/ui/Icon'
import { fmtDateTime,fmtRelTime } from '@/lib/format'
import { indexPhase,indexActionLabel } from './videoView'
export function RetrievalIndexPanel({index,busy,onBuild}:{index:RAGIndexResult|null;busy:string;onBuild:(index:RAGIndexResult)=>void}) {

    if (!index) {
      return (
        <div className="empty">
          <Icon name="layers" size="lg" />
          <b>索引状态不可用</b>
        </div>
      )
    }
    const stateView = index.indexed
      ? { chip: 'chip-ok', text: '已建立' }
      : index.status === 'indexing'
        ? { chip: 'chip-acc', text: '构建中' }
        : index.status === 'queued'
          ? { chip: 'chip-mute', text: '排队中' }
          : index.status === 'failed'
            ? { chip: 'chip-bad', text: '失败' }
      : index.needs_rebuild
        ? { chip: 'chip-warn', text: '需要重建' }
        : { chip: 'chip-mute', text: '未建立' }
    return (
      <>
        <div className="idx-list">
          <div className="idx-row"><span className="k">状态</span><span className="v"><span className={`chip ${stateView.chip}`}>{stateView.text}</span></span></div>
          <div className="idx-row"><span className="k">阶段</span><span className="v">{indexPhase(index)}</span></div>
          <div className="idx-row"><span className="k">证据块</span><span className="v mono">{index.status === 'indexing' && index.total_chunks > 0 ? `${index.completed_chunks} / ${index.total_chunks} 块已完成 Embedding` : `${index.chunks} 块`}</span></div>
          <div className="idx-row"><span className="k">向量模型</span><span className="v mono">{index.embedding_model || '—'}</span></div>
          {index.next_retry_at && <div className="idx-row"><span className="k">下次重试</span><span className="v">{fmtDateTime(index.next_retry_at)}</span></div>}
          {index.progress_at && <div className="idx-row"><span className="k">最近进度</span><span className="v">{fmtRelTime(index.progress_at)}</span></div>}
          {index.last_error && (
            <div className="idx-row"><span className="k">最近错误</span><span className="v" style={{ color: 'var(--bad)', fontSize: 12 }}>{index.last_error}</span></div>
          )}
        </div>
        {index.needs_rebuild && (
          <p style={{ fontSize: 13, color: 'var(--tx-4)', marginTop: 12 }}>索引已过期,需要重建</p>
        )}
        <button
          className="btn btn-sm"
          style={{ marginTop: 14 }}
          disabled={busy !== '' || index.status === 'indexing' || index.status === 'queued'}
          onClick={() => onBuild(index)}
        >
          <Icon name="layers" size="sm" />
          {indexActionLabel(index)}
        </button>
      </>
    )
  }
