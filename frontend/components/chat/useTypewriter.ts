'use client'

import { useEffect, useState } from 'react'

const CHAR_MS = 38
const PUNCT_PAUSE: Record<string, number> = {
  '。': 200, '？': 200, '！': 200, '，': 100, '；': 100, '：': 80, '\n': 120,
}

function prefersReducedMotion() {
  if (typeof window === 'undefined') return false
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/** 柔和打字机：用于非流式一次性返回的回答（如 Agent REST）；RAG SSE 仍走增量 append */
export function useTypewriter(text: string, active: boolean) {
  const [length, setLength] = useState(0)
  const [finished, setFinished] = useState(false)

  useEffect(() => {
    if (!active || !text) {
      setLength(0)
      setFinished(false)
      return
    }

    if (prefersReducedMotion()) {
      setLength(text.length)
      setFinished(true)
      return
    }

    setLength(0)
    setFinished(false)

    let index = 0
    const timers: ReturnType<typeof setTimeout>[] = []

    const revealNext = () => {
      if (index >= text.length) {
        setFinished(true)
        return
      }
      index += 1
      setLength(index)
      const ch = text[index - 1]
      const delay = CHAR_MS + (PUNCT_PAUSE[ch] ?? 0)
      timers.push(setTimeout(revealNext, delay))
    }

    timers.push(setTimeout(revealNext, 320))

    return () => timers.forEach(clearTimeout)
  }, [text, active])

  return {
    displayed: text.slice(0, length),
    finished,
    typing: active && !finished && length > 0,
  }
}
