'use client'

// Three variants of long-term memory governance, switchable via ?variant=,
// on throwaway /prototype/memory.
// A = settings privacy list · B = scoped archive · C = conflict-first ledger

import { Suspense } from 'react'
import { useSearchParams } from 'next/navigation'
import { PrototypeSwitcher } from '@/components/prototype/PrototypeSwitcher'
import { MEMORY_VARIANTS, type MemoryVariantKey } from '@/components/prototype/memory/types'
import { useMemoryDemo } from '@/components/prototype/memory/useMemoryDemo'
import { VariantA } from '@/components/prototype/memory/VariantA'
import { VariantB } from '@/components/prototype/memory/VariantB'
import { VariantC } from '@/components/prototype/memory/VariantC'

export default function PrototypeMemoryPage() {
  return (
    <Suspense fallback={<div className="h-[100dvh] bg-paper-1" />}>
      <MemoryPrototypeView />
    </Suspense>
  )
}

function MemoryPrototypeView() {
  const searchParams = useSearchParams()
  const raw = searchParams.get('variant') ?? 'A'
  const variant: MemoryVariantKey = MEMORY_VARIANTS.some(v => v.key === raw)
    ? (raw as MemoryVariantKey)
    : 'A'
  const demo = useMemoryDemo()
  const caption = `mock · 生效 ${demo.counts.active} · 冲突 ${demo.counts.conflicted} · 已撤回 ${demo.counts.withdrawn}`

  return (
    <>
      {variant === 'A' && <VariantA demo={demo} />}
      {variant === 'B' && <VariantB demo={demo} />}
      {variant === 'C' && <VariantC demo={demo} />}
      <PrototypeSwitcher
        variants={MEMORY_VARIANTS}
        current={variant}
        caption={caption}
        onReset={demo.reset}
      />
    </>
  )
}
