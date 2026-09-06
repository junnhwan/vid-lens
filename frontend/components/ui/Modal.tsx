'use client'

import { useEffect, useRef } from 'react'
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
  return (
    <Overlay onClose={onClose} confirmOnClose={confirmOnClose}>
      <div className={`modal${className ? ` ${className}` : ''}`} style={width ? { width } : undefined}>
        <div className="modal-head">
          <h3>{title}</h3>
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
