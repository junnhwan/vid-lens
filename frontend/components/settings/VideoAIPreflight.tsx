import { useCallback, useState } from 'react'
import { Icon } from '@/components/ui/Icon'
import { Modal } from '@/components/ui/Modal'
import { useAIAvailability } from './useAIAvailability'
import './VideoAIPreflight.css'

interface PendingAction {
  label: string
  run: () => void
}

/** Keeps an AI video operation behind one shared, explicit configuration check. */
export function useVideoAIPreflight() {
  const [pending, setPending] = useState<PendingAction | null>(null)

  const request = useCallback((label: string, run: () => void) => {
    setPending({ label, run })
  }, [])

  const close = useCallback(() => setPending(null), [])
  const continueAction = useCallback(() => {
    if (!pending) return
    const run = pending.run
    setPending(null)
    run()
  }, [pending])

  const dialog = pending && (
    <VideoAIPreflight
      actionLabel={pending.label}
      onClose={close}
      onContinue={continueAction}
    />
  )

  return { request, dialog }
}

function VideoAIPreflight({ actionLabel, onClose, onContinue }: {
  actionLabel: string
  onClose: () => void
  onContinue: () => void
}) {
  const ai = useAIAvailability()
  const loading = ai.isPending || ai.isFetching
  const canContinue = ai.ready && !loading

  return (
    <Modal
      title="视频处理前检查"
      onClose={onClose}
      width={540}
      className="video-ai-preflight"
      footer={(
        <>
          <button type="button" className="btn" onClick={onClose}>返回</button>
          {canContinue ? (
            <button type="button" className="btn btn-primary" onClick={onContinue}>
              <Icon name="play" size="sm" />继续{actionLabel}
            </button>
          ) : (
            <>
              <button type="button" className="btn" disabled={loading} onClick={() => void ai.refetch()}>
                {loading ? '检查中…' : '重新检查'}
              </button>
              <a className="btn btn-primary" href="/settings" target="_blank" rel="noopener noreferrer">
                <Icon name="settings" size="sm" />配置 AI
              </a>
            </>
          )}
        </>
      )}
    >
      <div className="video-ai-preflight-hero">
        <div className="video-ai-preflight-emblem" aria-hidden="true"><Icon name="cpu" /></div>
        <div>
          <span className="video-ai-preflight-kicker">VIDLENS · PROCESS CHECK</span>
          <h2>先确认 AI 能力，再开始处理</h2>
          <p>这一步会启动视频相关操作。确认默认配置包含下面四项模型，转写、检索和画面分析才能顺利衔接。</p>
        </div>
      </div>

      <div className="video-ai-preflight-action">
        <span>即将开始</span>
        <strong>{actionLabel}</strong>
        {ai.profileName && <small>默认配置 · {ai.profileName}</small>}
      </div>

      <div className="video-ai-preflight-list" aria-label="AI 模型配置状态">
        {ai.capabilities.map(capability => (
          <div className={`video-ai-preflight-item${capability.ready ? ' ready' : ''}`} key={capability.key}>
            <span className="video-ai-preflight-status" aria-hidden="true">
              <Icon name={capability.ready ? 'check' : 'alert'} size="sm" />
            </span>
            <span className="video-ai-preflight-copy">
              <strong>{capability.label}</strong>
              <small>{capability.model || '尚未配置模型'}</small>
            </span>
            <span className="video-ai-preflight-state">{capability.ready ? '已就绪' : '待配置'}</span>
          </div>
        ))}
      </div>

      {canContinue ? (
        <p className="video-ai-preflight-note ready-note" role="status">
          <Icon name="check" size="sm" />配置已齐全。实际模型调用可能消耗服务额度。
        </p>
      ) : (
        <p className="video-ai-preflight-note" role={ai.error ? 'alert' : 'status'}>
          <Icon name={ai.error ? 'alert' : 'info'} size="sm" />
          {loading ? '正在读取默认 AI 配置…' : ai.error ? ai.reason : ai.reason || '请先补齐默认 AI 配置。'}
        </p>
      )}
    </Modal>
  )
}
