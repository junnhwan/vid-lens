'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import type { AgentStep, StepStatus } from '@/components/prototype/agent/types'
import { DEMO_STEPS_TEMPLATE } from '@/components/prototype/agent/types'

const DELAYS = [600, 900, 1100, 1400]

export function useAgentDemo(autoStart = true) {
  const [steps, setSteps] = useState<AgentStep[]>([])
  const [running, setRunning] = useState(false)
  const [done, setDone] = useState(false)
  const timers = useRef<ReturnType<typeof setTimeout>[]>([])

  const clearTimers = () => {
    timers.current.forEach(clearTimeout)
    timers.current = []
  }

  const reset = useCallback(() => {
    clearTimers()
    setSteps([])
    setRunning(false)
    setDone(false)
  }, [])

  const run = useCallback(() => {
    reset()
    setRunning(true)
    const pending = DEMO_STEPS_TEMPLATE.map(s => ({ ...s, status: 'pending' as StepStatus }))
    setSteps(pending)

    let delay = 0
    DEMO_STEPS_TEMPLATE.forEach((step, i) => {
      delay += DELAYS[i] ?? 700
      timers.current.push(setTimeout(() => {
        setSteps(prev => prev.map((s, j) => {
          if (j < i) return { ...s, status: 'done' as StepStatus }
          if (j === i) return { ...s, status: 'running' as StepStatus }
          return s
        }))
      }, delay))

      timers.current.push(setTimeout(() => {
        setSteps(prev => prev.map((s, j) => (j <= i ? { ...s, status: 'done' as StepStatus } : s)))
        if (i === DEMO_STEPS_TEMPLATE.length - 1) {
          setRunning(false)
          setDone(true)
        }
      }, delay + (DELAYS[i] ?? 700) * 0.85))
    })
  }, [reset])

  useEffect(() => {
    if (autoStart) run()
    return clearTimers
  }, [autoStart, run])

  return { steps, running, done, run, reset }
}
