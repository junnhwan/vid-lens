/* 共享 Agent 活动视图：只展示已经发生的模拟事件，不预设后续步骤。 */
(function () {
  const { esc, ic, fmt } = window.Core
  const registry = new Map()
  const states = ['running', 'done', 'error', 'cancelled']
  const stateLabels = { running: '执行中', done: '已完成', error: '未完成', cancelled: '已取消' }
  let nextRun = 0

  function short(value, limit) {
    const chars = Array.from(String(value ?? '').trim())
    return chars.length > limit ? chars.slice(0, limit - 1).join('') + '…' : chars.join('')
  }

  function create(kind) {
    const run = {
      id: 'activity-' + Date.now().toString(36) + '-' + (++nextRun),
      kind: String(kind || 'summary'), status: 'running', steps: [], expanded: false,
      seq: 0, startedAt: Date.now()
    }
    registry.set(run.id, run)
    return run
  }

  function start(run, title, opts = {}) {
    if (!run || run.status !== 'running') return null
    const step = {
      id: run.id + '-step-' + (++run.seq), title: short(title || '执行任务', 40),
      status: 'running', detail: short(opts.detail || '', 160), tool: short(opts.tool || '', 40),
      startedAt: Date.now()
    }
    if (typeof opts.time === 'number' && Number.isFinite(opts.time) && opts.time >= 0) step.time = opts.time
    run.steps.push(step)
    registry.set(run.id, run)
    return step.id
  }

  function update(run, id, status, detail) {
    if (!run || !states.includes(status)) return null
    const step = run.steps.find(s => s.id === id)
    if (!step) return null
    step.status = status
    if (detail !== undefined) step.detail = short(detail, 160)
    if (status === 'running') delete step.finishedAt
    else step.finishedAt = Date.now()
    registry.set(run.id, run)
    return step
  }

  function finish(run, status = 'done') {
    if (!run) return null
    const finalStatus = ['done', 'error', 'cancelled'].includes(status) ? status : 'done'
    run.steps.filter(s => s.status === 'running').forEach(s => update(run, s.id, finalStatus))
    run.status = finalStatus
    run.finishedAt = Date.now()
    registry.set(run.id, run)
    return run
  }

  function duration(start, end) {
    if (!Number.isFinite(start) || !Number.isFinite(end)) return ''
    const seconds = Math.max(0, Math.round((end - start) / 1000))
    return seconds < 60 ? seconds + ' 秒' : Math.floor(seconds / 60) + ' 分 ' + (seconds % 60) + ' 秒'
  }

  function row(step, current) {
    const status = states.includes(step.status) ? step.status : 'cancelled'
    const mark = status === 'done' ? ic('check', 'sm') : status === 'error' ? ic('alert', 'sm') : status === 'cancelled' ? ic('x', 'sm') : '<i></i>'
    const meta = [step.tool, Number.isFinite(step.time) ? '视频 ' + fmt(step.time) : '', duration(step.startedAt, step.finishedAt)].filter(Boolean)
    return `<li class="activity-step activity-step-${status}${current ? ' activity-step-current' : ''}" data-step="${esc(step.id)}">
      <span class="activity-mark" aria-hidden="true">${mark}</span>
      <div class="activity-step-copy"><div class="activity-step-title"><span>${esc(step.title)}</span><span class="sr-only">，${stateLabels[status]}</span></div>
        ${step.detail ? `<p class="activity-step-detail">${esc(step.detail)}</p>` : ''}
        ${meta.length ? `<div class="activity-step-meta">${meta.map(esc).join('<span aria-hidden="true">·</span>')}</div>` : ''}
      </div>
    </li>`
  }

  function render(run, opts = {}) {
    if (!run) return ''
    registry.set(run.id, run)
    const title = opts.title === undefined ? 'Agent 执行过程' : String(opts.title)
    const limit = Math.max(1, Math.floor(Number(opts.limit) || 3))
    const active = run.status === 'running'
    const current = active ? [...run.steps].reverse().find(s => s.status === 'running') : null
    const ordered = current ? run.steps.filter(s => s.id !== current.id).concat(current) : run.steps
    const visible = run.expanded ? ordered : active ? ordered.slice(-limit) : []
    const count = run.steps.length
    const hidden = count - visible.length
    const unfinished = run.steps.filter(s => s.status === 'error' || s.status === 'cancelled').length
    const finalLabel = run.status === 'cancelled' ? '已取消' : run.status === 'error' || unfinished ? '已结束' : '已完成'
    const summary = active ? count ? `${count} 条活动` : '等待执行事件' : `${finalLabel}${unfinished ? ` · ${unfinished} 条异常记录` : ''} · ${count} 条活动`
    const elapsed = active ? '' : duration(run.startedAt, run.finishedAt)
    const toggleLabel = run.expanded ? '收起执行记录' : active && hidden > 0 ? `展开全部 ${count} 条` : active ? '展开执行记录' : '查看执行记录'
    return `<section class="activity-view${opts.compact ? ' activity-view-compact' : ''} activity-run-${esc(run.status)}" data-activity="${esc(run.id)}" aria-label="${esc(title)}">
      <div class="activity-head">
        <div class="activity-heading"><strong>${esc(title)}</strong><span class="activity-demo">模拟事件</span></div>
        <button class="activity-toggle" type="button" data-act="activity-toggle" data-run="${esc(run.id)}" aria-expanded="${!!run.expanded}" aria-label="${esc(toggleLabel)}">${ic('chev-d', 'sm')}</button>
      </div>
      <div class="activity-summary"><span>${esc(summary)}</span>${elapsed ? `<span>${esc(elapsed)}</span>` : ''}</div>
      ${visible.length ? `<ol class="activity-list">${visible.map(s => row(s, !!current && current.id === s.id)).join('')}</ol>` : ''}
      ${active && hidden > 0 && !run.expanded ? `<button class="activity-older" type="button" data-act="activity-toggle" data-run="${esc(run.id)}">${hidden} 条较早活动${ic('chev-d', 'sm')}</button>` : ''}
      ${current ? `<span class="sr-only" role="status" aria-live="polite">${esc(current.title)}</span>` : ''}
    </section>`
  }

  window.ActivityView = { create, start, update, finish, render, get: id => registry.get(id) }
})()
