/* 摘要生成的活动视图；本地 fixture 在动作开始/结束时发送事件。 */
(function () {
  const C = window.Core
  const { esc, ic, fmt } = C
  window.ProgressView = {
    render(v, logOpen) {
      const p = v.pipe
      if (!p) return ''
      const rows = Object.entries(v.figs || {}), failed = rows.filter(([,s]) => s === 'failed').length
      const selected = rows.filter(([,s]) => s === 'ok').length
      const doc = C.curDoc(v), current = rows.find(([,s]) => s === 'loading')?.[0]
      const fig = current && doc?.figures[current]
      const preview = fig ? `<button class="agent-preview" data-act="seek" data-t="${fig.t}" aria-label="回放候选画面 ${fmt(fig.t)}"><img src="${esc(fig.src)}" alt="${esc(fig.alt)}"><span>${fmt(fig.t)} · 候选画面</span></button>` : ''
      const acts = p.stage === 'failed' ? '<button class="btn btn-primary btn-sm" data-act="retry-summary">重试生成摘要</button>' : p.stage === 'idle' ? '<button class="btn btn-primary btn-sm" data-act="retry-summary">生成摘要</button>' : failed && p.stage === 'done' ? '<button class="link" data-act="goto-failed">定位未补充画面</button>' : ''
      const hint = p.stage === 'failed' ? '摘要未生成成功，视频与文字来源保留；可以重试。' : p.stage === 'idle' ? '已按你的选择仅导入，摘要尚未生成。' : doc ? p.stage === 'done' ? `摘要可阅读 · ${selected} 张画面 · ${v.tagLinks.length} 个标签${failed ? ' · ' + failed + ' 张待重试' : ''}` : '文字摘要已就绪，可以继续阅读；画面和标签按需补充。' : '可以离开页面；文字就绪后先开放阅读。'
      const legacy = !p.activity ? `<div class="agent-status"><strong>${p.stage === 'done' ? '摘要已完成' : '等待模拟执行事件'}</strong><span class="spacer"></span><button class="btn btn-ghost btn-xs" data-act="log-toggle">${logOpen ? '收起运行记录' : '展开运行记录'}</button></div>${logOpen ? '<ul class="progress-log">' + (p.log || []).map(l => `<li class="${esc(l.k)}">${ic('check','sm')}<span>${esc(l.t)}</span></li>`).join('') + '</ul>' : ''}` : ''
      return `<div class="summary-activity">${p.activity ? window.ActivityView.render(p.activity, { compact: !!doc }) : legacy}${preview}<div class="summary-activity-foot"><span>${esc(hint)}</span>${acts}</div></div>`
    }
  }
})()
