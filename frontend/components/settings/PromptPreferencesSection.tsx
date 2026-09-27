import { useEffect, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { PromptPreferenceView } from '@/lib/types'
import { useShell } from '@/components/shell/AppShell'
import { ErrorState } from '@/components/ui/AsyncState'
import './PromptPreferencesSection.css'

type SaveState = 'idle' | 'saving' | 'saved' | 'error'

export function PromptPreferencesSection({ readOnly }: { readOnly: boolean }) {
  const { registerLeaveGuard } = useShell()
  const [views, setViews] = useState<PromptPreferenceView[]>([])
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [selected, setSelected] = useState('')
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [saveStates, setSaveStates] = useState<Record<string, SaveState>>({})
  const [saveErrors, setSaveErrors] = useState<Record<string, string>>({})

  const load = async () => {
    setLoading(true)
    setLoadError('')
    try {
      const rows = await api.promptPreferences()
      setViews(rows)
      setDrafts(Object.fromEntries(rows.map(row => [row.function, row.user_instruction])))
      setSelected(current => rows.some(row => row.function === current) ? current : rows.find(row => row.editable)?.function || rows[0]?.function || '')
    } catch (error) {
      setLoadError(error instanceof ApiError ? error.message : '提示词配置加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { void load() }, [])
  const dirty = views.some(row => (drafts[row.function] ?? '') !== row.user_instruction)
  useEffect(() => {
    if (!dirty) { registerLeaveGuard(null); return }
    registerLeaveGuard(() => window.confirm('提示词偏好有未保存修改。放弃并离开吗？'))
    return () => registerLeaveGuard(null)
  }, [dirty, registerLeaveGuard])

  const save = async (row: PromptPreferenceView) => {
    const id = row.function
    if (readOnly || Object.values(saveStates).includes('saving')) return
    setSaveStates(previous => ({ ...previous, [id]: 'saving' }))
    setSaveErrors(previous => ({ ...previous, [id]: '' }))
    try {
      await api.setPromptPreference(id, drafts[id] ?? '')
      const refreshed = await api.promptPreferences()
      setViews(refreshed)
      setDrafts(previous => ({ ...previous, [id]: refreshed.find(item => item.function === id)?.user_instruction ?? '' }))
      setSaveStates(previous => ({ ...previous, [id]: 'saved' }))
    } catch (error) {
      setSaveStates(previous => ({ ...previous, [id]: 'error' }))
      setSaveErrors(previous => ({ ...previous, [id]: error instanceof ApiError ? error.message : '保存失败，请重试' }))
    }
  }

  if (loading) return <div className="prompt-load" role="status"><span className="skel" /><span className="skel" /><span className="skel" /><span>正在读取提示词偏好…</span></div>
  if (loadError) return <ErrorState message={loadError} onRetry={() => void load()} />
  if (views.length === 0) return <p className="prompt-empty">当前没有可展示的提示词规则。</p>

  const row = views.find(item => item.function === selected) || views[0]
  const draft = drafts[row.function] ?? ''
  const changed = draft !== row.user_instruction
  const state = saveStates[row.function] || 'idle'
  const savingAny = Object.values(saveStates).includes('saving')
  const limit = row.function === 'summary' ? 500 : 2000
  const editable = views.filter(item => item.editable)
  const fixed = views.filter(item => !item.editable)
  const status = state === 'saving' ? '正在保存偏好…'
    : state === 'error' ? saveErrors[row.function]
    : changed ? draft ? '有未保存的修改' : '恢复默认，等待保存'
    : state === 'saved' ? draft ? '已保存，对之后的新请求生效' : '已恢复默认，对之后的新请求生效'
    : draft ? '已保存个人偏好' : '使用产品默认规则'

  const updateDraft = (value: string) => {
    setDrafts(previous => ({ ...previous, [row.function]: value }))
    setSaveStates(previous => ({ ...previous, [row.function]: 'idle' }))
  }
  const statusTone = state === 'error' ? 'error' : state === 'saving' ? 'saving' : changed ? 'dirty' : 'saved'

  return (
    <section className="prompt-section">
      <header className="prompt-heading">
        <div>
          <h2>AI 提示词与偏好</h2>
          <p>只调整模型的表达方式。证据、事实、引用和输出结构始终由产品规则约束；每项偏好独立保存，仅影响之后的新请求。</p>
        </div>
        <span>{String(editable.length).padStart(2, '0')} EDITABLE / {String(fixed.length).padStart(2, '0')} FIXED</span>
      </header>
      <div className="prompt-principles">
        <span>✓ 按当前用户保存</span>
        <span>✓ 跨配置档与视频生效</span>
        <span>✓ 已完成内容不会重算</span>
      </div>
      <div className="prompt-layout">
        <nav className="prompt-rail" aria-label="提示词功能">
          <small>我的偏好</small>
          {editable.map(item => (
            <button key={item.function} type="button"
              className={item.function === row.function ? 'active' : ''}
              aria-current={item.function === row.function ? 'true' : undefined}
              onClick={() => setSelected(item.function)}>
              {item.label}
              {(drafts[item.function] ?? '') !== item.user_instruction && <i aria-label="未保存">●</i>}
            </button>
          ))}
          <small className="prompt-rail-fixed">固定规则 · 只读</small>
          {fixed.map(item => (
            <button key={item.function} type="button"
              className={item.function === row.function ? 'active' : ''}
              aria-current={item.function === row.function ? 'true' : undefined}
              onClick={() => setSelected(item.function)}>
              {item.label}
            </button>
          ))}
        </nav>
        <div className="prompt-work">
          <div className="prompt-work-head">
            <div><h3>{row.label}</h3><p>{row.scope}</p></div>
            <span className="prompt-kind">{row.editable ? '可编辑' : '固定'}</span>
          </div>
          {row.editable ? (
            <div className="prompt-columns">
              <div className="prompt-editor">
                <label htmlFor={'prompt-' + row.function}>我的表达偏好 <small>可选 · 不覆盖产品规则</small></label>
                <textarea id={'prompt-' + row.function} value={draft} maxLength={limit}
                  disabled={readOnly || savingAny}
                  placeholder="例如：用简洁中文回答，并优先标出关键时间点"
                  onChange={event => updateDraft(event.target.value)} />
                <div className="prompt-editor-meta">
                  <span>留空并保存即可恢复默认。</span><span className="mono">{draft.length} / {limit}</span>
                </div>
                <div className="prompt-actions">
                  <button className="btn btn-sm btn-primary" disabled={readOnly || savingAny || !changed}
                    onClick={() => void save(row)}>
                    {state === 'saving' ? '保存中…' : state === 'error' ? '重试保存' : '保存此偏好'}
                  </button>
                  <button className="btn btn-sm btn-ghost" disabled={readOnly || savingAny || !draft}
                    onClick={() => updateDraft('')}>恢复默认</button>
                  <button className="btn btn-sm btn-ghost" disabled={readOnly || savingAny || !changed}
                    onClick={() => updateDraft(row.user_instruction)}>撤销修改</button>
                </div>
                <p className={'prompt-status ' + statusTone} role="status">{status}</p>
              </div>
              <aside className="prompt-preview">
                <h4>生效配置预览</h4>
                <p>帮助理解指令的组成；实际请求由服务端构造。</p>
                <div className="prompt-layer"><b>01 · 产品规则（固定）</b><p>{row.product_instruction}</p></div>
                <div className="prompt-layer user"><b>02 · 我的表达偏好</b><p>{draft.trim() || '未添加个人偏好，使用产品默认规则。'}</p></div>
                <div className="prompt-layer runtime"><b>03 · 请求时动态加入</b><p>当前问题、可用证据与授权范围会随每次请求加入。</p></div>
              </aside>
            </div>
          ) : (
            <div className="prompt-fixed-detail">
              <p>这项由产品维护，仅供查看。{row.scope}</p>
              <div className="prompt-layer"><b>产品规则</b><p>{row.product_instruction}</p></div>
            </div>
          )}
          <p className="prompt-footnote">预览展示产品规则与当前草稿的组成方式，不会发起模型请求；请保存后再用于新请求。</p>
        </div>
      </div>
    </section>
  )
}
