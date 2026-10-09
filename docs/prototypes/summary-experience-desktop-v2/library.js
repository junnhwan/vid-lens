/* 桌面原型副本：紧凑标签筛选。沿用原视频库的标签与匹配规则。 */
(function () {
  const C = window.Core
  const { esc, ic } = C
  const limit = 6

  function tagBarHTML(tags, selected) {
    const active = new Set(selected)
    const visible = tags.filter((item, index) => index < limit || active.has(item.t.id))
    return visible.map(({ t, n }) => `<button type="button" class="tag${active.has(t.id) ? ' on' : ''}" data-act="lib-tag" data-id="${esc(t.id)}" aria-pressed="${active.has(t.id)}">${esc(t.name)}<span class="n">${n}</span></button>`).join('') +
      `<button type="button" class="tag lib-all-tags" data-act="lib-tags-more" aria-haspopup="dialog">${ic('filter', 'sm')}全部标签<span class="n">${tags.length}</span>${ic('chev-d', 'sm')}</button>`
  }

  function openTags(tags, selected, onApply) {
    const available = new Set(tags.map(({ t }) => t.id))
    const pending = new Set(selected.filter(id => available.has(id)))
    const el = C.openDialog(`<div class="dlg-head"><span class="dlg-icon">${ic('tag')}</span><div class="grow"><h3 id="lib-picker-title">选择筛选标签</h3><p>常用标签显示在视频库，其他标签可以在这里搜索</p></div><button type="button" class="btn btn-ghost btn-sm btn-ic" data-picker-close aria-label="关闭标签筛选">${ic('x')}</button></div><div class="dlg-body lib-picker-body"><div class="search lib-picker-search">${ic('search')}<input autofocus class="input" data-picker-search placeholder="搜索标签或别名…" aria-label="搜索标签或别名"></div><div class="lib-picker-status"><span data-picker-count></span><button type="button" class="btn btn-ghost btn-xs" data-picker-clear>清空选择</button></div><div class="lib-picker-list" role="group" aria-label="可选择的标签"></div></div><div class="dlg-foot"><span class="grow">应用后更新视频库；多标签匹配方式沿用当前选择</span><button type="button" class="btn btn-quiet" data-picker-close>取消</button><button type="button" class="btn btn-primary" data-picker-apply>${ic('check', 'sm')}应用筛选</button></div>`, { width: 620, cls: 'lib-tag-picker' })
    el.setAttribute('aria-labelledby', 'lib-picker-title')
    const search = el.querySelector('[data-picker-search]')
    const list = el.querySelector('.lib-picker-list')
    const count = el.querySelector('[data-picker-count]')

    function render() {
      const q = C.norm(search.value)
      const rows = tags.filter(({ t }) => !q || [t.name, ...(t.aliases || [])].some(name => C.norm(name).includes(q)))
      count.textContent = pending.size ? `已选择 ${pending.size} 个标签 · ${rows.length} 个匹配` : `共 ${rows.length} 个标签`
      el.querySelector('[data-picker-clear]').disabled = pending.size === 0
      list.innerHTML = rows.length ? rows.map(({ t, n }) => `<label class="lib-picker-row${pending.has(t.id) ? ' selected' : ''}"><input type="checkbox" value="${esc(t.id)}" ${pending.has(t.id) ? 'checked' : ''}><span class="lib-picker-name">${esc(t.name)}${t.aliases?.length ? `<small>${esc(t.aliases.join(' · '))}</small>` : ''}</span><span class="lib-picker-total">${n} 个视频</span></label>`).join('') : `<div class="lib-picker-empty">${ic('search')}<b>没有匹配的标签</b><span>试试标签名称或别名，已选标签会保留</span></div>`
    }

    search.addEventListener('input', render)
    list.addEventListener('change', e => {
      const checkbox = e.target.closest('input[type="checkbox"]')
      if (!checkbox) return
      checkbox.checked ? pending.add(checkbox.value) : pending.delete(checkbox.value)
      checkbox.closest('.lib-picker-row').classList.toggle('selected', checkbox.checked)
      const q = C.norm(search.value)
      const matching = tags.filter(({ t }) => !q || [t.name, ...(t.aliases || [])].some(name => C.norm(name).includes(q))).length
      count.textContent = pending.size ? `已选择 ${pending.size} 个标签 · ${matching} 个匹配` : `共 ${matching} 个标签`
      el.querySelector('[data-picker-clear]').disabled = pending.size === 0
    })
    el.querySelectorAll('[data-picker-close]').forEach(button => button.addEventListener('click', () => C.closeDialog()))
    el.querySelector('[data-picker-clear]').addEventListener('click', () => { pending.clear(); render(); search.focus() })
    el.querySelector('[data-picker-apply]').addEventListener('click', () => {
      const next = tags.filter(({ t }) => pending.has(t.id)).map(({ t }) => t.id)
      C.closeDialog()
      onApply(next)
    })
    // Keep keyboard navigation inside the open picker. Escape is handled by Core/UI.
    el.addEventListener('keydown', e => {
      if (e.key !== 'Tab') return
      const controls = Array.from(el.querySelectorAll('button:not(:disabled), input:not(:disabled), [tabindex="0"]')).filter(node => node.offsetParent)
      const first = controls[0], last = controls[controls.length - 1]
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus() }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus() }
    })
    render()
    return el
  }

  window.DesktopLibrary = { tagBarHTML, openTags }
})()
