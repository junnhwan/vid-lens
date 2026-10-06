import { useRef,useState,useLayoutEffect,type ReactNode } from 'react'
export function ClampRead({ children, className }: { children: ReactNode; className: string }) {
  const ref = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [canToggle, setCanToggle] = useState(false)

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const measure = () => {
      if (open) return
      setCanToggle(el.scrollHeight > el.clientHeight + 2)
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [children, open])

  return (
    <div className="clamp-read">
      <div ref={ref} className={`${className}${open ? ' open' : ''}`}>{children}</div>
      {canToggle && (
        <button
          type="button"
          className="frame-more"
          onClick={e => { e.stopPropagation(); setOpen(v => !v) }}
        >
          {open ? '收起' : '展开'}
        </button>
      )}
    </div>
  )
}

export function FrameRead({ children }: { children: ReactNode }) {
  return <ClampRead className="frame-read">{children}</ClampRead>
}
