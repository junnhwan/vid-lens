'use client'

import { useMemo, useState } from 'react'
import { ProtoShell } from '@/components/prototype/c/Shell'
import {
  kindLabel,
  scopeKey,
  sourceLabel,
  SCOPE_TYPE_LABEL,
  type MemoryItem,
  type MemoryScopeType,
} from '@/components/prototype/memory/types'
import type { MemoryDemo } from '@/components/prototype/memory/useMemoryDemo'

export const VARIANT_B_NAME = '按范围的档案柜'

interface ScopeBucket {
  key: string
  type: MemoryScopeType
  id: string
  label: string
  items: MemoryItem[]
}

function bucketsFrom(items: MemoryItem[]): ScopeBucket[] {
  const map = new Map<string, ScopeBucket>()
  for (const item of items) {
    const key = scopeKey(item)
    const existing = map.get(key)
    if (existing) {
      existing.items.push(item)
    } else {
      map.set(key, {
        key, type: item.scopeType, id: item.scopeId, label: item.scopeLabel, items: [item],
      })
    }
  }
  const order: MemoryScopeType[] = ['user', 'video', 'knowledge_base', 'run']
  return [...map.values()].sort((a, b) => order.indexOf(a.type) - order.indexOf(b.type))
}

export function VariantB({ demo }: { demo: MemoryDemo }) {
  const buckets = useMemo(() => bucketsFrom(demo.items), [demo.items])
  const [scope, setScope] = useState(buckets[0]?.key ?? '')
  const current = buckets.find(b => b.key === scope) ?? buckets[0]
  const [selectedId, setSelectedId] = useState<string | null>(current?.items[0]?.id ?? null)
  const selected = current?.items.find(i => i.id === selectedId) ?? current?.items[0] ?? null

  if (!current) {
    return (
      <ProtoShell active="memory">
        <div className="flex-1 flex items-center justify-center text-[13px] text-ink-4">没有记忆了。点底部「重置」恢复 mock。</div>
      </ProtoShell>
    )
  }

  return (
    <ProtoShell active="memory">
      <div className="h-full flex min-h-0">
        <aside className="w-[220px] shrink-0 border-r border-ink-0/8 overflow-y-auto py-5 px-3">
          <p className="px-2 text-[11px] text-ink-5 mb-3">按范围看，不按时间看。</p>
          {buckets.map(bucket => {
            const on = bucket.key === current.key
            const conflicts = bucket.items.filter(i => i.status === 'conflicted').length
            return (
              <button
                key={bucket.key}
                type="button"
                onClick={() => {
                  setScope(bucket.key)
                  setSelectedId(bucket.items[0]?.id ?? null)
                }}
                className={`w-full text-left px-3 py-2.5 rounded-lg mb-0.5 ${
                  on ? 'bg-sienna-500/8' : 'hover:bg-ink-0/4'
                }`}
              >
                <div className="flex items-baseline justify-between gap-2">
                  <span className={`text-[13px] truncate ${on ? 'font-medium text-ink-0' : 'text-ink-2'}`}>
                    {bucket.label}
                  </span>
                  <span className="text-[10px] tabular-nums text-ink-5">{bucket.items.length}</span>
                </div>
                <div className="text-[10px] text-ink-5 mt-0.5">
                  {SCOPE_TYPE_LABEL[bucket.type]}
                  {conflicts > 0 ? ` · ${conflicts} 条冲突` : ''}
                </div>
              </button>
            )
          })}
        </aside>

        <section className="flex-1 min-w-0 flex flex-col">
          <header className="px-6 pt-6 pb-4 border-b border-ink-0/8">
            <div className="text-[12px] text-ink-4">{SCOPE_TYPE_LABEL[current.type]}</div>
            <h1 className="text-[22px] font-semibold tracking-tight mt-1">{current.label}</h1>
            <p className="text-[13px] text-ink-3 mt-2 max-w-[46ch] leading-relaxed">
              {current.type === 'user' && '跟你这个人走，不绑某一支视频。'}
              {current.type === 'video' && '只在问这支视频时召回。视频删了，这些记忆也不会再进 Prompt。'}
              {current.type === 'knowledge_base' && '只在这个知识库的问答里可见，不会漏到别的库。'}
              {current.type === 'run' && '只活在这一轮。结束后不该升成长期记忆。'}
            </p>
          </header>

          <div className="flex-1 min-h-0 grid grid-cols-[minmax(0,1fr)_minmax(240px,0.85fr)]">
            <ul className="overflow-y-auto px-2 py-3 pb-32">
              {current.items.map(item => {
                const on = selected?.id === item.id
                return (
                  <li key={item.id}>
                    <button
                      type="button"
                      onClick={() => setSelectedId(item.id)}
                      className={`w-full text-left px-4 py-3 rounded-xl ${on ? 'bg-paper-0 ring-1 ring-ink-0/10' : 'hover:bg-ink-0/4'}`}
                    >
                      <div className="flex items-center gap-2">
                        <span className="text-[11px] text-ink-5">{kindLabel(item.kind)}</span>
                        <StatusMark status={item.status} />
                      </div>
                      <div className="text-[14px] text-ink-0 mt-1 leading-snug">{item.content}</div>
                    </button>
                  </li>
                )
              })}
            </ul>

            <aside className="border-l border-ink-0/8 overflow-y-auto px-5 py-5 pb-32 bg-paper-0/40">
              {selected ? (
                <Inspector item={selected} onWithdraw={demo.withdraw} onRemove={demo.remove} />
              ) : (
                <p className="text-[13px] text-ink-4">选一条记忆</p>
              )}
            </aside>
          </div>
        </section>
      </div>
    </ProtoShell>
  )
}

function StatusMark({ status }: { status: MemoryItem['status'] }) {
  if (status === 'conflicted') return <span className="text-[10px] text-sienna-700">冲突</span>
  if (status === 'withdrawn') return <span className="text-[10px] text-ink-5">已撤回</span>
  return <span className="text-[10px] text-moss">生效</span>
}

function Inspector({
  item, onWithdraw, onRemove,
}: {
  item: MemoryItem
  onWithdraw: (id: string) => void
  onRemove: (id: string) => void
}) {
  return (
    <div>
      <div className="text-[11px] text-ink-5 mb-2">v{item.version} · {item.id}</div>
      <p className="text-[16px] leading-relaxed text-ink-0">{item.content}</p>
      <dl className="mt-6 space-y-2.5 text-[12px]">
        <Row label="来源" value={`${sourceLabel(item.sourceType)} · ${item.sourceRef}`} />
        <Row label="写入" value={item.createdLabel} />
        <Row label="上次召回" value={item.lastUsedLabel ?? '还没用过'} />
        <Row label="过期" value={item.expiresLabel ?? '不过期'} />
        <Row label="向量投影" value={item.embeddingReady ? '可语义召回' : '仅按时间排序'} />
        <Row label="重要度" value={item.importance.toFixed(2)} />
      </dl>
      <p className="text-[11px] text-ink-5 mt-6 leading-relaxed">
        正式接口是撤回和删除。没有「改一改再存」——记忆不是笔记。
      </p>
      <div className="flex gap-2 mt-5">
        {item.status !== 'withdrawn' && (
          <button
            type="button"
            onClick={() => onWithdraw(item.id)}
            className="h-9 px-3 rounded-lg bg-ink-0 text-paper-0 text-[12px] proto-btn-lift"
          >
            撤回，不再召回
          </button>
        )}
        <button
          type="button"
          onClick={() => onRemove(item.id)}
          className="h-9 px-3 rounded-lg text-[12px] text-rust hover:bg-rust/8"
        >
          删除
        </button>
      </div>
    </div>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex gap-3">
      <dt className="w-16 shrink-0 text-ink-5">{label}</dt>
      <dd className="text-ink-2 break-all">{value}</dd>
    </div>
  )
}
