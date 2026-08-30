'use client'

import Link from 'next/link'
import { PageHero, ProtoShell } from '@/components/prototype/c/Shell'
import {
  kindLabel,
  sourceLabel,
  type MemoryItem,
} from '@/components/prototype/memory/types'
import type { MemoryDemo } from '@/components/prototype/memory/useMemoryDemo'

export const VARIANT_A_NAME = '设置里的偏好清单'

function groupByKind(items: MemoryItem[]) {
  const map = new Map<string, MemoryItem[]>()
  for (const item of items) {
    const key = item.kind
    const group = map.get(key) ?? []
    group.push(item)
    map.set(key, group)
  }
  return [...map.entries()]
}

export function VariantA({ demo }: { demo: MemoryDemo }) {
  const mine = demo.items.filter(item => item.scopeType === 'user')
  const bound = demo.items.filter(item => item.scopeType !== 'user')
  const live = mine.filter(item => item.status !== 'withdrawn')
  const archived = mine.filter(item => item.status === 'withdrawn')

  return (
    <ProtoShell active="memory">
      <div className="h-full flex flex-col">
      <PageHero
        kicker="设置 · 长期记忆"
        title="Agent 记得的事"
        desc="只展示你明确说过或确认过的内容。撤回后不再进入下次问答；删除会从列表里拿掉。"
      />

      <div className="px-8 pb-3">
        <div className="flex items-center gap-1 border-b border-ink-0/8">
          <Link
            href="/prototype/settings"
            className="px-4 py-2.5 text-[12px] border-b-2 -mb-px border-transparent text-ink-4 hover:text-ink-2"
          >
            AI 配置
          </Link>
          <span className="px-4 py-2.5 text-[12px] border-b-2 -mb-px border-sienna-500 text-ink-0 font-medium">
            长期记忆
          </span>
        </div>
      </div>

      <main className="flex-1 overflow-y-auto px-8 pb-36">
        <p className="text-[12px] text-ink-4 mb-6">
          生效 {live.length} · 冲突 {mine.filter(i => i.status === 'conflicted').length} · 已撤回 {archived.length}
        </p>

        <section className="max-w-[640px]">
          {groupByKind(live).map(([kind, rows]) => (
            <div key={kind} className="mb-8">
              <h2 className="text-[12px] text-ink-4 mb-2">{kindLabel(kind)}</h2>
              {rows.some(r => r.status === 'conflicted') && rows.length > 1 ? (
                <ConflictPair rows={rows} onWithdraw={demo.withdraw} onRemove={demo.remove} />
              ) : (
                <ul className="divide-y divide-ink-0/8 border-y border-ink-0/8">
                  {rows.map(item => (
                    <PreferenceRow key={item.id} item={item} onWithdraw={demo.withdraw} onRemove={demo.remove} />
                  ))}
                </ul>
              )}
            </div>
          ))}

          {archived.length > 0 && (
            <div className="mb-10">
              <h2 className="text-[12px] text-ink-4 mb-2">已撤回，不再召回</h2>
              <ul className="space-y-1">
                {archived.map(item => (
                  <li key={item.id} className="flex items-center justify-between gap-4 py-2">
                    <span className="text-[13px] text-ink-4 line-through">{item.content}</span>
                    <button
                      type="button"
                      onClick={() => demo.remove(item.id)}
                      className="text-[11px] text-ink-5 hover:text-rust"
                    >
                      删除
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          )}

          {bound.length > 0 && (
            <div className="pt-2">
              <h2 className="text-[12px] text-ink-4 mb-1">绑在视频 / 知识库上的</h2>
              <p className="text-[12px] text-ink-5 mb-3">
                这个方案把它们收在设置页底部，不当成日常要管的东西。
              </p>
              <ul className="space-y-2">
                {bound.map(item => (
                  <li key={item.id} className="text-[13px] text-ink-3">
                    <span className="text-ink-5">{item.scopeLabel}</span>
                    <span className="mx-2 text-ink-5">·</span>
                    {item.content}
                    {item.status === 'conflicted' && (
                      <span className="ml-2 text-[10px] text-sienna-700">冲突</span>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </section>
      </main>
      </div>
    </ProtoShell>
  )
}

function ConflictPair({
  rows, onWithdraw, onRemove,
}: {
  rows: MemoryItem[]
  onWithdraw: (id: string) => void
  onRemove: (id: string) => void
}) {
  return (
    <div className="rounded-xl bg-sienna-500/6 px-4 py-3 mb-1">
      <p className="text-[11px] text-sienna-700 mb-3">同一件事记了两种说法，下次问答会同时带上，不会偷偷挑一个。</p>
      <ul className="space-y-3">
        {rows.map(item => (
          <li key={item.id}>
            <PreferenceRow item={item} onWithdraw={onWithdraw} onRemove={onRemove} bare />
          </li>
        ))}
      </ul>
    </div>
  )
}

function PreferenceRow({
  item, onWithdraw, onRemove,
}: {
  item: MemoryItem
  onWithdraw: (id: string) => void
  onRemove: (id: string) => void
  bare?: boolean
}) {
  return (
    <div className="flex items-start justify-between gap-6 py-3">
      <div className="min-w-0">
        <div className="text-[15px] text-ink-0 leading-snug">
          {item.content}
          {item.status === 'conflicted' && (
            <span className="ml-2 text-[10px] text-sienna-700 align-middle">冲突</span>
          )}
        </div>
        <div className="text-[11px] text-ink-5 mt-1">
          {sourceLabel(item.sourceType)} · {item.createdLabel}
          {item.lastUsedLabel ? ` · 上次用到 ${item.lastUsedLabel}` : ''}
        </div>
      </div>
      <div className="flex items-center gap-3 shrink-0 pt-0.5">
        <button type="button" onClick={() => onWithdraw(item.id)} className="text-[11px] text-ink-3 hover:text-ink-0">
          撤回
        </button>
        <button type="button" onClick={() => onRemove(item.id)} className="text-[11px] text-ink-5 hover:text-rust">
          删除
        </button>
      </div>
    </div>
  )
}
