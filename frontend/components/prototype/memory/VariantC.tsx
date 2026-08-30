'use client'

import Link from 'next/link'
import { groupConflicts, kindLabel, sourceLabel, type MemoryItem } from '@/components/prototype/memory/types'
import type { MemoryDemo } from '@/components/prototype/memory/useMemoryDemo'

export const VARIANT_C_NAME = '冲突优先的治理台'

export function VariantC({ demo }: { demo: MemoryDemo }) {
  const conflicts = groupConflicts(demo.items)
  const recalled = demo.items
    .filter(item => item.status === 'active' || item.status === 'conflicted')
    .filter(item => item.scopeType !== 'run')
    .slice(0, 6)
  const ledger = demo.items

  return (
    <div className="h-[100dvh] flex flex-col bg-paper-1 text-ink-0 overflow-hidden proto-root">
      <header className="shrink-0 px-8 pt-6 pb-4 border-b border-ink-0/8 bg-paper-0/70">
        <div className="flex items-baseline justify-between gap-6">
          <div>
            <Link href="/prototype" className="text-[12px] text-ink-4 hover:text-ink-1">原型入口</Link>
            <h1 className="text-[26px] font-semibold tracking-tight mt-2">先处理说不清的记忆</h1>
            <p className="text-[13px] text-ink-3 mt-1.5 max-w-[52ch] leading-relaxed">
              后端会把冲突成组保留，不会静默挑一个。这个方案把「你来拍板」放在第一屏，清单变成审计日志。
            </p>
          </div>
          <div className="text-right text-[12px] text-ink-4 tabular-nums">
            <div>待决冲突 {conflicts.length} 组</div>
            <div className="mt-1">下次大约带上 {recalled.length} 条</div>
          </div>
        </div>
      </header>

      <main className="flex-1 min-h-0 overflow-y-auto pb-36">
        <section className="px-8 pt-6">
          {conflicts.length === 0 ? (
            <div className="py-8 text-[14px] text-ink-4">没有冲突。记忆会按原样进下次问答。</div>
          ) : (
            <div className="space-y-4">
              {conflicts.map(group => (
                <ConflictBoard key={`${group[0].scopeType}:${group[0].scopeId}:${group[0].kind}`} group={group} onKeep={demo.keep} />
              ))}
            </div>
          )}
        </section>

        <section className="px-8 mt-10">
          <h2 className="text-[12px] text-ink-4 mb-3">下次问答大概会带上这些</h2>
          <div className="bg-paper-0 rounded-xl ring-1 ring-ink-0/8 px-5 py-4">
            <p className="text-[11px] text-ink-5 mb-3">
              受 top-k / 字数上限裁剪。冲突项必须整组进入，所以「详细」和「简洁」会一起出现。
            </p>
            <ol className="space-y-2">
              {recalled.map((item, index) => (
                <li key={item.id} className="flex items-baseline gap-3 text-[13px]">
                  <span className="w-4 text-[11px] tabular-nums text-ink-5">{index + 1}</span>
                  <span className="text-ink-5 shrink-0 w-[88px] truncate">{item.scopeLabel}</span>
                  <span className={item.status === 'conflicted' ? 'text-sienna-700' : 'text-ink-1'}>
                    {item.content}
                  </span>
                </li>
              ))}
            </ol>
          </div>
        </section>

        <section className="px-8 mt-10 mb-8">
          <h2 className="text-[12px] text-ink-4 mb-1">全部记录</h2>
          <p className="text-[12px] text-ink-5 mb-4">像流水账，不像设置页。删除从这本账里抹掉。</p>
          <table className="w-full text-left">
            <thead>
              <tr className="text-[11px] text-ink-5 border-b border-ink-0/8">
                <th className="font-normal py-2 pr-4">写入</th>
                <th className="font-normal py-2 pr-4">范围</th>
                <th className="font-normal py-2 pr-4">内容</th>
                <th className="font-normal py-2 pr-4">状态</th>
                <th className="font-normal py-2" />
              </tr>
            </thead>
            <tbody>
              {ledger.map(item => (
                <tr key={item.id} className="border-b border-ink-0/6 align-top">
                  <td className="py-3 pr-4 text-[12px] text-ink-5 whitespace-nowrap">{item.createdLabel}</td>
                  <td className="py-3 pr-4 text-[12px] text-ink-4 whitespace-nowrap">{item.scopeLabel}</td>
                  <td className="py-3 pr-4">
                    <div className="text-[13px] text-ink-1">{item.content}</div>
                    <div className="text-[11px] text-ink-5 mt-0.5">
                      {kindLabel(item.kind)} · {sourceLabel(item.sourceType)}
                    </div>
                  </td>
                  <td className="py-3 pr-4 text-[12px] whitespace-nowrap">
                    {item.status === 'conflicted' && <span className="text-sienna-700">冲突</span>}
                    {item.status === 'active' && <span className="text-moss">生效</span>}
                    {item.status === 'withdrawn' && <span className="text-ink-5">已撤回</span>}
                  </td>
                  <td className="py-3 text-right whitespace-nowrap">
                    {item.status !== 'withdrawn' && (
                      <button type="button" onClick={() => demo.withdraw(item.id)} className="text-[11px] text-ink-3 hover:text-ink-0 mr-3">
                        撤回
                      </button>
                    )}
                    <button type="button" onClick={() => demo.remove(item.id)} className="text-[11px] text-ink-5 hover:text-rust">
                      删除
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      </main>
    </div>
  )
}

function ConflictBoard({ group, onKeep }: { group: MemoryItem[]; onKeep: (id: string) => void }) {
  const head = group[0]
  return (
    <div className="rounded-xl bg-paper-0 ring-1 ring-ink-0/8 overflow-hidden">
      <div className="px-5 py-3 border-b border-ink-0/8 text-[12px] text-ink-4">
        {head.scopeLabel} · {kindLabel(head.kind)}
      </div>
      <div className="grid sm:grid-cols-2 divide-y sm:divide-y-0 sm:divide-x divide-ink-0/8">
        {group.map(item => (
          <div key={item.id} className="p-5 flex flex-col">
            <p className="text-[16px] leading-snug text-ink-0 flex-1">{item.content}</p>
            <p className="text-[11px] text-ink-5 mt-3">
              {sourceLabel(item.sourceType)} · {item.createdLabel}
              {item.lastUsedLabel ? ` · 用过 ${item.lastUsedLabel}` : ''}
            </p>
            <button
              type="button"
              onClick={() => onKeep(item.id)}
              className="mt-4 h-9 px-3 self-start rounded-lg bg-ink-0 text-paper-0 text-[12px] proto-btn-lift"
            >
              留下这条，撤回另一条
            </button>
          </div>
        ))}
      </div>
    </div>
  )
}
