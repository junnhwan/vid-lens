import { useEffect, useMemo, useRef, useState } from 'react'

export function ModelCombobox({
  value,
  onChange,
  models,
  placeholder,
  id,
  label,
}: {
  value: string
  onChange: (v: string) => void
  models: string[]
  placeholder?: string
  id: string
  label: string
}) {
  const [open, setOpen] = useState(false)
  const [highlight, setHighlight] = useState(0)
  const wrapRef = useRef<HTMLDivElement>(null)

  const filtered = useMemo(() => {
    const q = value.trim().toLowerCase()
    if (!q) return models
    return models.filter(m => m.toLowerCase().includes(q))
  }, [models, value])

  useEffect(() => { setHighlight(0) }, [filtered])

  useEffect(() => {
    const onDoc = (e: MouseEvent) => {
      if (!wrapRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [])

  const pick = (m: string) => {
    onChange(m)
    setOpen(false)
  }

  return (
    <div className="combo" ref={wrapRef}>
      <input
        id={id}
        className="input"
        role="combobox"
        aria-label={label}
        aria-autocomplete="list"
        aria-expanded={open && models.length > 0}
        aria-controls={id + '-options'}
        aria-activedescendant={open && filtered[highlight] ? id + '-option-' + highlight : undefined}
        value={value}
        placeholder={placeholder || '模型'}
        onChange={e => { onChange(e.target.value); setOpen(true) }}
        onFocus={() => { if (models.length > 0) setOpen(true) }}
        onKeyDown={e => {
          if (!open && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
            setOpen(true)
            return
          }
          if (e.key === 'ArrowDown') {
            e.preventDefault()
            setHighlight(i => Math.min(filtered.length - 1, i + 1))
          } else if (e.key === 'ArrowUp') {
            e.preventDefault()
            setHighlight(i => Math.max(0, i - 1))
          } else if (e.key === 'Enter' && open && filtered[highlight]) {
            e.preventDefault()
            pick(filtered[highlight])
          } else if (e.key === 'Escape') {
            setOpen(false)
          }
        }}
      />
      {open && models.length > 0 && (
        <div id={id + '-options'} className="combo-list" role="listbox" aria-label={label + '列表'}>
          {filtered.length === 0 && (
            <div className="combo-empty">没有匹配「{value}」的模型</div>
          )}
          {filtered.map((m, i) => (
            <button
              id={id + '-option-' + i}
              key={m}
              type="button"
              role="option"
              aria-selected={i === highlight}
              className={i === highlight ? 'on' : ''}
              onMouseEnter={() => setHighlight(i)}
              onMouseDown={e => e.preventDefault()}
              onClick={() => pick(m)}
            >
              {m}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
