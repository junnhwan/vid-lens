'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import type { AgentDemoScenario, AgentStep, EvidenceChunk, StepStatus } from '@/components/prototype/agent/types'
import { DEMO_RESEARCH_STEPS, DEMO_STEPS_TEMPLATE } from '@/components/prototype/agent/types'

const BASIC_DELAYS = [600, 900, 1100, 1400]
const RESEARCH_DELAYS = [500, 700, 850, 600, 750, 900, 700, 1200]

function templateFor(scenario: AgentDemoScenario): AgentStep[] {
  return scenario === 'research' ? DEMO_RESEARCH_STEPS : DEMO_STEPS_TEMPLATE
}

function delaysFor(scenario: AgentDemoScenario): number[] {
  return scenario === 'research' ? RESEARCH_DELAYS : BASIC_DELAYS
}

export function useAgentDemo(autoStart = true, scenario: AgentDemoScenario = 'research') {
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
    const template = templateFor(scenario)
    const delays = delaysFor(scenario)
    const pending = template.map(s => ({ ...s, status: 'pending' as StepStatus }))
    setSteps(pending)

    let delay = 0
    template.forEach((step, i) => {
      delay += delays[i] ?? 700
      timers.current.push(setTimeout(() => {
        setSteps(prev => prev.map((s, j) => {
          if (j < i) return { ...s, status: 'done' as StepStatus }
          if (j === i) return { ...s, status: 'running' as StepStatus }
          return s
        }))
      }, delay))

      timers.current.push(setTimeout(() => {
        setSteps(prev => prev.map((s, j) => (j <= i ? { ...s, status: 'done' as StepStatus } : s)))
        if (i === template.length - 1) {
          setRunning(false)
          setDone(true)
        }
      }, delay + (delays[i] ?? 700) * 0.85))
    })
  }, [reset, scenario])

  useEffect(() => {
    if (autoStart) run()
    return clearTimers
  }, [autoStart, run])

  return { steps, running, done, run, reset, scenario }
}

/** 从步骤流中收集已观察到的证据片段（observe 步骤产出） */
export function collectEvidence(steps: AgentStep[]): EvidenceChunk[] {
  const seen = new Set<string>()
  const out: EvidenceChunk[] = []
  for (const step of steps) {
    if (step.kind !== 'observe' || step.status === 'pending') continue
    for (const ev of step.newEvidence ?? []) {
      if (seen.has(ev.id)) continue
      seen.add(ev.id)
      out.push(ev)
    }
  }
  return out
}
