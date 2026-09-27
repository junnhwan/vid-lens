import { useEffect, useId, useRef } from 'react'
import { Icon } from '@/components/ui/Icon'

const DEFAULT_CONFIRM = '关闭后已填写的内容会丢失,确定关闭?'

function shouldClose(confirmOnClose?: boolean | string | (() => boolean)): boolean {
  if (confirmOnClose == null || confirmOnClose === false) return true
  if (typeof confirmOnClose === 'function') return confirmOnClose()
  const msg = typeof confirmOnClose === 'string' ? confirmOnClose : DEFAULT_CONFIRM
  return window.confirm(msg)
}

/** 遮罩:只有 pointerdown 和 pointerup 都落在遮罩上才关闭,避免弹窗内拖出误关。 */
export function Overlay({
  children,
  onClose,
  confirmOnClose,
}: {
  children: React.ReactNode
  onClose: () => void
  confirmOnClose?: boolean | string | (() => boolean)
}) {
  const downOnBackdrop = useRef(false)

  const tryClose = () => {
    if (!shouldClose(confirmOnClose)) return
    onClose()
  }

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        if (!shouldClose(confirmOnClose)) return
        onClose()
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onClose, confirmOnClose])

  return (
    <div
      className="overlay"
      onPointerDown={e => { downOnBackdrop.current = e.target === e.currentTarget }}
      onPointerUp={e => {
        if (downOnBackdrop.current && e.target === e.currentTarget) tryClose()
        downOnBackdrop.current = false
      }}
    >
      {children}
    </div>
  )
}

export function Modal({
  title,
  onClose,
  confirmOnClose,
  children,
  footer,
  width,
  className,
}: {
  title: string
  onClose: () => void
  confirmOnClose?: boolean | string | (() => boolean)
  children: React.ReactNode
  footer?: React.ReactNode
  width?: number | string
  className?: string
}) {
  const labelId = useId()
  const boxRef = useRef<HTMLDivElement | null>(null)

  // 打开时焦点移入首个可交互元素(无可聚焦项则落在容器),关闭归还给触发元素。
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null
    const el = boxRef.current
    const focusInside = () => {
      if (!el) return
      const first = el.querySelector<HTMLElement>(
        '.modal-body input:not([disabled]), .modal-body textarea:not([disabled]), .modal-body select:not([disabled]), .modal-body button:not([disabled]), .modal-body a[href], .modal-body summary, .modal-body [tabindex]:not([tabindex="-1"])',
      )
      ;(first ?? el).focus()
    }
    // Wait until responsive containers have been laid out before moving focus.
    const frame = requestAnimationFrame(focusInside)
    const containFocus = (event: FocusEvent) => {
      const dialogs = document.querySelectorAll('[role="dialog"]')
      if (el && dialogs[dialogs.length - 1] === el && !el.contains(event.target as Node)) focusInside()
    }
    document.addEventListener('focusin', containFocus)
    return () => { cancelAnimationFrame(frame); document.removeEventListener('focusin', containFocus); prev?.focus?.() }
  }, [])

  return (
    <Overlay onClose={onClose} confirmOnClose={confirmOnClose}>
      <div
        ref={boxRef}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-labelledby={labelId}
        onKeyDown={event => {
          if (event.key !== 'Tab') return
          const controls = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('button:not([disabled]), a[href], input:not([disabled]), textarea:not([disabled]), select:not([disabled]), summary, [tabindex="0"]')).filter(element => element.offsetParent !== null)
          const first = controls[0], last = controls[controls.length - 1]
          if (!first) { event.preventDefault(); return }
          if (event.shiftKey && (document.activeElement === first || document.activeElement === event.currentTarget)) { event.preventDefault(); last.focus() }
          else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
        }}
        className={`modal${className ? ` ${className}` : ''}`}
        style={width ? { width } : undefined}
      >
        <div className="modal-head">
          <h3 id={labelId}>{title}</h3>
          <button className="btn btn-ic btn-ghost" onClick={() => { if (shouldClose(confirmOnClose)) onClose() }} aria-label="关闭">
            <Icon name="x" />
          </button>
        </div>
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-foot">{footer}</div>}
      </div>
    </Overlay>
  )
}

export function ConfirmModal({
  title,
  children,
  confirmLabel = '确定',
  danger,
  busy,
  onConfirm,
  onClose,
}: {
  title: string
  children: React.ReactNode
  confirmLabel?: string
  danger?: boolean
  busy?: boolean
  onConfirm: () => void
  onClose: () => void
}) {
  return (
    <Modal
      title={title}
      onClose={onClose}
      width={420}
      footer={(
        <>
          <button className="btn" disabled={busy} onClick={onClose}>取消</button>
          <button
            className={danger ? 'btn btn-danger' : 'btn btn-primary'}
            disabled={busy}
            onClick={onConfirm}
          >
            {busy ? '处理中…' : confirmLabel}
          </button>
        </>
      )}
    >
      <p style={{ fontSize: 13.5, color: 'var(--tx-2)', lineHeight: 1.65, margin: 0 }}>{children}</p>
    </Modal>
  )
}

export function DrawerVeil({ onClose }: { onClose: () => void }) {
  const downOnBackdrop = useRef(false)

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onClose])

  return (
    <div
      className="drawer-veil"
      onPointerDown={e => { downOnBackdrop.current = e.target === e.currentTarget }}
      onPointerUp={e => {
        if (downOnBackdrop.current && e.target === e.currentTarget) onClose()
        downOnBackdrop.current = false
      }}
    />
  )
}
