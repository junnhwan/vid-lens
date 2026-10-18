import { useEffect, useId, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react"
import { ModelIcon, resolveBrand } from "./brandMarks"

function ChoiceMark({ brand, name }: { brand?: string; name?: string }) {
  if (resolveBrand(brand, name) === "generic" && !brand) return null
  return <ModelIcon brand={brand} name={name} decorative />
}

export type ChoiceBadge = {
  label: string
  note?: string
}

export type ChoiceOption = {
  id: string
  label: string
  hint?: string
  brand?: string
  disabled?: boolean
  mark?: ChoiceBadge
}

export function ChoiceMenu({
  label,
  value,
  options,
  placeholder,
  disabled,
  unavailable,
  align = "start",
  onChange,
}: {
  label: string
  value: string
  options: ChoiceOption[]
  placeholder: string
  disabled?: boolean
  unavailable?: string
  align?: "start" | "end"
  onChange: (id: string) => void
}) {
  const [mounted, setMounted] = useState(false)
  const [open, setOpen] = useState(false)
  const [cursor, setCursor] = useState(0)
  const root = useRef<HTMLDivElement>(null)
  const list = useRef<HTMLUListElement>(null)
  const hideTimer = useRef(0)
  const openFrame = useRef(0)
  const listId = useId()
  const current = options.find((option) => option.id === value)
  const enabled = options.filter((option) => !option.disabled)
  const currentMark = unavailable ? undefined : current?.mark
  const spoken = [label, unavailable || current?.label || placeholder, currentMark?.label, currentMark?.note].filter(Boolean).join("，")

  useEffect(() => {
    if (!open) return
    list.current?.focus({ preventScroll: true })
    const menu = list.current
    const item = menu?.querySelector<HTMLElement>('[data-active="true"]')
    if (!menu || !item) return
    const top = item.offsetTop
    const bottom = top + item.offsetHeight
    if (top < menu.scrollTop) menu.scrollTop = top
    else if (bottom > menu.scrollTop + menu.clientHeight) menu.scrollTop = bottom - menu.clientHeight
  }, [open, cursor])

  useEffect(() => {
    return () => {
      window.clearTimeout(hideTimer.current)
      cancelAnimationFrame(openFrame.current)
    }
  }, [])

  useEffect(() => {
    if (!mounted) return
    function onPointer(event: MouseEvent) {
      if (!root.current?.contains(event.target as Node)) close()
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.preventDefault()
        close()
      }
    }
    document.addEventListener("mousedown", onPointer)
    document.addEventListener("keydown", onKey)
    return () => {
      document.removeEventListener("mousedown", onPointer)
      document.removeEventListener("keydown", onKey)
    }
  }, [mounted])

  function close() {
    cancelAnimationFrame(openFrame.current)
    setOpen(false)
    window.clearTimeout(hideTimer.current)
    hideTimer.current = window.setTimeout(() => setMounted(false), 220)
  }

  function openMenu() {
    if (disabled) return
    window.clearTimeout(hideTimer.current)
    const index = Math.max(0, enabled.findIndex((option) => option.id === value))
    setCursor(index)
    setMounted(true)
    cancelAnimationFrame(openFrame.current)
    openFrame.current = requestAnimationFrame(() => {
      openFrame.current = requestAnimationFrame(() => setOpen(true))
    })
  }

  function choose(id: string) {
    onChange(id)
    close()
  }

  function onTriggerKey(event: ReactKeyboardEvent) {
    if (disabled) return
    if (event.key === "ArrowDown" || event.key === "Enter" || event.key === " ") {
      event.preventDefault()
      if (!mounted) openMenu()
    }
  }

  function onListKey(event: ReactKeyboardEvent) {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault()
      setCursor((index) => {
        const next = event.key === "ArrowDown" ? index + 1 : index - 1
        return (next + enabled.length) % Math.max(enabled.length, 1)
      })
    }
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault()
      const option = enabled[cursor]
      if (option) choose(option.id)
    }
    if (event.key === "Escape") {
      event.preventDefault()
      close()
    }
  }

  return (
    <div className="choice" data-align={align} data-recommend={currentMark ? "true" : undefined} ref={root}>
      <button
        type="button"
        className="choice-trigger"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={listId}
        aria-label={spoken}
        disabled={disabled}
        onClick={() => (open ? close() : openMenu())}
        onKeyDown={onTriggerKey}
      >
        <ChoiceMark brand={current?.brand} name={current?.label} />
        <span className="choice-label">{unavailable || current?.label || placeholder}</span>
        {currentMark ? (
          <em className="choice-recommend">
            <span>{currentMark.label}</span>
          </em>
        ) : null}
        <span className="choice-caret" aria-hidden="true" />
      </button>
      {mounted ? (
        <ul
          className="choice-menu"
          id={listId}
          ref={list}
          role="listbox"
          aria-label={label}
          data-open={open ? "true" : "false"}
          tabIndex={-1}
          onKeyDown={onListKey}
        >
          {options.map((option) => {
            const index = enabled.findIndex((item) => item.id === option.id)
            const active = index === cursor
            return (
              <li key={option.id}>
                <button
                  type="button"
                  role="option"
                  aria-selected={option.id === value}
                  data-active={active ? "true" : "false"}
                  data-recommend={option.mark ? "true" : undefined}
                  disabled={option.disabled}
                  onMouseEnter={() => {
                    if (index >= 0) setCursor(index)
                  }}
                  onClick={() => choose(option.id)}
                >
                  <ChoiceMark brand={option.brand} name={option.label} />
                  <span>
                    <strong>
                      <span className="choice-name">{option.label}</span>
                      {option.mark ? (
                        <em className="choice-recommend">
                          <span>{option.mark.label}</span>
                        </em>
                      ) : null}
                    </strong>
                    {option.mark?.note || option.hint ? (
                      <small>
                        {option.mark?.note ? <span className="choice-recommend-note">{option.mark.note}</span> : null}
                        {option.hint ? <span>{option.hint}</span> : null}
                      </small>
                    ) : null}
                  </span>
                  {option.id === value ? <i aria-hidden="true" /> : null}
                </button>
              </li>
            )
          })}
        </ul>
      ) : null}
    </div>
  )
}
