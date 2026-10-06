import { useCallback, useState } from 'react'
import { Icon } from '@/components/ui/Icon'
import { Modal } from '@/components/ui/Modal'
import { useAIAvailability } from './useAIAvailability'
import type { CapabilityActionKey } from '@/lib/types'
import './VideoAIPreflight.css'

interface PendingAction {
  label: string
  run: () => void
  action: CapabilityActionKey
}

/** Keeps an AI video operation behind one shared, explicit configuration check. */
export function useVideoAIPreflight(defaultAction: CapabilityActionKey = 'chat') {
  const [pending, setPending] = useState<PendingAction | null>(null)

  const request = useCallback((label: string, run: () => void, action: CapabilityActionKey = defaultAction) => {
    if (action === 'upload') { run(); return }
    setPending({ label, run, action })
  }, [defaultAction])

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
      action={pending.action}
      onClose={close}
      onContinue={continueAction}
    />
  )

  return { request, dialog }
}

function VideoAIPreflight({ actionLabel, action, onClose, onContinue }: {
  actionLabel: string
  action: CapabilityActionKey
  onClose: () => void
  onContinue: () => void
}) {
  const ai = useAIAvailability(false, action)
  const loading = ai.isPending || ai.isFetching
  const canContinue = ai.ready && !loading
  const local = action === 'ocr' || action === 'align'

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
                <Icon name="settings" size="sm" />{local ? '查看能力设置' : '配置 AI'}
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
          <h2>先确认所需能力，再开始处理</h2>
          <p>只检查本次操作需要的能力。配置完整表示可以提交，真实服务和本地模型状态仍以执行结果为准。</p>
        </div>
      </div>

      <div className="video-ai-preflight-action">
        <span>即将开始</span>
        <strong>{actionLabel}</strong>
        {ai.profileName && <small>默认配置 · {ai.profileName}</small>}
      </div>

      <div className="video-ai-preflight-list" aria-label="能力配置状态">
        {ai.capabilities.map(capability => (
          <div className={`video-ai-preflight-item${capability.ready ? ' ready' : ''}`} key={capability.key}>
            <span className="video-ai-preflight-status" aria-hidden="true">
              <Icon name={capability.ready ? 'check' : 'alert'} size="sm" />
            </span>
            <span className="video-ai-preflight-copy">
              <strong>{capability.label}</strong>
              <small>{capability.model || (local ? capability.ready ? '本地工具已配置' : '请检查服务端依赖' : '尚未配置模型')}</small>
            </span>
            <span className="video-ai-preflight-state">{capability.ready ? '已就绪' : '待配置'}</span>
          </div>
        ))}
      </div>

      {canContinue ? (
        <p className="video-ai-preflight-note ready-note" role="status">
          <Icon name="check" size="sm" />本次操作所需配置已齐全。{local ? '本地工具将在提交后检查并执行。' : '实际模型调用可能消耗服务额度。'}
        </p>
      ) : (
        <p className="video-ai-preflight-note" role={ai.error ? 'alert' : 'status'}>
          <Icon name={ai.error ? 'alert' : 'info'} size="sm" />
          {loading ? '正在读取所需能力配置…' : ai.reason || '请先补齐所需能力配置。'}
        </p>
      )}
    </Modal>
  )
}
