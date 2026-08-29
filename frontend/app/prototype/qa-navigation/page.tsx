'use client'

import { Suspense } from 'react'
import { useSearchParams } from 'next/navigation'
import PrototypeSwitcher from '@/components/prototype/PrototypeSwitcher'
import { NavVariantAB, NavVariantA, NavVariantB } from '@/components/prototype/qa-nav/variants'

const VARIANTS = [
  { key: 'AB', name: 'A+B 融合（推荐）' },
  { key: 'A', name: '仅侧栏分组' },
  { key: 'B', name: '仅问答案例台' },
] as const

export default function QaNavigationPrototypePage() {
  return (
    <Suspense fallback={<div className="h-screen bg-[#f7f4ef]" />}>
      <QaNavView />
    </Suspense>
  )
}

function QaNavView() {
  const sp = useSearchParams()
  const variant = sp.get('variant') ?? 'AB'

  return (
    <>
      {variant === 'AB' && <NavVariantAB />}
      {variant === 'A' && <NavVariantA />}
      {variant === 'B' && <NavVariantB />}
      <div className="pb-20">
        <PrototypeSwitcher variants={[...VARIANTS]} current={variant} />
      </div>
    </>
  )
}
