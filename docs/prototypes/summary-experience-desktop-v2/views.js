/* 映知 VidLens · 摘要体验原型 · 页面与交互 */
(function () {
  const C = window.Core
  const AV = window.ActivityView
  const { $, $$, esc, ic, fmt, md, inline, plain, ago, clock, timeBtn, toast, openPop, closePop, openDialog, closeDialog, dialogEl, P, D } = C
  const S = () => C.S()
  let R = { name: 'home' }
  const IMP = freshImport()
  const UIS = { mapFold: {}, mapChanged: {}, mPlayer: false, revealed: new Set(), annoUI: {}, floatOpen: false, lastFollow: null, streaming: {}, workspaces: {} }
  let SEL = null, REV = null, LB = null

  function freshImport() { return { tab: 'url', url: '', parsed: null, err: '', file: null, focus: '', more: false, mode: 'auto', map: 'auto', visual: true, tags: true, importOnly: false } }
  const content = () => $('#content')
  const go = h => { if (location.hash === h) render(); else location.hash = h }
  const short = (s, n = 18) => s.length > n ? s.slice(0, n) + '…' : s
  const readable = v => v.versions.length > 0

  /* Desktop workspace keeps reading and conversation in place. */
  function workspace(v) {
    return UIS.workspaces[v.id] ||= { dock: innerWidth < 1240 ? 'closed' : 'player', focus: '', top: 0, anchor: null, threadTop: 0, threadBottom: true, restore: null }
  }
  function captureReading(v) {
    const w = workspace(v), root = $('#reading')
    if (!root || w.focus === 'ask') return
    if (root.dataset.kind === 'transcript') { w.transcriptTop = content().scrollTop; return }
    w.top = content().scrollTop
    if (w.top < 4) { w.anchor = null; return }
    const top = content().getBoundingClientRect().top + 58
    const el = [...root.querySelectorAll('.para[data-para], figure[data-fig-slot]')].find(x => x.getBoundingClientRect().bottom > top)
    w.anchor = el ? { selector: el.dataset.para ? `.para[data-para="${el.dataset.para}"]` : `figure[data-fig-slot="${el.dataset.figSlot}"]`, offset: el.getBoundingClientRect().top - top } : null
  }
  function restoreReading(v) {
    const w = workspace(v)
    if (w.focus === 'ask') return
    const root = $('#reading')
    if (root?.dataset.kind === 'transcript') { content().scrollTo({ top: w.transcriptTop || 0, behavior: 'instant' }); return }
    content().scrollTo({ top: w.top, behavior: 'instant' })
    const el = w.anchor && root?.querySelector(w.anchor.selector)
    if (el) content().scrollTo({ top: content().scrollTop + el.getBoundingClientRect().top - content().getBoundingClientRect().top - 58 - w.anchor.offset, behavior: 'instant' })
    spy()
  }
  function captureThread(v) {
    const t = $('#thread'); if (!t) return
    const w = workspace(v)
    w.threadTop = t.scrollTop
    w.threadBottom = t.scrollHeight - t.scrollTop - t.clientHeight < 100
  }
  function restoreThread(v) {
    const t = $('#thread'); if (!t) return
    const w = workspace(v)
    t.scrollTop = w.threadBottom ? t.scrollHeight : w.threadTop
    if (!t._workspaceScroll) { t.addEventListener('scroll', () => captureThread(v), { passive: true }); t._workspaceScroll = true }
    autosize()
  }
  function syncWorkspace(v) {
    const w = workspace(v), el = $('.workspace')
    if (el) el.className = `detail workspace dock-${w.dock}`
    document.documentElement.dataset.focus = w.focus
    const bar = $('.tabs-bar'); if (bar) bar.outerHTML = tabsHTML(v)
  }
  function renderDock(v) {
    const side = $('#side'); if (!side) return
    side.innerHTML = dockHTML(v)
    syncWorkspace(v)
    C.bindScrub(side); C.pSync(); restoreThread(v)
    requestAnimationFrame(() => restoreReading(v))
  }
  function changeDock(v, kind, focusInput = false) {
    if (kind === 'ask' && !readable(v)) { toast('摘要可读后就能提问', { kind: 'info' }); return }
    captureReading(v); captureThread(v)
    const w = workspace(v)
    if (w.focus) { w.focus = ''; w.restore = null }
    if (w.dock !== 'closed') w.lastDock = w.dock
    w.dock = kind
    renderDock(v)
    if (focusInput) $('#ask-input')?.focus({ preventScroll: true })
  }
  function changeFocus(v, kind) {
    const w = workspace(v)
    captureReading(v); captureThread(v)
    if (w.focus === kind || kind === 'restore') {
      w.focus = ''; w.dock = w.restore?.dock || w.dock; w.restore = null
    } else {
      if (kind === 'ask' && !readable(v)) return
      if (!w.focus) w.restore = { dock: w.dock }
      w.focus = kind
      if (kind === 'ask') w.dock = 'ask'
    }
    renderDock(v)
    if (w.focus === 'ask') { content().scrollTo({ top: 0, behavior: 'instant' }); $('#ask-input')?.focus({ preventScroll: true }) }
  }

  /* =========================================================
     Shell
     ========================================================= */
  function shellHTML() {
    return `<div class="app">
      <aside class="rail" id="rail" aria-label="主导航">
        <a class="brand" href="#/">${C.brandMark()}<div><div class="brand-name">映知</div><div class="brand-sub">VIDLENS</div></div></a>
        <a class="nav-item" data-nav="home" href="#/" title="首页">${ic('home')}<span class="nav-label">首页</span></a>
        <span class="rail-caption" aria-hidden="true">资料</span>
        <a class="nav-item" data-nav="library" href="#/library" title="视频库">${ic('video')}<span class="nav-label">视频库</span></a>
        <a class="nav-item" data-nav="kb" href="#/kb" title="知识库">${ic('folder')}<span class="nav-label">知识库</span><span class="soon">后续</span></a>
        <span class="rail-caption" aria-hidden="true">学习</span>
        <a class="nav-item" data-nav="ask" href="#/ask" title="问答">${ic('message')}<span class="nav-label">问答</span></a>
        <div class="rail-spacer"></div>
        <div class="theme-seg" role="group" aria-label="外观主题">
          <button data-act="theme" data-theme="dark" aria-label="深色主题" title="深色 · 放映厅">${ic('moon')}</button>
          <button data-act="theme" data-theme="light" aria-label="浅色主题" title="浅色 · 阅读">${ic('sun')}</button>
        </div>
        <div class="rail-user"><span class="avatar">演</span><span class="who"><b>演示用户</b><span>原型 · 数据存于本机</span></span></div>
      </aside>
      <div class="main">
        <header class="topbar">
          <button class="topbar-menu" data-act="nav-toggle" aria-label="折叠或展开导航" aria-controls="rail">${ic('panel')}</button>
          <nav class="crumb" id="crumb" aria-label="面包屑"></nav>
          <div class="topbar-actions">
            <button class="demo-badge" data-act="console" aria-label="原型演示控制台" title="原型演示：数据均为模拟">${'<i></i>'}<span>桌面 v2 · 演示</span></button>
            <button class="btn btn-primary btn-sm import-btn" data-act="open-import" aria-label="导入视频">${ic('plus', 'sm')}<span>导入视频</span></button>
          </div>
        </header>
        <div class="content" id="content"><div id="view"></div></div>
      </div>
    </div><div class="toasts" id="toasts" aria-live="polite"></div>`
  }
  function updateShell() {
    const nav = R.name === 'detail' ? (R.tab === 'ask' ? 'ask' : 'library') : R.name === 'askIndex' ? 'ask' : R.name
    $$('.rail [data-nav]').forEach(a => { const on = a.dataset.nav === nav; a.classList.toggle('active', on); on ? a.setAttribute('aria-current', 'page') : a.removeAttribute('aria-current') })
    $$('.theme-seg button').forEach(b => b.classList.toggle('on', b.dataset.theme === S().theme))
    const v = R.id && C.getV(R.id)
    const crumbs = R.name === 'home' ? [['首页']] : R.name === 'library' ? [['视频库']] : R.name === 'kb' ? [['知识库']] : R.name === 'askIndex' ? [['问答']]
      : R.name === 'detail' && v ? [['视频库', '#/library'], [v.title]] : [['视频库', '#/library'], ['未找到']]
    $('#crumb').innerHTML = crumbs.map((c, i) => (i ? '<span class="div">/</span>' : '') + (c[1] ? `<a href="${c[1]}">${esc(c[0])}</a>` : `<b>${esc(c[0])}</b>`)).join('')
  }
  function applyPrefs() {
    const s = S(), h = document.documentElement
    h.dataset.theme = s.theme; h.dataset.font = s.reading.font; h.dataset.size = s.reading.size
    h.dataset.nav = s.ui.nav || ''
  }

  /* =========================================================
     Router
     ========================================================= */
  function parse() {
    const h = location.hash.slice(1) || '/'
    const [path, qs] = h.split('?')
    const parts = path.split('/').filter(Boolean)
    const q = new URLSearchParams(qs || '')
    if (!parts.length) return { name: 'home', q }
    if (parts[0] === 'library') return { name: 'library', q }
    if (parts[0] === 'kb') return { name: 'kb', q }
    if (parts[0] === 'ask') return { name: 'askIndex', q }
    if (parts[0] === 'v' && parts[1]) return { name: 'detail', id: parts[1], tab: parts[2] === 'ask' ? 'ask' : parts[2] === 'transcript' ? 'transcript' : 'summary', q }
    return { name: 'home', q }
  }
  function render() {
    const prev = R
    if (prev.name === 'detail') { const old = C.getV(prev.id); if (old) { captureReading(old); captureThread(old) } }
    R = parse()
    closePop(); hideSel(); closeRail()
    const keepScroll = prev.name === R.name && prev.id === R.id && prev.tab === R.tab
    const top = content().scrollTop
    const v = R.id && C.getV(R.id)
    if (v) {
      const w = workspace(v)
      if (R.tab === 'ask' && (prev.id !== R.id || prev.tab !== 'ask')) {
        w.restore = { dock: w.dock }; w.dock = 'ask'; w.focus = 'ask'
      } else if (R.tab !== 'ask' && prev.tab === 'ask' && w.focus === 'ask') {
        w.focus = ''; w.dock = w.restore?.dock || 'ask'; w.restore = null
      }
      document.documentElement.dataset.focus = w.focus
    } else document.documentElement.dataset.focus = ''
    let html
    if (R.name === 'home') html = homeHTML()
    else if (R.name === 'library') html = libraryHTML()
    else if (R.name === 'kb') html = kbHTML()
    else if (R.name === 'askIndex') html = askIndexHTML()
    else if (R.name === 'detail') html = v ? detailHTML(v) : notFoundHTML()
    content().classList.remove('no-scroll')
    $('#view').innerHTML = `<div class="view">${html}</div>`
    if (R.name === 'library') renderLibResults()
    content().scrollTop = keepScroll ? top : 0
    updateShell()
    if (v) { if (P.vid !== v.id) C.pLoad(v); C.bindScrub($('#view')); C.pSync(); restoreThread(v); requestAnimationFrame(() => restoreReading(v)) }
    if (R.name === 'home' || R.name === 'library') bindDrop()
    setTimeout(() => S().videos.forEach(x => x.tagLinks.forEach(l => delete l.fresh)), 50)
    spy()
  }
  window.addEventListener('hashchange', render)

  /* =========================================================
     Thumbs & small parts
     ========================================================= */
  function thumbHTML(v) {
    if (v.template === 'pg') return `<img src="${D.main.cover}" alt="">`
    const t = v.thumb || { hue: 30, glyph: 'MP4', sub: '本地文件' }, h = t.hue
    return `<svg viewBox="0 0 320 180" width="100%" height="100%" preserveAspectRatio="xMidYMid slice" aria-hidden="true"><rect width="320" height="180" fill="hsl(${h} 30% 14%)"/><circle cx="260" cy="40" r="120" fill="hsl(${h} 60% 45%)" opacity=".22"/><circle cx="40" cy="190" r="90" fill="hsl(${h + 30} 50% 50%)" opacity=".12"/><g stroke="hsl(${h} 40% 80%)" stroke-opacity=".06">${Array.from({ length: 12 }, (_, i) => `<path d="M${i * 28} 0V180"/>`).join('')}</g><text x="24" y="98" font-family="PingFang SC,system-ui,sans-serif" font-size="${t.glyph.length > 5 ? 38 : 48}" font-weight="800" fill="#fff">${esc(t.glyph)}</text><text x="26" y="128" font-family="PingFang SC,system-ui,sans-serif" font-size="15" fill="hsl(${h} 70% 78%)">${esc(t.sub)}</text><text x="300" y="168" text-anchor="end" font-family="PingFang SC,sans-serif" font-size="10" fill="#fff" fill-opacity=".45">演示封面</text></svg>`
  }
  const srcLabel = v => v.kind === 'file' ? `<span class="src">${ic('file', 'sm')}本地文件</span>` : `<span class="src"><span class="bili">B站</span>${esc(v.up)}</span>`
  function stageText(v) {
    const p = v.pipe || {}
    if (v.status === 'failed') return '摘要未生成'
    if (v.status === 'idle') return '未生成摘要'
    return { receive: v.kind === 'file' ? '正在上传' : '正在接收视频', source: v.kind === 'file' ? '正在识别语音' : '正在读取字幕', organize: '正在整理内容', visual: '正在补充画面', tags: '正在归类' }[p.stage] || ''
  }
  function statusChip(v, card) {
    const figs = Object.values(v.figs), ok = figs.filter(x => x === 'ok').length, total = figs.filter(x => x !== 'skipped').length
    if (v.status === 'processing') return `<span class="chip chip-acc"><span class="dot pulse"></span>${stageText(v)}</span>`
    if (v.status === 'text_ready') return `<span class="chip chip-info"><span class="dot pulse"></span>摘要可读 · ${v.pipe.stage === 'visual' ? '补充画面 ' + ok + '/' + total : '正在归类'}</span>`
    if (v.status === 'partial') return `<span class="chip chip-warn">${ic('alert')}${figs.filter(x => x === 'failed').length} 张画面未补充</span>`
    if (v.status === 'failed') return `<span class="chip chip-bad">${ic('alert')}摘要未生成</span>`
    if (v.status === 'idle') return `<span class="chip">未生成摘要</span>`
    if (card && v.pipe?.doneAt && Date.now() - v.pipe.doneAt < 15 * 60e3) return `<span class="chip chip-ok">${ic('check')}刚完成</span>`
    return ''
  }
  function tagChips(v, max) {
    const links = v.tagLinks.filter(l => S().tags[l.id])
    const shown = max ? links.slice(0, max) : links
    return shown.map(l => `<a class="tag${l.origin === 'manual' ? ' manual' : ''}${l.fresh ? ' new' : ''}" href="#/library?tags=${l.id}" title="${l.origin === 'manual' ? '你添加或保留的标签' : 'Agent 自动添加'}">${esc(S().tags[l.id].name)}</a>`).join('') + (max && links.length > max ? `<span class="tag-more">+${links.length - max}</span>` : '')
  }

  /* =========================================================
     Home / Import
     ========================================================= */
  function homeHTML() {
    const recent = S().videos.filter(readable).slice().sort((a, b) => b.updatedAt - a.updatedAt).slice(0, 3)
    return `<div class="home">
      <div class="home-hero">
        <div class="eyebrow">${ic('sparkles', 'sm')}视频摘要 · 原型演示</div>
        <h1>导入视频，得到一份可以回看的摘要</h1>
        <p>粘贴 B 站链接或选择本地文件，写下你关心的内容。字幕、截图、分类会自动完成。</p>
      </div>
      ${importFormHTML('home')}
      <div class="import-steps"><span>${ic('subtitles', 'sm')}优先使用平台字幕</span><span>${ic('photo', 'sm')}需要时自动配上画面</span><span>${ic('tag', 'sm')}自动归类、去重</span><span>${ic('message', 'sm')}读完可以直接选段提问</span></div>
      <section class="home-section"><h2>最近的摘要<a href="#/library">全部视频${ic('chev-r', 'sm')}</a></h2><div class="recent-list" id="recent-slot">${recent.map(recentHTML).join('') || '<p class="muted">还没有摘要。</p>'}</div></section>
      <section class="home-section"><div class="later-card">${ic('folder')}<div><b>知识库与跨视频问答 · 后续开放</b>按标签把多个视频组成知识库，一起提问。本原型优先完成单个视频的摘要、提问和修订。</div></div></section>
    </div>`
  }
  function recentHTML(v) {
    const doc = C.curDoc(v)
    return `<a class="recent-item" href="#/v/${v.id}"><div class="thumb">${thumbHTML(v)}</div><div class="grow"><b>${esc(v.title)}</b><p>${esc(plain(doc.overview))}</p></div>${statusChip(v) || `<span class="btn btn-ghost btn-sm">${ic('book', 'sm')}阅读</span>`}</a>`
  }
  function importFormHTML(ctx) {
    const I = IMP
    return `<div class="import-card" data-import="${ctx}">
      <div class="import-tabs" role="tablist">
        <button role="tab" aria-selected="${I.tab === 'url'}" class="${I.tab === 'url' ? 'on' : ''}" data-act="imp-tab" data-tab="url">${ic('link', 'sm')}B 站链接</button>
        <button role="tab" aria-selected="${I.tab === 'file'}" class="${I.tab === 'file' ? 'on' : ''}" data-act="imp-tab" data-tab="file">${ic('upload', 'sm')}本地文件</button>
      </div>
      <div data-imp="source">${impSourceHTML()}</div>
      <div class="focus-field">
        <label class="field-label" for="focus-${ctx}">你最关心什么？<small>可选，Agent 会按这个组织摘要</small></label>
        <textarea class="textarea" id="focus-${ctx}" data-imp-focus maxlength="2000" placeholder="例如：重点整理配置步骤和常见错误">${esc(I.focus)}</textarea>
        <div class="focus-examples" data-imp="examples">${impExamplesHTML()}</div>
      </div>
      <div class="import-more${I.more ? ' open' : ''}" data-imp="more">${impMoreHTML()}</div>
      <div class="import-foot">
        <span class="note">${ic('info', 'sm')}原型演示：不会上传文件或访问 B 站，处理过程为模拟</span>
        <span data-imp="submit">${impSubmitHTML()}</span>
      </div>
    </div>`
  }
  function impSourceHTML() {
    const I = IMP
    if (I.tab === 'url') {
      let prev = ''
      if (I.parsed === 'loading') prev = `<div class="source-preview loading"><span class="thumb-sm"></span><div class="grow"><b></b><span></span></div></div>`
      else if (I.parsed) {
        const dup = C.getV('pg')
        prev = `<div class="source-preview"><img src="${D.main.cover}" alt=""><div class="grow"><b>${esc(D.main.doc.title)}</b><span>${I.parsed.short ? '短链已解析 · ' : ''}${D.main.bv} · P${I.parsed.part} / 共 ${D.main.parts} P · ${esc(D.main.up)} · ${fmt(D.main.duration)}</span>${dup ? `<span style="display:block;color:var(--acc-strong);margin-top:2px">这个视频已在视频库中${readable(dup) ? '，摘要可以直接阅读' : '，正在处理'}</span>` : ''}</div></div>`
      }
      return `<div class="url-row">${ic('link')}
          <input class="input" data-imp-url value="${esc(I.url)}" placeholder="粘贴 B 站视频链接：bilibili.com/video/BV… 或 b23.tv/…" aria-label="B 站视频链接" autocomplete="off" spellcheck="false">
          <button class="btn btn-ghost btn-sm" data-act="imp-sample">${ic('clipboard', 'sm')}示例链接</button>
        </div>${I.err ? `<div class="field-error">${ic('alert', 'sm')}${esc(I.err)}</div>` : ''}${prev}`
    }
    if (I.file) return `<div class="source-preview"><span class="thumb-sm" style="display:grid;place-items:center;color:var(--acc)">${ic('video', 'lg')}</span><div class="grow"><b>${esc(I.file.name)}</b><span>${esc(I.file.size)} · 本地文件会使用语音识别 · 演示中不会真正上传</span></div><button class="btn btn-ghost btn-sm" data-act="imp-file-clear">更换</button></div>`
    return `<div class="dropzone" data-act="imp-file" data-drop role="button" tabindex="0" aria-label="选择本地视频">${ic('upload')}<b>拖入视频文件，或点击选择</b><span>单文件 2 GB 以内 · 演示中只读取文件名和大小</span></div><input type="file" accept="video/*" hidden data-imp-file>`
  }
  function impExamplesHTML() { return D.focusExamples.map(x => `<button data-act="imp-ex" class="${IMP.focus === x ? 'on' : ''}">${esc(x)}</button>`).join('') }
  function impMoreHTML() {
    const I = IMP
    const seg = (k, opts) => `<div class="seg seg-sm" role="radiogroup">${opts.map(([val, label]) => `<button role="radio" aria-checked="${I[k] === val}" class="${I[k] === val ? 'on' : ''}" data-act="imp-opt" data-k="${k}" data-val="${val}">${label}</button>`).join('')}</div>`
    const modeHint = { auto: 'Agent 根据视频内容选择文字、图文或关键帧', text: '只生成文字摘要，不补充画面', image_text: '正文旁配上必要的视频截图', keyframes: '以几张关键画面为主，配图注和回放时间' }[I.mode]
    const sum = [{ auto: '形式自动', text: '文字为主', image_text: '图文', keyframes: '关键帧' }[I.mode], I.visual ? '按需补图' : '不补图', I.tags ? '自动分类' : '不分类', I.importOnly ? '仅导入' : ''].filter(Boolean).join(' · ')
    return `<button data-act="imp-more" aria-expanded="${I.more}">${ic('chev-r', 'sm').replace('class="ic', 'class="ic chev')}更多选项<span class="summary-line">${sum}</span></button>
      <div class="collapse${I.more ? ' open' : ''}"><div><div class="opt-grid">
        <div class="opt-block"><div class="field-label">摘要形式</div>${seg('mode', [['auto', '自动'], ['text', '文字'], ['image_text', '图文'], ['keyframes', '关键帧']])}<p class="hint">${modeHint}</p></div>
        <div class="opt-block"><div class="field-label">思维导图</div>${seg('map', [['auto', '自动'], ['open', '默认展开'], ['closed', '默认收起']])}<p class="hint">自动：桌面端展开，手机端收起</p></div>
        <label class="switch"><input type="checkbox" data-imp-check="visual" ${I.visual ? 'checked' : ''} ${I.mode === 'text' ? 'disabled' : ''}><span class="track"></span><span><b>按需补充画面</b><span class="d">遇到配置、代码、图表时截取对应画面</span></span></label>
        <label class="switch"><input type="checkbox" data-imp-check="tags" ${I.tags ? 'checked' : ''}><span class="track"></span><span><b>自动分类到标签</b><span class="d">优先复用已有标签，自动去重</span></span></label>
        <label class="switch"><input type="checkbox" data-imp-check="importOnly" ${I.importOnly ? 'checked' : ''}><span class="track"></span><span><b>仅导入，稍后再生成摘要</b><span class="d">适合先收藏、之后再处理</span></span></label>
      </div></div></div>`
  }
  function impSubmitHTML() {
    const I = IMP, dup = I.tab === 'url' && I.parsed && I.parsed !== 'loading' && C.getV('pg')
    if (dup) return `<button class="btn btn-primary btn-lg" data-act="imp-read">${ic('book')}阅读已有摘要</button>`
    return `<button class="btn btn-primary btn-lg" data-act="imp-submit">${I.importOnly ? '导入视频' : '生成摘要'}${ic('arrow-r')}</button>`
  }
  function impRefresh(parts) {
    $$('[data-import]').forEach(root => parts.forEach(p => {
      const el = root.querySelector(`[data-imp="${p}"]`); if (!el) return
      if (p === 'source') el.innerHTML = impSourceHTML()
      if (p === 'examples') el.innerHTML = impExamplesHTML()
      if (p === 'more') { el.innerHTML = impMoreHTML(); el.classList.toggle('open', IMP.more) }
      if (p === 'submit') el.innerHTML = impSubmitHTML()
    }))
    $$('[data-import] .import-tabs button').forEach(b => { b.classList.toggle('on', b.dataset.tab === IMP.tab); b.setAttribute('aria-selected', b.dataset.tab === IMP.tab) })
    bindDrop()
  }
  let parseTimer
  function onUrlInput(val) {
    IMP.url = val; IMP.err = ''; clearTimeout(parseTimer)
    const s = val.trim()
    if (!s) { IMP.parsed = null; impRefresh(['source', 'submit']); refocusUrl(); return }
    parseTimer = setTimeout(() => {
      const m = s.match(/bilibili\.com\/video\/(BV[0-9A-Za-z]{10})/i), sh = /b23\.tv\/[0-9A-Za-z]+/i.test(s)
      if (!m && !sh) { IMP.parsed = null; IMP.err = '这不是 B 站视频链接。支持 bilibili.com/video/BV… 和 b23.tv 短链'; impRefresh(['source', 'submit']); refocusUrl(); return }
      const pm = s.match(/[?&]p=([^&#]*)/)
      if (pm && !/^[1-9]\d*$/.test(pm[1])) { IMP.parsed = null; IMP.err = '分 P 参数无效：p 需要是正整数'; impRefresh(['source', 'submit']); refocusUrl(); return }
      if (pm && +pm[1] > D.main.parts) { IMP.parsed = null; IMP.err = `这个视频只有 ${D.main.parts} 个分 P，没有 P${pm[1]}（演示数据）`; impRefresh(['source', 'submit']); refocusUrl(); return }
      IMP.parsed = 'loading'; impRefresh(['source', 'submit']); refocusUrl()
      setTimeout(() => { if (IMP.url.trim() !== s) return; IMP.parsed = { part: pm ? +pm[1] : 1, short: sh }; impRefresh(['source', 'submit']); refocusUrl() }, 650)
    }, 320)
  }
  function refocusUrl() { const a = document.activeElement; if (a && a.matches?.('[data-imp-url]')) return; const el = $$('[data-imp-url]').find(x => x.closest('[data-import]')?.dataset.import === (dialogEl() ? 'dlg' : 'home')); if (el && lastUrlFocus) { el.focus(); el.setSelectionRange(el.value.length, el.value.length) } }
  let lastUrlFocus = false
  function submitImport() {
    const I = IMP
    const prefs = { mode: I.mode, map: I.map, visual: I.mode !== 'text' && I.visual, tags: I.tags, importOnly: I.importOnly }
    let v
    if (I.tab === 'url') {
      if (!I.url.trim()) { I.err = '先粘贴一个 B 站视频链接，或点「示例链接」'; impRefresh(['source']); return }
      if (I.err) return
      if (I.parsed === 'loading' || !I.parsed) { toast('正在解析链接，请稍候', { kind: 'info' }); return }
      v = C.baseVideo({ id: 'pg', template: 'pg', title: D.main.doc.title, kind: 'bili', up: D.main.up, bv: D.main.bv, part: I.parsed.part, duration: D.main.duration, focus: I.focus.trim(), prefs, createdAt: Date.now() })
    } else {
      if (!I.file) { toast('先选择一个本地视频文件', { kind: 'info' }); return }
      v = C.baseVideo({ id: 'f' + Date.now().toString(36), template: 'pg', title: I.file.name.replace(/\.[^.]+$/, ''), kind: 'file', file: I.file.name, size: I.file.size, duration: D.main.duration, focus: I.focus.trim(), prefs, createdAt: Date.now() })
    }
    S().videos.unshift(v)
    C.startPipe(v)
    Object.assign(IMP, freshImport())
    if (dialogEl()) closeDialog(true)
    go('#/v/' + v.id)
    toast(prefs.importOnly ? '已导入，可以稍后生成摘要' : '已开始处理，离开页面也会继续', { kind: 'info' })
  }
  function openImportDialog() {
    const el = openDialog(`<div class="dlg-head"><div class="grow"><h3>导入视频</h3><p>导入后会自动整理成摘要，不需要逐步启动转写或截图</p></div><button class="btn btn-ghost btn-sm btn-ic" data-act="dlg-close" aria-label="关闭">${ic('x')}</button></div><div class="dlg-body" style="padding-bottom:22px">${importFormHTML('dlg')}</div>`, { width: 720 })
    el.querySelector('.import-card').style.cssText = 'box-shadow:none;border:0;padding:0;background:transparent'
    el.querySelector('.import-card').classList.add('flat')
    bindDrop()
  }
  function bindDrop() {
    $$('[data-drop]').forEach(z => {
      if (z._b) return; z._b = true
      z.addEventListener('dragover', e => { e.preventDefault(); z.classList.add('over') })
      z.addEventListener('dragleave', () => z.classList.remove('over'))
      z.addEventListener('drop', e => { e.preventDefault(); z.classList.remove('over'); const f = e.dataTransfer.files[0]; if (f) pickFile(f) })
    })
    $$('[data-imp-file]').forEach(inp => { if (inp._b) return; inp._b = true; inp.addEventListener('change', () => { if (inp.files[0]) pickFile(inp.files[0]) }) })
  }
  function pickFile(f) {
    const mb = f.size / 1048576
    IMP.file = { name: f.name, size: mb > 1024 ? (mb / 1024).toFixed(1) + ' GB' : Math.max(1, Math.round(mb)) + ' MB' }
    impRefresh(['source', 'submit'])
  }

  /* =========================================================
     Library
     ========================================================= */
  function libQuery() {
    const q = R.q || new URLSearchParams()
    const tags = (q.get('tags') || '').split(',').filter(Boolean).map(C.resolveTag).filter(Boolean)
    return { kw: q.get('q') || '', tags: Array.from(new Set(tags)), match: q.get('match') === 'any' ? 'any' : 'all', st: ['ready', 'processing', 'attention'].includes(q.get('st')) ? q.get('st') : 'all' }
  }
  function setLib(patch) {
    const f = Object.assign(libQuery(), patch)
    const q = new URLSearchParams()
    if (f.kw) q.set('q', f.kw); if (f.tags.length) q.set('tags', f.tags.join(',')); if (f.match === 'any') q.set('match', 'any'); if (f.st !== 'all') q.set('st', f.st)
    const h = '#/library' + (q.toString() ? '?' + q : '')
    history.replaceState(null, '', h); R.q = q
    renderLibResults()
  }
  function filterVideos(f) {
    const kw = f.kw.trim().toLowerCase()
    return S().videos.filter(v => {
      if (f.st === 'ready' && !readable(v)) return false
      if (f.st === 'processing' && !(v.status === 'processing' || v.status === 'text_ready')) return false
      if (f.st === 'attention' && !['failed', 'partial', 'idle'].includes(v.status)) return false
      if (f.tags.length) {
        const has = f.tags.map(id => v.tagLinks.some(l => l.id === id))
        if (f.match === 'all' ? !has.every(Boolean) : !has.some(Boolean)) return false
      }
      if (kw) {
        const doc = C.curDoc(v)
        const hay = (v.title + ' ' + (doc ? doc.overview + doc.keyPoints.join('') : '') + ' ' + v.tagLinks.map(l => { const t = S().tags[l.id]; return t ? t.name + t.aliases.join(' ') : '' }).join(' ')).toLowerCase()
        if (!hay.includes(kw)) return false
      }
      return true
    })
  }
  function libraryHTML() {
    const f = libQuery()
    return `<div class="page page-wide" style="max-width:1320px">
      <div class="page-head"><div class="grow"><h1>视频库</h1><p>按标签和关键词查找视频，打开就能阅读摘要</p></div>
        <button class="btn" data-act="tag-manager">${ic('tag', 'sm')}管理标签</button>
      </div>
      <div class="lib-toolbar">
        <div class="search">${ic('search')}<input class="input" data-lib-search value="${esc(f.kw)}" placeholder="搜索标题、摘要或标签…" aria-label="搜索视频"></div>
        <div class="seg" role="radiogroup" aria-label="处理状态" id="lib-st">${stSegHTML(f)}</div>
      </div>
      <div class="tagbar" id="lib-tagbar">${tagbarHTML(f)}</div>
      <div class="filter-summary" id="lib-summary"></div>
      <div id="lib-grid"></div>
    </div>`
  }
  function stSegHTML(f) { return [['all', '全部'], ['ready', '可阅读'], ['processing', '处理中'], ['attention', '需处理']].map(([k, l]) => `<button role="radio" aria-checked="${f.st === k}" class="${f.st === k ? 'on' : ''}" data-act="lib-st" data-st="${k}">${l}</button>`).join('') }
  function tagbarHTML(f) {
    const tags = Object.values(S().tags).map(t => ({ t, n: C.tagCount(t.id) })).filter(x => x.n > 0 || f.tags.includes(x.t.id)).sort((a, b) => b.n - a.n || a.t.name.localeCompare(b.t.name, 'zh'))
    return `<span class="tagbar-label">${ic('tag', 'sm')}标签</span>
      <div class="tagbar-list">${window.DesktopLibrary.tagBarHTML(tags, f.tags)}</div>
      <div class="tagbar-tools">${f.tags.length > 1 ? `<div class="seg seg-sm" role="radiogroup" aria-label="多个标签的匹配方式"><button role="radio" class="${f.match === 'all' ? 'on' : ''}" data-act="lib-match" data-m="all" aria-checked="${f.match === 'all'}">同时包含</button><button role="radio" class="${f.match === 'any' ? 'on' : ''}" data-act="lib-match" data-m="any" aria-checked="${f.match === 'any'}">包含任一</button></div>` : ''}</div>`
  }
  function renderLibResults() {
    const f = libQuery(), list = filterVideos(f), all = S().videos.length
    $('#lib-tagbar').innerHTML = tagbarHTML(f)
    $('#lib-st').innerHTML = stSegHTML(f)
    const active = f.kw || f.tags.length || f.st !== 'all'
    const names = f.tags.map(id => '「' + esc(S().tags[id].name) + '」').join('')
    $('#lib-summary').innerHTML = !all ? '' : active ? `<span>找到 <b>${list.length}</b> 个视频${f.tags.length ? ' · ' + (f.tags.length > 1 ? (f.match === 'all' ? '同时包含 ' : '包含任一 ') : '标签 ') + names : ''}${f.kw ? ' · 关键词「' + esc(f.kw) + '」' : ''}</span><button class="btn btn-ghost btn-xs" data-act="lib-clear">${ic('x', 'sm')}清除筛选</button>` : `<span>共 ${all} 个视频 · ${S().videos.filter(readable).length} 个摘要可阅读</span>`
    let body
    if (!all) body = `<div class="empty"><div class="art">${ic('video')}</div><h3>视频库还是空的</h3><p>导入一个 B 站链接或本地视频，几分钟后就能阅读摘要。导入时可以写下你关心的重点。</p><div class="acts"><button class="btn btn-primary" data-act="open-import">${ic('plus', 'sm')}导入第一个视频</button><button class="btn btn-quiet" data-act="con-restore">恢复示例视频库（演示）</button></div></div>`
    else if (!list.length) body = `<div class="empty"><div class="art">${ic('search')}</div><h3>${f.tags.length > 1 && f.match === 'all' ? '没有同时包含 ' + names + ' 的视频' : '没有符合条件的视频'}</h3><p>${f.tags.length > 1 && f.match === 'all' ? '可以改为「包含任一」，或减少一个标签。' : '换个关键词，或清除筛选查看全部视频。'}</p><div class="acts">${f.tags.length > 1 && f.match === 'all' ? `<button class="btn" data-act="lib-match" data-m="any">改为包含任一</button>` : ''}<button class="btn btn-quiet" data-act="lib-clear">清除筛选</button></div></div>`
    else body = `<div class="video-grid">${list.map((v, i) => cardHTML(v, i)).join('')}</div>`
    $('#lib-grid').innerHTML = body
  }
  function cardHTML(v, i = 0, noAnim) {
    const doc = C.curDoc(v), figs = Object.values(v.figs), ok = figs.filter(x => x === 'ok').length, total = figs.filter(x => x !== 'skipped').length
    const pre = v.status === 'processing' && !readable(v)
    const idx = C.ORDER.indexOf(v.pipe?.stage)
    let mid
    if (pre) mid = `<div class="vprog"><div class="row"><b>${stageText(v)}${v.pipe.stage === 'receive' && v.kind === 'file' ? ' ' + (v.pipe.upload || 0) + '%' : v.pipe.stage === 'organize' ? ' · ' + v.pipe.organized + '/' + C.template(v).blocks.length + ' 章' : ''}</b><span>${Math.max(1, idx + 1)}/5</span></div><div class="meter"><i style="width:${Math.max(6, (idx + (v.pipe.stage === 'organize' ? v.pipe.organized / C.template(v).blocks.length : .4)) / 5 * 100)}%"></i></div></div><p class="vsum pending">文字整理好后就能阅读，离开页面也会继续处理</p>`
    else if (v.status === 'failed') mid = `<p class="vfail">${ic('alert', 'sm')}<span>${esc(v.pipe.fail?.msg || '处理失败')}。视频和字幕已保存，重试不会重新下载。</span></p>`
    else if (v.status === 'idle') mid = `<p class="vsum pending">已导入，尚未生成摘要</p>`
    else mid = `<p class="vsum">${esc(plain(doc.overview))}</p>${v.status === 'text_ready' ? `<div class="vprog"><div class="row"><span>${v.pipe.stage === 'visual' ? '正在补充画面' : '正在归类'}</span><span>${v.pipe.stage === 'visual' ? ok + '/' + total : ''}</span></div><div class="meter${v.pipe.stage === 'tags' ? ' indet' : ''}"><i style="width:${total ? ok / total * 100 : 50}%"></i></div></div>` : ''}`
    const primary = readable(v) ? `<a class="btn btn-primary btn-sm" href="#/v/${v.id}">${ic('book', 'sm')}阅读摘要</a>`
      : v.status === 'failed' ? `<button class="btn btn-primary btn-sm" data-act="lib-retry" data-id="${v.id}">${ic('refresh', 'sm')}重试生成</button>`
        : v.status === 'idle' ? `<a class="btn btn-primary btn-sm" href="#/v/${v.id}">${ic('sparkles', 'sm')}生成摘要</a>`
          : `<a class="btn btn-sm" style="flex:1" href="#/v/${v.id}">${ic('clock', 'sm')}查看进度</a>`
    const chip = statusChip(v, true)
    return `<article class="vcard${noAnim ? ' no-anim' : ''}" data-card="${v.id}" style="animation-delay:${Math.min(i, 8) * 35}ms">
      <a class="thumb" href="#/v/${v.id}" tabindex="-1" aria-hidden="true">${thumbHTML(v)}${chip ? `<span class="vstate">${chip}</span>` : ''}<span class="vlen">${fmt(v.duration)}</span></a>
      <div class="vbody">
        <h3><a href="#/v/${v.id}">${esc(v.title)}</a></h3>
        <div class="vmeta">${srcLabel(v)}<span class="sep">·</span><span>${ago(v.updatedAt)}</span></div>
        ${mid}
        <div class="vtags">${tagChips(v, 3) || (v.status === 'processing' || v.status === 'text_ready' ? '<span class="tag-more">归类会在摘要完成后进行</span>' : '')}</div>
      </div>
      <div class="vfoot">${primary}${readable(v) ? `<a class="btn btn-sm btn-ic" href="#/v/${v.id}/ask" aria-label="围绕摘要提问" title="提问">${ic('message', 'sm')}</a>` : ''}</div>
    </article>`
  }

  /* =========================================================
     Detail
     ========================================================= */
  function detailHTML(v) {
    const w = workspace(v)
    return `${headHTML(v)}${tabsHTML(v)}
      <div class="detail workspace dock-${w.dock}">
        <div class="reading" id="reading" data-kind="${R.tab === 'transcript' ? 'transcript' : 'summary'}">${R.tab === 'transcript' ? transcriptHTML(v) : summaryHTML(v)}</div>
        <aside class="side dock" id="side" aria-label="回放与问答">${dockHTML(v)}</aside>
      </div>`
  }
  function headHTML(v) {
    return `<div class="detail-head workspace-head">
      <div class="head-main"><h1 title="${esc(v.title)}">${esc(v.title)}</h1>
        <div class="head-meta">${srcLabel(v)}<span class="sep">·</span><span>${fmt(v.duration)}</span>
          <span class="head-tags" id="tags-slot">${tagsHTML(v)}</span>
        </div>
      </div>
      <div class="head-tools"><button class="btn btn-ghost btn-sm btn-ic" data-act="video-info" aria-label="视频信息" title="视频信息">${ic('info')}</button></div>
    </div>`
  }
  function tagsHTML(v) {
    const st = v.pipe?.stage
    if (!v.tagLinks.length && v.prefs.tags && (v.status === 'processing' || v.status === 'text_ready') && st !== 'tags') return `<span class="tag-state">${ic('tag', 'sm')}摘要完成后会自动归类</span>`
    if (st === 'tags' && v.status !== 'ready') return `${tagChips(v)}<span class="tag-state"><span class="spinner" style="width:12px;height:12px"></span>正在归类，优先复用已有标签…</span>`
    return `${tagChips(v)}<button class="add-tag" data-act="tag-edit" aria-haspopup="dialog">${ic(v.tagLinks.length ? 'pencil' : 'plus', 'sm')}${v.tagLinks.length ? '编辑标签' : '添加标签'}</button>`
  }
  function tabsHTML(v) {
    const w = workspace(v)
    const n = v.chat.msgs.filter(m => m.role === 'user').length
    const tab = (k, label, icon, extra = '') => `<a class="tab${R.tab === k ? ' on' : ''}" href="#/v/${v.id}${k === 'summary' ? '' : '/' + k}" ${R.tab === k ? 'aria-current="page"' : ''}>${ic(icon, 'sm')}${label}${extra}</a>`
    const can = readable(v)
    return `<div class="tabs-bar"><div class="tabs-inner">
      ${tab('summary', '摘要', 'book')}${tab('transcript', '字幕', 'subtitles')}
      <span class="spacer"></span>
      ${w.focus ? `<button class="btn btn-sm focus-button" data-act="focus-restore">${ic('panel', 'sm')}恢复布局</button>` : `<button class="btn btn-ghost btn-sm focus-button" data-act="focus-read">${ic('maximize', 'sm')}放大阅读</button>`}
      ${can ? `<button class="btn ${w.dock === 'ask' ? 'btn-quiet' : 'btn-ghost'} btn-sm" data-act="open-ask">${ic('message', 'sm')}提问${n ? `<span class="count">${n}</span>` : ''}</button><button class="btn btn-ghost btn-sm" data-act="open-edit">${ic('wand', 'sm')}修改摘要</button>` : ''}
      <button class="btn btn-ghost btn-sm btn-ic" data-act="dock-toggle" aria-label="${w.dock === 'closed' ? '展开辅助面板' : '收起辅助面板'}" title="${w.dock === 'closed' ? '展开辅助面板' : '收起辅助面板'}">${ic('panel', 'sm')}</button>
      <button class="btn btn-ghost btn-sm btn-ic" data-act="more" aria-label="更多操作" aria-haspopup="menu">${ic('dots')}</button>
    </div></div>`
  }
  function dockHTML(v) {
    const w = workspace(v), ask = w.dock === 'ask', ver = C.curVer(v)
    return `<div class="dock-head">
      <div class="dock-tabs" role="tablist" aria-label="辅助面板">
        <button role="tab" aria-selected="${!ask}" class="${ask ? '' : 'on'}" data-act="dock-tab" data-tab="player">${ic('video', 'sm')}回放</button>
        <button role="tab" aria-selected="${ask}" class="${ask ? 'on' : ''}" data-act="dock-tab" data-tab="ask" ${readable(v) ? '' : 'disabled'}>${ic('message', 'sm')}问答</button>
      </div>
      <div class="dock-actions">${ask ? `<button class="btn btn-ghost btn-xs btn-ic" data-act="focus-ask" aria-label="${w.focus === 'ask' ? '恢复布局' : '放大问答'}" title="${w.focus === 'ask' ? '恢复布局' : '放大问答'}">${ic('maximize', 'sm')}</button>` : ''}<button class="btn btn-ghost btn-xs btn-ic" data-act="dock-toggle" aria-label="收起辅助面板">${ic('x', 'sm')}</button></div>
    </div>
    <div class="dock-body">${ask ? `<div class="dock-chat">
      <div class="dock-context"><span>${ic('book', 'sm')}依据摘要 v${ver?.n || 1}</span>${v.chat.msgs.length ? `<button class="btn btn-ghost btn-xs" data-act="ask-new">${ic('plus', 'sm')}新对话</button>` : '<span>可选段、引用截图</span>'}</div>
      <div class="ask-thread" id="thread"><div class="thread-inner" id="thread-inner">${threadHTML(v)}</div></div>
      <div class="composer-wrap" id="composer-wrap">${composerHTML(v)}</div>
    </div>` : sideHTML(v)}</div>`
  }
  function summaryHTML(v) {
    return `<div id="progress-slot">${progressHTML(v)}</div>
      <div id="regen-slot">${regenHTML(v)}</div>
      <div class="m-player" id="m-player">${mPlayerHTML(v)}</div>
      <div id="doc-slot">${docHTML(v)}</div>`
  }
  function progressHTML(v) {
    return window.ProgressView.render(v, !!S().ui.logOpen[v.id])
  }
  function regenHTML(v) {
    if (!v.pendingRegen) return ''
    const cur = C.curVer(v)
    return `<div class="notice info merge-banner">${ic('info')}<div class="grow"><b>后台生成了新的摘要原稿</b><div>${esc(v.pendingRegen.note)}你修改过的版本（v${cur.n}）会继续显示，不会被覆盖。</div><div class="acts"><button class="btn btn-sm" data-act="regen-view">${ic('eye', 'sm')}查看变化</button><button class="btn btn-sm btn-quiet" data-act="regen-keep">保留我的版本</button><button class="btn btn-sm btn-quiet" data-act="regen-adopt">采用新原稿</button></div></div></div>`
  }
  function viewOf(v) {
    const doc = C.curDoc(v), vis = C.wantsVisual(v)
    const set = S().ui.view[v.id]
    if (set && (set === 'text' || vis)) return set
    if (!vis) return 'text'
    if (v.prefs.mode === 'keyframes') return 'frames'
    if (v.prefs.mode === 'text') return 'text'
    return doc.resolvedMode === 'image_text' ? 'rich' : 'text'
  }
  function docHTML(v) {
    const doc = C.curDoc(v)
    if (!doc) {
      const p = v.pipe || {}, n = C.template(v).blocks.length
      if (p.stage === 'idle') return `<div class="empty" style="padding:44px 24px"><div class="art">${ic('sparkles')}</div><h3>还没有摘要</h3><p>点击上方「生成摘要」，几十秒后就能阅读。</p></div>`
      const org = p.organized || 0, dim = p.stage === 'failed' ? ' style="opacity:.45"' : ''
      return `<div aria-hidden="true"${dim}><div class="section-label">概览</div><div class="skel-block"><div class="skel-line" style="width:96%"></div><div class="skel-line" style="width:88%"></div><div class="skel-line" style="width:62%"></div></div>
        ${C.template(v).blocks.map((b, i) => i < org ? `<div class="skel-title"><span class="ch-num">${String(i + 1).padStart(2, '0')}</span>${esc(b.title)}</div><div class="skel-block"><div class="skel-line" style="width:${92 - i * 3}%"></div><div class="skel-line" style="width:${78 + i * 2}%"></div></div>` : i === org && p.stage !== 'failed' ? `<div class="skel-block" style="opacity:.6"><div class="skel-line" style="width:40%;height:16px"></div><div class="skel-line" style="width:84%"></div></div>` : '').join('')}
        ${org < n ? '' : ''}</div>`
    }
    const view = viewOf(v), vis = C.wantsVisual(v), ver = C.curVer(v)
    const seg = [['rich', '图文', 'photo'], ['text', '文字', 'text'], ['frames', '关键帧', 'grid']].map(([k, l, i]) => `<button role="tab" aria-selected="${view === k}" class="${view === k ? 'on' : ''}" data-act="view" data-view="${k}" ${k !== 'text' && !vis ? 'disabled title="这份摘要没有补充画面"' : ''}>${ic(i, 'sm')}${l}</button>`).join('')
    const modeNote = !vis ? (v.prefs.mode === 'text' || !v.prefs.visual ? '按你的选择生成文字摘要，未补充画面' : doc.modeReason) : v.prefs.mode === 'auto' ? doc.modeReason + '。三种形式使用同一份内容' : '按你选择的形式生成。三种形式使用同一份内容'
    return `<div class="read-toolbar">
        <div class="seg" role="tablist" aria-label="摘要形式">${seg}</div>
        <button class="btn btn-ghost btn-sm btn-ic" data-act="read-settings" aria-label="阅读设置" title="阅读设置">${ic('type')}</button>
        <span class="spacer"></span>
        <button class="ver-btn" data-act="versions" aria-haspopup="dialog">${ic('history', 'sm')}<b>v${ver.n}</b><span>${esc(ver.label)}</span>${ic('chev-d', 'sm')}</button>
      </div>
      <p class="mode-note"><button class="mode-info" data-act="summary-info" title="${esc(modeNote)}">${ic('sparkles', 'sm')}Agent 已整理 · 查看内容来源</button></p>
      ${overviewHTML(v, doc)}
      ${mapHTML(v, doc)}
      ${view === 'frames' ? framesHTML(v, doc) : doc.blocks.map((b, i) => chapterHTML(v, doc, b, i, view === 'rich')).join('')}
      <div class="doc-footer"><span>摘要 v${ver.n} · ${esc(ver.label)} · ${ago(ver.at)}</span><span>来源：${v.kind === 'file' ? '语音识别' : '平台字幕（中文）'}</span>${vis ? `<span>画面来自原视频${v.kind === 'bili' ? ' P' + (v.part || 1) : ''}</span>` : ''}<button class="btn btn-ghost btn-xs" data-act="export">${ic('download', 'sm')}导出 Markdown</button></div>`
  }
  function revisedSet(v) { const ver = C.curVer(v); return new Set(ver && ver.by === 'revise' ? ver.changed || [] : []) }
  function overviewHTML(v, doc) {
    const rs = revisedSet(v)
    return `<section class="overview" id="blk-overview" data-block-sec="overview">
      ${doc.focus ? `<div class="focus-echo">${ic('target', 'sm')}<span>按你的关注点整理：${esc(doc.focus)}</span></div>` : ''}
      <div class="section-label">概览</div>
      <div class="prose lead" data-selectable data-block="overview"><div class="para${rs.has('overview') ? ' revised' : ''}" data-para="overview">${md(doc.overview)}</div></div>
      <ol class="points">${doc.keyPoints.map(k => `<li>${inline(k)}</li>`).join('')}</ol>
      ${doc.fit ? `<p class="fit-line">${ic('info', 'sm')}<span>${esc(doc.fit)}</span></p>` : ''}
    </section>`
  }
  function mapHTML(v, doc) {
    const open = S().ui.mapOpen[v.id] ?? (v.prefs.map === 'open')
    UIS.mapFold[v.id] ||= Object.fromEntries(doc.blocks.map(b => [b.id, true]))
    const m = window.MindMap.build(doc, { folded: UIS.mapFold[v.id], changed: UIS.mapChanged[v.id] })
    return `<section class="map-wrap${open ? ' open' : ''}" id="map">
      <div class="map-head"><button class="toggle" data-act="map-toggle" aria-expanded="${open}" aria-controls="map-body">${ic('map')}内容结构<small>${doc.blocks.length} 章 · ${m.count} 个节点</small>${ic('chev-r', 'sm').replace('class="ic', 'class="ic chev')}</button>${open ? `<button class="btn btn-ghost btn-xs" data-act="map-expand">全部展开</button>` : ''}<button class="btn btn-ghost btn-xs" data-act="map-zoom">${ic('maximize', 'sm')}查看导图</button></div>
      ${!open ? `<div class="map-overview">${doc.blocks.map((b, i) => `<button class="map-chapter" data-act="map-go" data-block="${b.id}"><span>${String(i + 1).padStart(2, '0')}</span>${esc(b.short || b.title)}</button>`).join('')}</div>` : ''}
      <div class="collapse${open ? ' open' : ''}" id="map-body"><div><div class="map-body">${m.svg}</div></div></div>
    </section>`
  }
  function chapterHTML(v, doc, b, i, rich, compact) {
    const rs = revisedSet(v)
    const figsFor = pid => rich ? b.figures.filter(f => f.after === pid).map(f => figureHTML(v, doc, f.id)).join('') : ''
    const orphan = rich ? b.figures.filter(f => !b.paras.some(p => p.id === f.after)).map(f => figureHTML(v, doc, f.id)).join('') : ''
    return `<section class="chapter" id="${compact ? 'ask-' : ''}blk-${b.id}" data-block-sec="${b.id}">
      <div class="ch-head"><span class="ch-num">${String(i + 1).padStart(2, '0')}</span><h2>${esc(b.title)}</h2>
        <div class="ch-actions">${compact ? `<button class="btn btn-ghost btn-xs" data-act="block-ask" data-block="${b.id}" title="把整段加入注释">${ic('plus', 'sm')}整段加入</button>` : `<button class="btn btn-ghost btn-xs btn-ic" data-act="block-ask" data-block="${b.id}" aria-label="围绕这一章提问" title="围绕这一章提问">${ic('message', 'sm')}</button>`}<button class="btn btn-ghost btn-xs btn-ic" data-act="block-edit" data-block="${b.id}" aria-label="让 Agent 修改这一章" title="让 Agent 修改这一章">${ic('wand', 'sm')}</button></div>
        <span class="ch-time">${timeBtn(b.start)}</span></div>
      <div class="prose" data-selectable data-block="${b.id}">${b.paras.map(p => `<div class="para${rs.has(p.id) ? ' revised' : ''}" data-para="${p.id}">${md(p.md)}</div>${figsFor(p.id)}`).join('')}${orphan}</div>
    </section>`
  }
  function figureHTML(v, doc, id) {
    const f = doc.figures[id], st = v.figs[id] || 'ok', no = C.figOrder(doc).indexOf(id) + 1
    if (st === 'skipped') return `<div data-fig-slot="${id}" hidden></div>`
    if (st === 'pending' || st === 'loading') return `<figure class="figure" data-fig-slot="${id}"><div class="slot pending"><div class="in">${st === 'loading' ? `<span class="spinner"></span><b>正在挑选画面</b><span>Agent 正在查看 ${fmt(f.t - 12)}–${fmt(f.t + 12)} 的候选画面</span>` : `${ic('photo', 'lg')}<span>等待补充画面 · 图 ${no}</span>`}</div></div></figure>`
    if (st === 'failed') return `<figure class="figure" data-fig-slot="${id}"><div class="slot failed"><div class="in">${ic('alert', 'lg')}<b>图 ${no} 没有补充成功</b><span>画面读取超时（演示）。正文不受影响，回放仍可使用。</span><div class="acts"><button class="btn btn-sm" data-act="retry-fig" data-fig="${id}">${ic('refresh', 'sm')}重试</button><button class="btn btn-sm btn-ghost" data-act="skip-fig" data-fig="${id}">跳过这张</button>${timeBtn(f.t)}</div></div></div></figure>`
    const rev = UIS.revealed.has(v.id + id) ? ' reveal' : ''
    UIS.revealed.delete(v.id + id)
    return `<figure class="figure${rev}" data-fig-slot="${id}"><div class="frame" data-act="zoom" data-fig="${id}" role="button" tabindex="0" aria-label="放大图 ${no}：${esc(f.alt)}"><img src="${f.src}" alt="${esc(f.alt)}" loading="lazy"><span class="zoom">${ic('maximize', 'sm')}</span></div><figcaption class="figcap"><span class="fig-no">图 ${no}</span><span class="txt">${inline(f.caption)}</span><span class="fig-tools">${timeBtn(f.t)}<button class="btn btn-ghost btn-xs btn-ic" data-act="fig-ask" data-fig="${id}" aria-label="引用图 ${no} 提问" title="引用这张图提问">${ic('message', 'sm')}</button></span></figcaption></figure>`
  }
  function framesHTML(v, doc) {
    const items = doc.blocks.flatMap((b, i) => b.figures.map(f => ({ b, i, id: f.id }))).filter(x => v.figs[x.id] !== 'skipped')
    if (!items.length) return `<div class="notice">${ic('info')}<div class="grow">这份摘要没有关键画面，可以切换到文字视图阅读。</div></div>`
    return `<div class="kf-grid">${items.map((x, k) => `<div class="kf-item${k === 0 ? ' wide' : ''}"><div class="kf-ch"><span>第 ${x.i + 1} 章</span>·<button data-act="goto-block" data-block="${x.b.id}">${esc(x.b.title)} ${ic('arrow-r', 'sm').replace('class="ic', 'style="display:inline;vertical-align:-2px" class="ic')}</button></div>${figureHTML(v, doc, x.id)}</div>`).join('')}</div>`
  }
  function sideHTML(v) {
    return `<div id="side-player">${C.playerHTML(v)}</div>
      <div class="player-foot"><span class="now" data-now="${v.id}"></span><label class="follow" title="播放时自动滚动到对应章节"><input type="checkbox" data-follow ${S().ui.follow ? 'checked' : ''}>跟随播放</label></div>
      <nav class="toc" id="toc-slot" aria-label="章节目录">${tocHTML(v)}</nav>`
  }
  function tocHTML(v) {
    const doc = C.curDoc(v)
    if (!doc) { const org = v.pipe?.organized || 0; return `<div class="toc-title"><span>章节</span><span>整理中</span></div>${C.template(v).blocks.slice(0, org).map(b => `<button class="item" disabled><span class="t">${fmt(b.start)}</span><span class="n">${esc(b.title)}</span></button>`).join('')}${org < C.template(v).blocks.length && v.status !== 'failed' ? '<div style="padding:8px 10px"><div class="skel-line" style="width:70%"></div></div>' : ''}` }
    const nf = Object.values(v.figs).filter(x => x === 'ok').length
    return `<div class="toc-title"><span>章节</span><span>${doc.blocks.length} 章${nf ? ' · ' + nf + ' 张画面' : ''}</span></div>${doc.blocks.map(b => { const n = b.figures.filter(f => v.figs[f.id] === 'ok').length; return `<button class="item" data-act="toc" data-block="${b.id}"><span class="t">${fmt(b.start)}</span><span class="n">${esc(b.title)}</span>${n ? `<span class="figs">${ic('photo', 'sm')}${n}</span>` : ''}</button>` }).join('')}`
  }
  function mPlayerHTML(v) {
    return `<div class="bar"><button class="btn btn-sm btn-ic" data-act="p-toggle" data-mbar-toggle="${v.id}" aria-label="播放">${ic('play')}</button><span class="now" data-now="${v.id}"></span><button class="btn btn-ghost btn-sm" data-act="m-player" aria-expanded="${UIS.mPlayer}">${UIS.mPlayer ? '收起' : '展开播放器'}${ic(UIS.mPlayer ? 'chev-d' : 'video', 'sm')}</button></div>${UIS.mPlayer ? C.playerHTML(v) : ''}`
  }
  function transcriptHTML(v) {
    const doc = C.curDoc(v) || C.template(v), cues = C.cuesOf(v)
    return `<div class="notice src-note" style="margin-bottom:18px">${ic('subtitles')}<div class="grow"><b>${v.kind === 'file' ? '语音识别文字' : '平台字幕（中文）'}</b> · ${cues.length} 条 · 点击任一句回放。<span class="muted">${v.kind === 'file' ? '本地文件没有字幕，使用了语音识别。' : '平台字幕只说明来源，不代表经过人工校对。'}</span></div></div>
      <div class="transcript">${doc.blocks.map((b, i) => `<div class="cue-ch"><span class="ch-num" style="margin:0">${String(i + 1).padStart(2, '0')}</span>${esc(b.title)}</div>${cues.filter(c => c.t >= b.start && c.t < b.end).map(c => `<button class="cue-row" data-act="seek" data-t="${c.t}" data-cue="${c.t}"><span class="t">${fmt(c.t)}</span><span>${esc(c.text)}</span></button>`).join('')}`).join('')}</div>`
  }

  /* partial updates */
  function renderDoc(v) {
    const slot = $('#doc-slot'); if (!slot) return
    slot.innerHTML = docHTML(v)
    const ms = $('#m-player'); if (ms) { ms.innerHTML = mPlayerHTML(v); C.bindScrub(ms) }
    C.pSync(); spy()
  }
  function renderSide(v) {
    const t = $('#toc-slot'); if (t) t.innerHTML = tocHTML(v)
    const sp = $('#side-player'); if (sp) { sp.innerHTML = C.playerHTML(v); C.bindScrub(sp) }
    C.pSync(); spy()
  }
  function renderTags(v) { const t = $('#tags-slot'); if (t) t.innerHTML = tagsHTML(v); setTimeout(() => v.tagLinks.forEach(l => delete l.fresh), 50) }

  /* =========================================================
     Ask
     ========================================================= */
  function askOpen() { const s = S().ui.askPanel; return s == null ? !C.isMobile() : s }
  function askHTML(v) {
    const doc = C.curDoc(v)
    if (!doc) return `<div class="page"><div class="empty"><div class="art">${ic('message')}</div><h3>摘要整理好后就能提问</h3><p>当前${stageText(v) || '正在处理'}。文字摘要可读后，这里会自动可用。</p><div class="acts"><a class="btn" href="#/v/${v.id}">查看处理进度</a></div></div></div>`
    const ver = C.curVer(v)
    return `<div class="ask${askOpen() ? '' : ' panel-closed'}" id="ask">
      <div class="ask-sheet-veil" data-act="ask-panel"></div>
      <aside class="ask-panel" aria-label="摘要面板">
        <div class="ask-panel-head">${ic('book', 'sm')}<b>摘要</b><span class="ver">v${ver.n} · ${esc(ver.label)}</span><button class="btn btn-ghost btn-sm btn-ic" data-act="ask-panel" aria-label="收起摘要">${ic('x')}</button></div>
        <div class="ask-panel-hint">${ic('quote', 'sm')}选中一段文字，或点「整段加入」，作为注释附在问题上方</div>
        <div class="ask-panel-body" id="ask-panel-body">${askPanelBody(v, doc)}</div>
      </aside>
      <section class="ask-main">
        <div class="ask-top"><button class="btn btn-ghost btn-sm" data-act="ask-panel" aria-expanded="${askOpen()}">${ic('panel', 'sm')}${askOpen() ? '收起摘要' : '展开摘要'}</button><span class="title">围绕「${esc(short(v.title, 30))}」提问</span>${v.chat.msgs.length ? `<button class="btn btn-ghost btn-xs" data-act="ask-new">${ic('plus', 'sm')}新对话</button>` : ''}</div>
        <div class="ask-thread" id="thread"><div class="thread-inner" id="thread-inner">${threadHTML(v)}</div></div>
        <div class="composer-wrap" id="composer-wrap">${composerHTML(v)}</div>
        <div id="float-slot">${UIS.floatOpen ? floatHTML(v) : ''}</div>
      </section>
    </div>`
  }
  function askPanelBody(v, doc) {
    const rs = revisedSet(v)
    return `<section class="chapter" id="ask-blk-overview" data-block-sec="overview"><div class="ch-head"><span class="ch-num">00</span><h2>概览</h2><div class="ch-actions"><button class="btn btn-ghost btn-xs" data-act="block-ask" data-block="overview">${ic('plus', 'sm')}整段加入</button></div></div><div class="prose" data-selectable data-block="overview"><div class="para${rs.has('overview') ? ' revised' : ''}" data-para="overview">${md(doc.overview)}</div></div></section>` + doc.blocks.map((b, i) => chapterHTML(v, doc, b, i, false, true)).join('')
  }
  function threadHTML(v) {
    const msgs = v.chat.msgs
    if (!msgs.length) return `<div class="ask-intro"><div class="art">${ic('message', 'lg')}</div><h2>围绕这份摘要提问</h2><p>回答会标出视频中的时间点，点击就能回放核对。</p>
      <div class="how"><span>${ic('quote', 'sm')}选中摘要文字，加入注释</span><span>${ic('message', 'sm')}问题单独输入</span><span>${ic('wand', 'sm')}要改摘要，切到「让 Agent 修改」</span></div>
      ${v.template === 'pg' ? `<div class="sugg-list">${D.suggestions.map(s => `<button class="sugg-btn" data-act="sugg" data-q="${esc(s)}">${esc(s)}</button>`).join('')}</div>` : ''}</div>`
    const lastAi = msgs.map(m => m.role).lastIndexOf('ai')
    return msgs.map((m, i) => msgHTML(v, m, i === lastAi)).join('')
  }
  function snapHTML(v, a) {
    const cur = C.curVer(v), stale = cur && cur.id !== a.ver
    return `<button class="snap" data-act="snap-toggle"><span class="h">${ic(a.kind === 'figure' ? 'photo' : 'quote', 'sm')}摘要引用 · ${esc(a.blockTitle)} · <span class="ver">v${a.verN}</span>${stale ? `<span class="stale">· 摘要已更新到 v${cur.n}，这里保留提问时的内容</span>` : ''}</span>${a.figSrc ? `<img class="snap-figure" src="${esc(a.figSrc)}" alt="提问时引用的截图">` : ''}<span class="qt">${esc(a.quote)}</span></button>`
  }
  function msgHTML(v, m, isLast) {
    if (m.role === 'user') return `<div class="msg user" data-msg="${m.id}"><div class="bubble">${m.annos.length ? `<div class="msg-annos">${m.annos.map(a => snapHTML(v, a)).join('')}</div>` : ''}<div class="q">${esc(m.text)}</div></div></div>`
    if (m.role === 'event') return `<div class="msg event" data-msg="${m.id}"><div class="event-line">${ic(m.icon || 'wand', 'sm')}<span>${m.text}</span>${m.undo && C.curVer(v)?.id === m.ver ? `<button class="btn btn-ghost btn-xs" data-act="undo">${ic('undo', 'sm')}撤销</button>` : ''}</div></div>`
    const body = m.status === 'thinking' ? ''
      : `<div class="answer" data-answer="${m.id}">${md(m.status === 'streaming' ? safeSlice(m.text, UIS.streaming[m.id] || 0) : m.text)}${m.status === 'streaming' ? '<span class="caret"></span>' : ''}</div>`
    const cites = (m.text.match(/\{\{t:/g) || []).length
    const meta = m.status === 'done' ? `<div class="ans-meta"><span class="src-chip">${ic('book', 'sm')}依据：摘要 v${m.verN}${m.annoCount ? ' · ' + m.annoCount + ' 条注释' : ''}${cites ? ' · 字幕 ' + cites + ' 处' : ''}</span><span>· 预置回答（演示）</span><span>· 不会修改摘要</span></div>${isLast && m.sugg?.length ? `<div class="ans-follow">${m.sugg.map(s => `<button data-act="sugg" data-q="${esc(s)}">${esc(s)}</button>`).join('')}</div>` : ''}` : ''
    const activity = m.activity ? AV.render(m.activity, { compact: true }) : ''
    const stop = ['thinking', 'streaming'].includes(m.status) ? `<button class="activity-stop" data-act="stop-answer" data-msg="${m.id}">停止回答</button>` : ''
    return `<div class="msg ai" data-msg="${m.id}"><div class="avatar-ai">${ic('sparkles', 'sm')}</div><div class="bubble">${activity}${stop}${body}${meta}</div></div>`
  }
  function safeSlice(t, n) {
    let s = t.slice(0, n)
    const open = s.lastIndexOf('{{'); if (open > s.lastIndexOf('}}')) s = s.slice(0, open)
    if ((s.match(/`/g) || []).length % 2) s = s.slice(0, s.lastIndexOf('`'))
    if ((s.match(/\*\*/g) || []).length % 2) s = s.slice(0, s.lastIndexOf('**'))
    return s
  }
  function composerHTML(v) {
    const c = v.chat, edit = c.mode === 'edit', cur = C.curVer(v)
    const cards = c.annos.map(a => {
      const u = UIS.annoUI[a.id] || {}, stale = cur && a.ver !== cur.id
      return `<div class="anno${stale ? ' stale' : ''}${u.open ? ' open' : ''}" data-anno="${a.id}">
        <div class="anno-top">${a.figSrc ? `<img class="anno-thumb" src="${esc(a.figSrc)}" alt="引用截图">` : ''}<span class="src">${ic(a.kind === 'figure' ? 'photo' : 'quote', 'sm')}${esc(a.blockTitle)}<span class="kind">· ${a.kind === 'figure' ? '截图 ' + fmt(a.figTime) : a.kind === 'block' ? '整章' : '选段'}</span></span>
          <button class="anno-ver" data-act="anno-ver" data-id="${a.id}" aria-expanded="${!!u.ver}" title="查看版本">v${a.verN}</button>
          <div class="anno-acts"><button data-act="anno-open" data-id="${a.id}" aria-label="${u.open ? '收起' : '展开'}注释" aria-expanded="${!!u.open}">${ic(u.open ? 'chev-d' : 'chev-r', 'sm')}</button><button data-act="anno-goto" data-id="${a.id}" aria-label="返回原段" title="返回原段">${ic('target', 'sm')}</button><button data-act="anno-remove" data-id="${a.id}" aria-label="移除注释" title="移除">${ic('x', 'sm')}</button></div></div>
        <div class="qt">${esc(a.quote)}</div>
        ${u.ver ? `<div class="ver-info">来自摘要 v${a.verN}（${esc(a.verLabel)}），选于 ${clock(a.at)}。${stale ? `当前摘要已更新到 v${cur.n}；发送时会使用你选中时的原文。也可以移除后重新选择。` : '与当前版本一致。'}</div>` : ''}
      </div>`
    }).join('')
    const total = c.annos.reduce((s, a) => s + a.quote.length, 0)
    return `<div class="composer${edit ? ' edit-mode' : ''}">
      ${c.annos.length ? `<div class="annos"><div class="annos-head">${ic('quote', 'sm')}引用 ${c.annos.length}/3<span class="grow">· 随问题发送</span><button class="btn btn-ghost btn-xs" data-act="anno-clear">清空</button></div>${cards}</div>` : ''}
      <textarea id="ask-input" rows="1" aria-label="${edit ? '修改要求' : '问题'}" placeholder="${edit ? '说明你希望 Agent 怎么改，例如：把这段改成更容易照着操作的步骤' : c.annos.length ? '针对上面的注释输入你的问题…' : '输入你的问题，例如：这个报错怎么彻底解决？'}">${esc(edit ? (c.editDraft || '') : c.draft)}</textarea>
      <div class="composer-foot">
        <div class="seg seg-sm mode-seg" role="radiogroup" aria-label="输入用途"><button role="radio" aria-checked="${!edit}" class="${!edit ? 'on' : ''}" data-act="mode" data-mode="ask">${ic('message', 'sm')}提问</button><button role="radio" aria-checked="${edit}" class="${edit ? 'on edit' : ''}" data-act="mode" data-mode="edit">${ic('wand', 'sm')}让 Agent 修改</button></div>
        <span class="grow">${edit ? '先预览，再应用' : '<kbd>Enter</kbd> 发送'}</span>
        ${edit ? `<button class="send edit" data-act="send">${ic('wand', 'sm')}生成修改预览</button>` : `<button class="send" data-act="send" aria-label="发送问题">${ic('send')}</button>`}
      </div>
    </div>
    <p class="composer-note">原型演示：回答为预置内容；普通提问不会修改摘要</p>`
  }
  function floatHTML(v) { return `<div class="float-player"><button class="close" data-act="float-close" aria-label="关闭播放器">${ic('x', 'sm')}</button>${C.playerHTML(v)}</div>` }
  function renderComposer(v, focus) {
    const w = $('#composer-wrap'); if (!w) return
    w.innerHTML = composerHTML(v); autosize()
    if (focus) { const t = $('#ask-input'); t.focus(); t.setSelectionRange(t.value.length, t.value.length) }
  }
  function renderThread(v, force = false) { const t = $('#thread-inner'); if (t) { captureThread(v); if (force) workspace(v).threadBottom = true; t.innerHTML = threadHTML(v); restoreThread(v) } }
  function scrollThread(force = false) { const v = curV(), t = $('#thread'); if (t && v && (force || workspace(v).threadBottom)) t.scrollTop = t.scrollHeight }
  function autosize() { const t = $('#ask-input'); if (!t) return; t.style.height = 'auto'; t.style.height = Math.min(200, Math.max(56, t.scrollHeight)) + 'px' }
  function addAnno(v, a) {
    const doc = C.curDoc(v), ver = C.curVer(v), c = v.chat
    const b = a.blockId === 'overview' ? { title: '概览' } : doc.blocks.find(x => x.id === a.blockId)
    if (c.annos.some(x => x.blockId === a.blockId && x.quote === a.quote)) { toast('这段已经在注释里了', { kind: 'info' }); return false }
    if (c.annos.length >= 3) { toast('最多附加 3 条摘要注释，可以先移除一条', { kind: 'warn' }); return false }
    if (c.annos.reduce((s, x) => s + x.quote.length, 0) + a.quote.length > 3000) { toast('注释总字数超过 3000，请选短一些', { kind: 'warn' }); return false }
    c.annos.push({ id: C.uid('a'), blockId: a.blockId, paraId: a.paraId, blockTitle: b.title, quote: a.quote, kind: a.kind, figId: a.figId, figSrc: a.figSrc, figTime: a.figTime, ver: ver.id, verN: ver.n, verLabel: ver.label, at: Date.now() })
    C.save(); return true
  }
  function blockText(doc, blockId) { if (blockId === 'overview') return plain(doc.overview); const b = doc.blocks.find(x => x.id === blockId); return b.paras.map(p => plain(p.md)).join('\n') }
  function afterAnno(v) {
    const hadDraft = !!v.chat.draft.trim()
    if (workspace(v).dock === 'ask' && workspace(v).focus !== 'read') renderComposer(v, true)
    else changeDock(v, 'ask', true)
    toast(hadDraft ? '已加入引用，问题草稿已保留' : '已加入引用，输入问题后发送')
  }
  function togglePanel(open) {
    S().ui.askPanel = open; C.save()
    const a = $('#ask'); if (!a) return
    a.classList.toggle('panel-closed', !open)
    const b = $('.ask-top [data-act="ask-panel"]'); if (b) { b.innerHTML = ic('panel', 'sm') + (open ? '收起摘要' : '展开摘要'); b.setAttribute('aria-expanded', open) }
  }
  async function sendQuestion(v, q) {
    const c = v.chat
    if (UIS.busy) return
    const ver = C.curVer(v), annos = c.annos.map(a => ({ ...a }))
    c.msgs.push({ id: C.uid('m'), role: 'user', text: q, annos, at: Date.now() })
    const ai = { id: C.uid('m'), role: 'ai', status: 'thinking', text: '', annos: annos.length > 0, annoCount: annos.length, verN: ver.n, at: Date.now(), activity: AV.create('question') }
    c.msgs.push(ai); c.annos = []; c.draft = ''
    UIS.busy = true; UIS.questionId = ai.id; UIS.questionOwner = { v, ai }
    const alive = () => UIS.questionId === ai.id && C.getV(v.id) === v && C.curVer(v)?.id === ver.id && c.msgs.includes(ai) && ai.status !== 'cancelled'
    const aborted = () => { cancelQuestion(v, ai); return false }
    const refresh = () => {
      C.save()
      if (R.name !== 'detail' || R.id !== v.id) return
      const el = $(`[data-msg="${ai.id}"]`)
      if (el) { captureThread(v); el.outerHTML = msgHTML(v, ai, true); restoreThread(v) }
    }
    const perform = async (title, detail, time) => {
      if (!alive()) return aborted()
      const id = AV.start(ai.activity, title, { detail, time })
      refresh()
      await new Promise(resolve => setTimeout(resolve, 850))
      if (!alive()) return aborted()
      AV.update(ai.activity, id, 'done'); refresh()
      return true
    }
    C.save(); renderThread(v, true); renderComposer(v, false)
    if (R.name === 'detail' && R.id === v.id) { const tabs = $('.tabs-bar'); if (tabs) tabs.outerHTML = tabsHTML(v) }
    if (!await perform(annos.length ? '读取你选中的摘要引用' : '读取当前摘要与问题', `依据摘要 v${ver.n}${annos.length ? ' · ' + annos.length + ' 条引用' : ''}`)) return
    const config = /配置|参数|并发|连接数|图|画面|截图/.test(q), bench = /压测|池大小|TPS|性能/.test(q)
    const topic = config ? '连接池参数' : bench ? '压测结果' : /报错|预处理|prepared/.test(q) ? '报错原因' : '问题'
    if (!await perform('查找' + topic + '相关字幕', '使用本地预置的时间片段（演示）')) return
    const citedFigure = annos.find(a => a.figId), figId = citedFigure?.figId || (config ? 'f2' : bench ? 'f4' : null)
    const fig = figId && C.curDoc(v)?.figures[figId]
    if (fig && v.figs[figId] === 'ok') {
      if (!await perform('查看 ' + fmt(fig.t) + ' 的' + (bench ? '压测图表' : '配置画面'), '核对这张预置截图与相关文字', fig.t)) return
    }
    if (!alive()) { aborted(); return }
    const res = C.answer(v, q, annos)
    ai.text = res.text; ai.sugg = res.sugg; ai.status = 'streaming'; if (res.topic) c.topic = res.topic
    const answerStep = AV.start(ai.activity, '整理' + topic + '的回答', { detail: '回答正在输出，执行记录与正文同时显示' })
    UIS.streaming[ai.id] = 0; refresh()
    const iv = setInterval(() => {
      if (!alive()) { clearInterval(iv); aborted(); return }
      UIS.streaming[ai.id] += 4
      const box = $(`[data-answer="${ai.id}"]`)
      if (UIS.streaming[ai.id] >= ai.text.length) {
        clearInterval(iv); AV.update(ai.activity, answerStep, 'done'); AV.finish(ai.activity)
        ai.status = 'done'; UIS.busy = false; UIS.questionId = null; UIS.questionOwner = null; delete UIS.streaming[ai.id]
        refresh(); scrollThread(); return
      }
      if (box) { box.innerHTML = md(safeSlice(ai.text, UIS.streaming[ai.id])) + '<span class="caret"></span>'; scrollThread() }
    }, 38)
  }

  function cancelQuestion(v = UIS.questionOwner?.v, m = UIS.questionOwner?.ai) {
    if (!v || !m || !['thinking', 'streaming'].includes(m.status)) return
    m.text = m.status === 'streaming' ? safeSlice(m.text, UIS.streaming[m.id] || 0) : ''
    m.status = 'cancelled'; AV.finish(m.activity, 'cancelled'); delete UIS.streaming[m.id]
    if (UIS.questionId === m.id) { UIS.questionId = null; UIS.questionOwner = null; UIS.busy = false }
    C.save()
  }

  /* =========================================================
     Selection toolbar
     ========================================================= */
  let selEl = null
  function hideSel() { selEl?.remove(); selEl = null }
  function checkSel() {
    const sel = getSelection()
    if (!sel || sel.isCollapsed || !sel.rangeCount) { hideSel(); return }
    const range = sel.getRangeAt(0)
    const elOf = n => n.nodeType === 1 ? n : n.parentElement
    const r1 = elOf(range.startContainer)?.closest('[data-selectable]'), r2 = elOf(range.endContainer)?.closest('[data-selectable]')
    if (!r1 || !r2 || R.name !== 'detail') { hideSel(); return }
    const text = sel.toString().replace(/[ \t]+\n/g, '\n').replace(/\n{2,}/g, '\n').trim()
    if (text.length < 2) { hideSel(); return }
    const inAsk = !!r1.closest('.ask-panel')
    const p1 = elOf(range.startContainer).closest('[data-para]'), p2 = elOf(range.endContainer).closest('[data-para]')
    if (r1 !== r2) { SEL = null; showSel(range, `<span class="hint">${ic('info', 'sm')}&nbsp;一次只能选择同一章节内的文字</span>`); return }
    SEL = { vid: R.id, blockId: r1.dataset.block, paraId: (p1 || p2)?.dataset.para, multi: p1 !== p2, quote: text.slice(0, 1500), inAsk }
    showSel(range, `<button data-act="sel-ask">${ic(inAsk ? 'plus' : 'message', 'sm')}${inAsk ? '加入注释' : '提问'}</button><button class="edit" data-act="sel-edit">${ic('wand', 'sm')}让 Agent 修改</button><span class="div"></span><button data-act="sel-copy" aria-label="复制">${ic('copy', 'sm')}</button>`)
  }
  function showSel(range, html) {
    hideSel()
    selEl = document.createElement('div'); selEl.className = 'sel-bar'; selEl.setAttribute('role', 'toolbar'); selEl.setAttribute('aria-label', '选中文字操作')
    selEl.innerHTML = html
    selEl.addEventListener('mousedown', e => e.preventDefault())
    document.body.appendChild(selEl)
    const r = range.getBoundingClientRect(), w = selEl.offsetWidth, h = selEl.offsetHeight
    const coarse = matchMedia('(pointer: coarse)').matches
    let top = coarse ? r.bottom + 12 : r.top - h - 10
    if (top < 64) top = r.bottom + 10
    if (top + h > innerHeight - 8) top = innerHeight - h - 8
    selEl.style.top = top + 'px'
    selEl.style.left = Math.max(10, Math.min(r.left + r.width / 2 - w / 2, innerWidth - w - 10)) + 'px'
  }
  document.addEventListener('mouseup', e => { if (selEl && selEl.contains(e.target)) return; setTimeout(checkSel, 10) })
  document.addEventListener('keyup', e => { if (e.shiftKey || e.key === 'Shift') checkSel() })
  let selT; document.addEventListener('selectionchange', () => { if (!matchMedia('(pointer: coarse)').matches) return; clearTimeout(selT); selT = setTimeout(checkSel, 350) })

  function markQuote(para, quote) {
    const walker = document.createTreeWalker(para, NodeFilter.SHOW_TEXT)
    const nodes = []; let full = ''
    while (walker.nextNode()) { nodes.push({ n: walker.currentNode, s: full.length }); full += walker.currentNode.data }
    const map = []; let comp = ''
    for (let i = 0; i < full.length; i++) if (!/\s/.test(full[i])) { map.push(i); comp += full[i] }
    const needle = quote.replace(/\s+/g, '')
    let at = comp.indexOf(needle), len = needle.length
    if (at < 0) { const head = needle.slice(0, 24); at = comp.indexOf(head); len = head.length }
    if (at < 0) return false
    const start = map[at], end = map[at + len - 1] + 1
    nodes.slice().reverse().forEach(({ n, s }) => {
      const a = Math.max(start, s), b = Math.min(end, s + n.data.length)
      if (a >= b) return
      const r = document.createRange(); r.setStart(n, a - s); r.setEnd(n, b - s)
      const m = document.createElement('mark'); m.className = 'quote-hl'
      try { r.surroundContents(m) } catch (e) { }
    })
    setTimeout(() => $$('mark.quote-hl', para).forEach(m => m.replaceWith(...m.childNodes)), 3200)
    return true
  }
  function flashPara(el) { if (!el) return; el.classList.remove('flash'); void el.offsetWidth; el.classList.add('flash'); setTimeout(() => el.classList.remove('flash'), 2200) }

  /* =========================================================
     Revision dialog
     ========================================================= */
  function openRevision(v, scope, req = '', opts = {}) {
    if (!readable(v)) { toast('摘要可读后才能修改', { kind: 'info' }); return }
    REV = { vid: v.id, scope: scope || { kind: 'doc' }, initialScope: scope || { kind: 'doc' }, req, step: 'input', plan: null, from: opts.from, err: '' }
    openDialog(revHTML(), { width: 800, onClose: () => { if (REV?.step === 'working') AV.finish(REV.activity, 'cancelled'); REV = null } })
    if (opts.auto && req.trim()) startRev()
  }
  function scopeLabel(v, sc) {
    const doc = C.curDoc(v)
    if (sc.kind === 'doc') return { t: '整篇摘要', q: '可能调整多个章节，所有改动都会列在预览中' }
    const b = sc.blockId === 'overview' ? { title: '概览' } : doc.blocks.find(x => x.id === sc.blockId)
    const i = doc.blocks.indexOf(b)
    if (sc.kind === 'block') return { t: (i >= 0 ? `第 ${i + 1} 章 · ` : '') + b.title + '（整章）', q: blockText(doc, sc.blockId) }
    return { t: '选中的段落 · ' + (i >= 0 ? `第 ${i + 1} 章 ` : '') + b.title, q: sc.quote }
  }
  function revHTML() {
    const v = C.getV(REV.vid), ver = C.curVer(v)
    const head = `<div class="dlg-head"><span class="dlg-icon">${ic('wand')}</span><div class="grow"><h3>让 Agent 修改摘要</h3><p>修改会先生成差异预览，你确认后才写入新版本，之后也可以撤销</p></div><button class="btn btn-ghost btn-sm btn-ic" data-act="dlg-close" aria-label="关闭">${ic('x')}</button></div>`
    if (REV.step === 'input') {
      const opts = [REV.initialScope.kind !== 'doc' ? REV.initialScope : null, { kind: 'doc' }].filter(Boolean)
      return head + `<div class="dlg-body">
        <div class="field-label">修改范围</div>
        <div class="rev-scope" role="radiogroup">${opts.map((sc, i) => { const l = scopeLabel(v, sc), on = sc.kind === REV.scope.kind; return `<label class="scope-opt${on ? ' on' : ''}"><input type="radio" name="rev-scope" data-rev-scope="${i}" ${on ? 'checked' : ''}><span><b>${esc(l.t)}</b><span class="q">${esc(l.q)}</span></span></label>` }).join('')}</div>
        ${REV.initialScope.kind === 'doc' ? `<p class="field-help" style="margin:-8px 0 16px;font-size:12.5px;color:var(--tx-4)">也可以在正文中选中一段文字，再选择「让 Agent 修改」，只改那一段。</p>` : ''}
        <label class="field-label" for="rev-req">怎么修改</label>
        <textarea class="textarea" id="rev-req" rows="3" style="min-height:88px" placeholder="例如：把这段改成更容易照着操作的步骤" autofocus>${esc(REV.req)}</textarea>
        ${REV.err ? `<div class="field-error">${ic('alert', 'sm')}${esc(REV.err)}</div>` : ''}
        <div class="rev-presets">${['把这段改成更容易照着操作的步骤', '更简洁，保留关键参数', '把要点整理成编号步骤'].map(p => `<button data-act="rev-preset" data-p="${esc(p)}">${esc(p)}</button>`).join('')}</div>
      </div>
      <div class="dlg-foot"><span class="grow">当前 v${ver.n}，应用后生成 v${Math.max(...v.versions.map(x => x.n)) + 1}。事实性内容会对照字幕，不会编造。</span><button class="btn btn-quiet" data-act="dlg-close">取消</button><button class="btn btn-primary" data-act="rev-go">${ic('sparkles', 'sm')}生成修改预览</button></div>`
    }
    if (REV.step === 'working') {
      return head + `<div class="dlg-body"><div class="rev-req">${ic('quote', 'sm')}<span>你的要求：<b>${esc(REV.req)}</b> · 范围：${esc(scopeLabel(v, REV.scope).t)}</span></div>${AV.render(REV.activity)}</div>
        <div class="dlg-foot"><span class="grow">Agent 正在修改，不会改动当前阅读的版本</span><button class="btn btn-quiet" data-act="dlg-close">取消</button></div>`
    }
    const plan = REV.plan
    return head + `<div class="dlg-body">
      ${AV.render(REV.activity, { compact: true })}
      <div class="rev-req">${ic('quote', 'sm')}<span>你的要求：<b>${esc(REV.req)}</b> · 范围：${esc(scopeLabel(v, REV.scope).t)}</span></div>
      ${plan.generic ? `<div class="notice warn" style="margin-bottom:14px">${ic('info')}<div class="grow">${plan.kind === 'other' ? '原型只识别「整理成步骤」和「更简洁」两类要求，下面是按规则生成的示例改写。' : '这部分没有预置 Agent 改写，下面是按规则生成的示例改写。完整效果可在第 5 章「三类常见报错排查」或选择「全文」范围查看。'}</div></div>` : ''}
      <div class="impact">${plan.impact.map(x => `<span>${ic(x.icon, 'sm')}${esc(x.t)}</span>`).join('')}</div>
      ${plan.changes.map(c => `<div class="diff-block"><div class="diff-title">${c.chNo ? `<span class="ch-num">${String(c.chNo).padStart(2, '0')}</span>` : ''}${esc(c.blockTitle)}</div><div class="diff-cols"><div class="diff-col before"><div class="lbl">${ic('x', 'sm')}修改前 · v${ver.n}</div><div class="prose">${md(c.before)}</div></div><div class="diff-col after"><div class="lbl">${ic('check', 'sm')}修改后</div><div class="prose">${md(c.after)}</div></div></div></div>`).join('')}
    </div>
    <div class="dlg-foot"><span class="grow">应用后正文、画面位置和导图会一起更新为 v${Math.max(...v.versions.map(x => x.n)) + 1}</span><button class="btn btn-quiet" data-act="rev-back">调整要求</button><button class="btn btn-quiet" data-act="dlg-close">放弃</button><button class="btn btn-primary" data-act="rev-apply">${ic('check', 'sm')}应用修改</button></div>`
  }
  function renderRev() { const el = dialogEl(); if (el && REV) { el.innerHTML = revHTML(); (el.querySelector('#rev-req') || el.querySelector('.btn-primary'))?.focus() } }
  async function startRev() {
    if (!REV || REV.step === 'working') return
    const t = dialogEl()?.querySelector('#rev-req'); if (t) REV.req = t.value
    if (!REV.req.trim()) { REV.err = '先写下你希望怎么修改'; renderRev(); return }
    const rev = REV, v = C.getV(rev.vid)
    rev.err = ''; rev.step = 'working'; rev.activity = AV.create('revision')
    const alive = () => REV === rev && rev.step === 'working' && C.getV(v.id) === v
    const perform = async (title, detail, fn) => {
      if (!alive()) return false
      const id = AV.start(rev.activity, title, { detail }); renderRev()
      await new Promise(resolve => setTimeout(resolve, 900))
      if (!alive()) return false
      if (fn) fn()
      AV.update(rev.activity, id, 'done'); renderRev(); return true
    }
    const compact = /简洁|精简|简短/.test(rev.req), steps = /步骤|编号|操作/.test(rev.req)
    if (!await perform(compact ? '确认需要保留的关键信息' : steps ? '确定步骤改写的目标' : '读取你的修改要求', rev.req)) return
    if (!await perform(rev.scope.kind === 'doc' ? '定位相关摘要段落' : '读取选中的章节与段落', scopeLabel(v, rev.scope).t)) return
    if (!await perform(compact ? '精简正文并保留关键参数' : steps ? '将相关段落组织成操作步骤' : '生成示例改写', '本地规则模拟，不调用模型', () => { rev.plan = C.planRevision(v, rev.scope, rev.req) })) return
    if (rev.plan.empty) {
      AV.finish(rev.activity, 'error'); rev.step = 'input'; rev.err = '没有找到可修改内容，换个要求或扩大范围试试'; renderRev(); return
    }
    if (!await perform('检查差异与引用位置', `${rev.plan.changes.length} 段正文 · 画面和导图按同一份内容同步`)) return
    AV.finish(rev.activity); rev.step = 'preview'; renderRev()
  }

  function applyRev() {
    const v = C.getV(REV.vid), plan = REV.plan, req = REV.req, from = REV.from
    const changedConcepts = plan.doc.blocks.flatMap(b => b.concepts.filter(c => c.changed).map(c => { delete c.changed; return c.id }))
    const ver = C.applyRevision(v, plan, req)
    ver.activity = REV.activity
    C.save()
    UIS.mapChanged[v.id] = [...changedConcepts, ...plan.changes.map(c => c.blockId)]
    setTimeout(() => { UIS.mapChanged[v.id] = [] }, 4000)
    closeDialog()
    if (from === 'ask') { v.chat.editDraft = ''; v.chat.mode = 'ask'; v.chat.annos = []; v.chat.msgs.push({ id: C.uid('m'), role: 'event', icon: 'wand', text: `已按你的要求修改「${esc(plan.changes[0].blockTitle)}」${plan.changes.length > 1 ? '等 ' + plan.changes.length + ' 段' : ''}，摘要更新为 v${ver.n}`, ver: ver.id, undo: true, at: Date.now() }); C.save() }
    refreshAfterVersion(v)
    const first = plan.changes[0]
    setTimeout(() => {
      if (workspace(v).focus === 'ask') changeFocus(v, 'restore')
      if (R.tab === 'transcript') go('#/v/' + v.id)
      const scope = $('#reading')
      plan.changes.forEach(c => flashPara(scope?.querySelector(`.para[data-para="${c.pid}"]`)))
      scope?.querySelector(`.para[data-para="${first.pid}"]`)?.scrollIntoView({ behavior: 'smooth', block: 'center' })
    }, 80)
    toast(`已应用修改，摘要更新为 v${ver.n}`, { action: { label: '撤销', fn: () => doUndo(v) } })
  }
  function doUndo(v) {
    const base = C.undoRevision(v)
    if (!base) { toast('没有可以撤销的修改', { kind: 'info' }); return }
    refreshAfterVersion(v)
    toast(`已撤销，恢复到 v${base.n}`, { kind: 'info' })
  }
  function refreshAfterVersion(v) {
    if (R.name !== 'detail' || R.id !== v.id) return
    renderDoc(v); renderSide(v); $('#regen-slot') && ($('#regen-slot').innerHTML = regenHTML(v))
    if (workspace(v).dock === 'ask') { renderThread(v); renderComposer(v); const ctx = $('.dock-context span'); if (ctx) ctx.innerHTML = ic('book', 'sm') + '依据摘要 v' + C.curVer(v).n }
  }

  /* =========================================================
     Lightbox
     ========================================================= */
  function openLightbox(v, id) {
    const doc = C.curDoc(v)
    const list = C.figOrder(doc).filter(x => v.figs[x] === 'ok')
    LB = { vid: v.id, list, i: Math.max(0, list.indexOf(id)), zoom: false, ret: document.activeElement }
    const el = document.createElement('div'); el.className = 'lightbox'; el.id = 'lightbox'; el.setAttribute('role', 'dialog'); el.setAttribute('aria-modal', 'true'); el.setAttribute('aria-label', '查看画面')
    document.body.appendChild(el); renderLB()
  }
  function renderLB() {
    const el = $('#lightbox'); if (!el || !LB) return
    const v = C.getV(LB.vid), doc = C.curDoc(v), id = LB.list[LB.i], f = doc.figures[id]
    const b = doc.blocks.find(x => x.figures.some(y => y.id === id)), bi = doc.blocks.indexOf(b)
    const no = C.figOrder(doc).indexOf(id) + 1
    el.innerHTML = `<div class="lb-top"><span class="grow">图 ${no} · 第 ${bi + 1} 章 ${esc(b.title)} · ${LB.i + 1} / ${LB.list.length}</span><button class="btn btn-sm" data-act="lb-zoom">${ic(LB.zoom ? 'maximize' : 'zoom', 'sm')}${LB.zoom ? '适应窗口' : '原尺寸'}</button><button class="btn btn-sm btn-ic" data-act="lb-close" aria-label="关闭">${ic('x')}</button></div>
      <div class="lb-stage${LB.zoom ? ' zoomed' : ''}"><img src="${f.src}" alt="${esc(f.alt)}" data-act="lb-zoom">${LB.list.length > 1 ? `<button class="lb-nav prev" data-act="lb-prev" aria-label="上一张">${ic('chev-l', 'lg')}</button><button class="lb-nav next" data-act="lb-next" aria-label="下一张">${ic('chev-r', 'lg')}</button>` : ''}</div>
      <div class="lb-cap"><span class="txt">${inline(f.caption)}</span><button class="time-btn" data-act="lb-seek" data-t="${f.t}">${ic('play')}回放此处 ${fmt(f.t)}</button></div>`
    el.querySelector('[data-act="lb-close"]').focus()
  }
  function closeLB() { $('#lightbox')?.remove(); const r = LB?.ret; LB = null; r?.focus?.({ preventScroll: true }) }

  /* =========================================================
     Tag editor & manager
     ========================================================= */
  function tagEditorHTML(v, q = '') {
    const nq = C.norm(q)
    const linked = new Set(v.tagLinks.map(l => l.id))
    const all = Object.values(S().tags).filter(t => !linked.has(t.id))
    const matches = (nq ? all.filter(t => C.norm(t.name).includes(nq) || t.aliases.some(a => C.norm(a).includes(nq))) : all.sort((a, b) => C.tagCount(b.id) - C.tagCount(a.id))).slice(0, 6)
    const exact = nq && C.findTag(q)
    return `<div class="pop-title">${ic('tag', 'sm')}标签<span style="margin-left:auto;font-weight:500;letter-spacing:0">手动调整会一直保留</span></div>
      <input class="input" data-te-input value="${esc(q)}" placeholder="添加标签，例如：数据库" aria-label="添加标签" maxlength="80">
      <div class="sugg">${matches.map((t, i) => `<button class="${i === 0 && nq ? 'hl' : ''}" data-act="te-add" data-name="${esc(t.name)}">${ic('plus', 'sm')}${esc(t.name)}${nq && !C.norm(t.name).includes(nq) ? `<small>别名：${esc(t.aliases.find(a => C.norm(a).includes(nq)))}</small>` : `<small>${C.tagCount(t.id)} 个视频</small>`}</button>`).join('')}${nq && !exact ? `<button data-act="te-add" data-name="${esc(q.trim())}" class="${matches.length ? '' : 'hl'}">${ic('plus', 'sm')}新建「${esc(q.trim())}」<small>手动标签</small></button>` : ''}</div>
      <div class="sec">当前标签 · ${v.tagLinks.length}</div>
      <div class="current">${v.tagLinks.map(l => `<span class="tag${l.origin === 'manual' ? ' manual' : ''}">${esc(S().tags[l.id]?.name || '')}<button class="x" data-act="te-remove" data-id="${l.id}" aria-label="移除 ${esc(S().tags[l.id]?.name || '')}">${ic('x', 'sm')}</button></span>`).join('') || '<span class="muted" style="font-size:12.5px">还没有标签</span>'}</div>
      ${v.suggestion && !C.findTag(v.suggestion) ? `<div class="sec">${ic('bulb', 'sm')}Agent 建议（未自动添加）</div><div class="current"><button class="tag" data-act="te-add" data-name="${esc(v.suggestion)}" data-sugg="1">${ic('plus', 'sm')}${esc(v.suggestion)}</button></div>` : ''}
      ${v.rejected.length ? `<div class="sec">已移除的自动标签 · Agent 不会再添加</div><div class="rejected">${v.rejected.filter(id => S().tags[id]).map(id => `<span class="tag">${esc(S().tags[id].name)}<button class="x" data-act="te-restore" data-id="${id}" aria-label="恢复" title="恢复">${ic('undo', 'sm')}</button></span>`).join('')}</div>` : ''}
      <div class="legend"><span class="legend-dot m">你添加或保留的</span><span class="legend-dot">Agent 自动添加的</span></div>
      <div class="foot"><button class="btn btn-sm" data-act="te-retag">${ic('refresh', 'sm')}让 Agent 重新归类</button><span style="flex:1"></span><button class="btn btn-sm btn-primary" data-act="te-done">完成</button></div>`
  }
  let teAnchor = null
  function openTagEditor(anchor) {
    const v = C.getV(R.id); teAnchor = anchor
    const el = openPop(anchor, tagEditorHTML(v), { cls: 'tag-editor', align: 'left' })
    if (el) bindTE(el, v)
  }
  function refreshTE(v, keepQ) {
    const el = $('.popover.tag-editor'); if (!el) return
    const q = keepQ ? el.querySelector('[data-te-input]').value : ''
    el.innerHTML = tagEditorHTML(v, q); bindTE(el, v)
    const inp = el.querySelector('[data-te-input]'); inp.focus(); inp.setSelectionRange(q.length, q.length)
    el._place?.()
  }
  function bindTE(el, v) {
    const inp = el.querySelector('[data-te-input]')
    inp.addEventListener('input', () => refreshTE(v, true))
    inp.addEventListener('keydown', e => { if (e.key === 'Enter' && !e.isComposing) { e.preventDefault(); const hl = el.querySelector('.sugg .hl') || el.querySelector('.sugg button'); if (inp.value.trim() && hl) hl.click() } })
  }
  function tagsChanged(v) { renderTags(v); C.save() }
  function openTagManager() {
    openDialog(tmHTML(), { drawer: true })
  }
  function tmHTML(editing) {
    const tags = Object.values(S().tags).sort((a, b) => C.tagCount(b.id) - C.tagCount(a.id))
    return `<div class="dlg-head"><span class="dlg-icon" style="background:var(--acc-dim);color:var(--acc)">${ic('tag')}</span><div class="grow"><h3>管理标签</h3><p>Agent 会优先复用这些标签。你改过的名称和归类不会被覆盖。</p></div><button class="btn btn-ghost btn-sm btn-ic" data-act="dlg-close" aria-label="关闭">${ic('x')}</button></div>
      <div class="dlg-body" style="flex:1">
        ${S().mergeSuggestions.filter(m => S().tags[m.from] && S().tags[m.to]).map(m => `<div class="tm-sugg"><div class="h">${ic('merge', 'sm')}建议合并：「${esc(S().tags[m.from].name)}」→「${esc(S().tags[m.to].name)}」</div><p>${esc(m.reason)} 共影响 ${C.tagCount(m.from)} 个视频。</p><div class="acts"><button class="btn btn-sm btn-primary" data-act="tm-merge" data-id="${m.id}">合并</button><button class="btn btn-sm btn-quiet" data-act="tm-dismiss" data-id="${m.id}">忽略</button></div></div>`).join('')}
        <div class="tm-list">${tags.map(t => editing === t.id ? `<div class="tm-row"><input class="input" data-tm-name="${t.id}" value="${esc(t.name)}" maxlength="80" aria-label="新名称"><button class="btn btn-sm btn-primary" data-act="tm-save" data-id="${t.id}">保存</button><button class="btn btn-sm btn-ghost" data-act="tm-cancel">取消</button></div>` : `<div class="tm-row"><div class="grow"><b>${esc(t.name)}</b><span class="origin${t.origin === 'manual' ? ' m' : ''}">${t.origin === 'manual' ? '手动' : 'Agent'}</span><small>${t.aliases.length ? '别名：' + esc(t.aliases.join('、')) : '无别名'}</small></div><span class="cnt">${C.tagCount(t.id)} 个</span><button class="btn btn-ghost btn-xs btn-ic" data-act="tm-edit" data-id="${t.id}" aria-label="重命名 ${esc(t.name)}">${ic('pencil', 'sm')}</button><button class="btn btn-ghost btn-xs btn-ic" data-act="tm-del" data-id="${t.id}" aria-label="删除 ${esc(t.name)}">${ic('trash', 'sm')}</button></div>`).join('')}</div>
        <p class="tm-note">${ic('info', 'sm')}<span>「数据库」和「PostgreSQL」层级不同，不会被自动合并。重命名后旧名称会保留为别名，旧的筛选链接仍然有效。</span></p>
      </div>`
  }
  function renderTM(editing) { const el = dialogEl(); if (el && el.classList.contains('drawer')) { el.innerHTML = tmHTML(editing); el.querySelector('[data-tm-name]')?.focus() } }

  /* =========================================================
     Demo console
     ========================================================= */
  function toggleConsole() {
    const ex = $('#console'); if (ex) { ex.remove(); return }
    const s = S()
    const el = document.createElement('div'); el.className = 'console'; el.id = 'console'; el.setAttribute('role', 'dialog'); el.setAttribute('aria-label', '原型演示控制台')
    const radio = (name, val, cur, label, sub) => `<label class="opt"><input type="radio" name="${name}" value="${val}" data-con="${name}" ${cur === val ? 'checked' : ''}><span>${label}${sub ? `<small>${sub}</small>` : ''}</span></label>`
    el.innerHTML = `<h4>${ic('flask', 'sm')}原型演示控制台<button class="btn btn-ghost btn-xs btn-ic" style="margin-left:auto" data-act="console" aria-label="关闭">${ic('x', 'sm')}</button></h4>
      <p>这是可交互的高保真原型。所有数据保存在本机浏览器，不会上传文件、访问 B 站或调用模型。</p>
      <div class="grp"><b>生成过程的演示结果</b>${radio('scenario', 'ok', s.scenario, '顺利完成')}${radio('scenario', 'partial', s.scenario, '一张画面补充失败', '默认 · 可以重试或跳过')}${radio('scenario', 'fail', s.scenario, '摘要生成失败', '可以重试，不会重新下载')}</div>
      <div class="grp"><b>处理速度</b>${radio('speed', 'normal', s.speed, '正常（约 15 秒）')}${radio('speed', 'fast', s.speed, '快速')}</div>
      <div class="grp"><b>状态演示</b><div class="btns"><button class="btn btn-sm" data-act="con-replay">${ic('play', 'sm')}重播摘要生成过程</button><button class="btn btn-sm" data-act="con-regen" ${C.getV('pg') && readable(C.getV('pg')) ? '' : 'disabled title="先导入示例视频"'}>${ic('refresh', 'sm')}模拟后台生成新原稿</button><button class="btn btn-sm" data-act="con-empty">${ic('video', 'sm')}清空视频库（空状态）</button><button class="btn btn-sm" data-act="con-restore">${ic('undo', 'sm')}恢复示例视频库</button></div></div>
      <div class="grp"><b>重置</b><div class="btns"><button class="btn btn-sm btn-danger" data-act="con-reset">${ic('trash', 'sm')}重置全部演示数据</button></div></div>
      <div class="grp"><b>建议体验路径</b><p style="font-size:12.5px;color:var(--tx-3);line-height:1.7">打开示例摘要 → 引用文字或截图提问 → 放大阅读／问答 → 修改并撤销 → 视频库按标签筛选。查看生成过程：点击「重播摘要生成过程」。</p></div>`
    document.body.appendChild(el)
    el.addEventListener('change', e => { const t = e.target; if (t.dataset.con) { s[t.dataset.con] = t.value; C.save(); toast(t.dataset.con === 'scenario' ? '已设置演示结果：' + { ok: '顺利完成', partial: '会有一张画面补充失败', fail: '摘要会生成失败' }[t.value] : '处理速度：' + (t.value === 'fast' ? '快速' : '正常'), { kind: 'info' }) } })
  }

  /* =========================================================
     Other pages
     ========================================================= */
  function kbHTML() {
    const top = Object.values(S().tags).map(t => ({ t, n: C.tagCount(t.id) })).filter(x => x.n).sort((a, b) => b.n - a.n).slice(0, 8)
    return `<div class="page" style="max-width:920px"><div class="page-head"><div class="grow"><h1>知识库</h1><p>后续开放 · 本原型优先完成单个视频的摘要主流程</p></div></div>
      <div class="kb-card"><h2>按标签组建知识库，跨视频提问</h2><p>视频导入后已经自动归类。之后可以直接用这些标签筛选出一组视频，组成知识库：内容不多时完整放进上下文回答，内容很多时再按需检索。标签只用于筛选，不会改变视频的访问权限。</p>
        <div class="kb-flow"><div><b>1 · 选择标签</b>例如「PostgreSQL」+「故障排查」</div><div><b>2 · 确认视频范围</b>只包含你有权访问的视频</div><div><b>3 · 跨视频提问</b>回答会标出来自哪个视频的哪一段</div></div>
        <div class="vtags" style="margin-top:6px">${top.map(({ t, n }) => `<a class="tag" href="#/library?tags=${t.id}">${esc(t.name)}<span class="n">${n}</span></a>`).join('')}</div>
        <div><button class="btn" disabled>${ic('plus', 'sm')}新建知识库 · 即将推出</button></div></div></div>`
  }
  function askIndexHTML() {
    const list = S().videos.filter(readable)
    return `<div class="page" style="max-width:920px"><div class="page-head"><div class="grow"><h1>问答</h1><p>选择一个视频，围绕它的摘要提问。跨视频问答会在知识库中提供。</p></div></div>
      <div class="recent-list">${list.map(v => `<a class="recent-item" href="#/v/${v.id}/ask"><div class="thumb">${thumbHTML(v)}</div><div class="grow"><b>${esc(v.title)}</b><p>${v.chat.msgs.length ? v.chat.msgs.filter(m => m.role === 'user').length + ' 个问题 · 最近：' + esc(v.chat.msgs.filter(m => m.role === 'user').pop()?.text || '') : esc(plain(C.curDoc(v).overview))}</p></div><span class="btn btn-ghost btn-sm">${ic('message', 'sm')}提问</span></a>`).join('') || `<div class="empty"><div class="art">${ic('message')}</div><h3>还没有可以提问的视频</h3><p>先导入一个视频，摘要可读后就能提问。</p><div class="acts"><a class="btn btn-primary" href="#/">导入视频</a></div></div>`}</div>
      <div class="later-card" style="margin-top:22px">${ic('folder')}<div><b>跨视频问答 · 后续开放</b>会基于标签筛选出的知识库回答。</div></div></div>`
  }
  function notFoundHTML() { return `<div class="page"><div class="empty"><div class="art">${ic('search')}</div><h3>没有找到这个视频</h3><p>它可能已被删除，或演示数据已重置。</p><div class="acts"><a class="btn btn-primary" href="#/library">返回视频库</a></div></div></div>` }

  /* =========================================================
     Scroll spy & misc
     ========================================================= */
  let spyRaf
  function spy() {
    cancelAnimationFrame(spyRaf)
    spyRaf = requestAnimationFrame(() => {
      if (R.name !== 'detail' || R.tab === 'transcript') return
      const secs = $$('#reading .chapter'); let cur = null
      secs.forEach(s => { if (s.getBoundingClientRect().top < 170) cur = s.dataset.blockSec })
      $$('.toc button.item').forEach(b => b.classList.toggle('reading', b.dataset.block === cur))
    })
  }
  function scrollToBlock(id, paraId) {
    const v = curV()
    if (v && workspace(v).focus === 'ask') changeFocus(v, 'restore')
    const root = $('#reading')
    const el = paraId ? root?.querySelector(`.para[data-para="${paraId}"]`) : root?.querySelector(`[data-block-sec="${id}"]`)
    if (!el) return
    el.scrollIntoView({ behavior: 'smooth', block: paraId ? 'center' : 'start' })
    if (paraId) flashPara(el); else flashPara(el.querySelector('.para'))
  }
  function exportMd(v) {
    const doc = C.curDoc(v), ver = C.curVer(v)
    const order = C.figOrder(doc)
    let out = `# ${doc.title}\n\n> 摘要 v${ver.n} · ${ver.label} · 映知 VidLens 原型演示导出（文字版，图片以图注与时间表示）\n\n## 概览\n\n${doc.overview}\n\n${doc.keyPoints.map((k, i) => `${i + 1}. ${k}`).join('\n')}\n\n`
    doc.blocks.forEach((b, i) => {
      out += `## ${i + 1}. ${b.title}（${fmt(b.start)}）\n\n`
      b.paras.forEach(p => { out += plainMd(p.md) + '\n\n'; b.figures.filter(f => f.after === p.id && v.figs[f.id] === 'ok').forEach(f => { out += `> 图 ${order.indexOf(f.id) + 1}（${fmt(doc.figures[f.id].t)}）：${doc.figures[f.id].caption}\n\n` }) })
    })
    const blob = new Blob([out], { type: 'text/markdown;charset=utf-8' })
    const a = document.createElement('a'); a.href = URL.createObjectURL(blob); a.download = `vidlens-summary-v${ver.n}.md`; a.click()
    setTimeout(() => URL.revokeObjectURL(a.href), 1000)
    toast('已导出 Markdown（文字版）')
  }
  const plainMd = s => s.replace(/\{\{t:\d+\}\}/g, '')
  function closeRail() { $('#rail')?.classList.remove('open'); $('.rail-veil')?.remove() }

  /* =========================================================
     Actions
     ========================================================= */
  const curV = () => C.getV(R.id)
  const A = {
    'activity-toggle': el => { const run = AV.get(el.dataset.run); if (!run) return; run.expanded = !run.expanded; const box = el.closest('[data-activity]'); const focused = box?.contains(document.activeElement); if (box) { box.outerHTML = AV.render(run, { compact: !!box.closest('.msg'), title: box.getAttribute('aria-label') }); if (focused) $(`[data-activity="${run.id}"] .activity-toggle`)?.focus({ preventScroll: true }) } C.save() },
    'stop-answer': el => { const v = curV(), m = v?.chat.msgs.find(x => x.id === el.dataset.msg); if (!m || !['thinking', 'streaming'].includes(m.status)) return; cancelQuestion(v, m); renderThread(v); toast('已停止，收到的正文与执行记录保留') },
    'dlg-close': () => closeDialog(),
    'nav-toggle': () => { const v = curV(); if (v) captureReading(v); S().ui.nav = S().ui.nav === 'compact' ? '' : 'compact'; C.save(); applyPrefs(); if (v) requestAnimationFrame(() => restoreReading(v)) },
    'focus-read': () => changeFocus(curV(), 'read'),
    'focus-ask': () => changeFocus(curV(), 'ask'),
    'focus-restore': () => changeFocus(curV(), 'restore'),
    'open-ask': () => changeDock(curV(), 'ask', true),
    'dock-tab': el => changeDock(curV(), el.dataset.tab),
    'dock-toggle': () => { const v = curV(), w = workspace(v); changeDock(v, w.dock === 'closed' ? (w.lastDock || 'player') : 'closed') },
    'video-info': el => { const v = curV(); openPop(el, `<div class="pop-title">${ic('info', 'sm')}视频信息</div><div class="video-info"><b>${esc(v.title)}</b><p>${v.kind === 'bili' ? esc(v.bv) + ' · P' + (v.part || 1) : esc(v.file) + ' · ' + esc(v.size)}</p><p>导入于 ${ago(v.createdAt)}</p>${v.kind === 'bili' ? `<button class="btn btn-sm" data-act="ext-link">${ic('external', 'sm')}打开原视频</button>` : ''}</div>`, { align: 'right' }) },
    'summary-info': el => { const v = curV(), doc = C.curDoc(v); openPop(el, `<div class="pop-title">${ic('sparkles', 'sm')}内容来源</div><div class="video-info"><p>${esc(doc.modeReason)}</p><p>文字来源：${v.kind === 'file' ? '语音识别' : '平台字幕（中文）'}</p><p>图文、文字和关键帧展示同一份摘要。视频截图与时间保持对应。</p><p class="muted">当前内容与画面均为原型演示素材。</p></div>`, { align: 'left' }) },
    theme: el => { S().theme = el.dataset.theme; C.save(); applyPrefs(); updateShell() },
    rail: () => { const r = $('#rail'); if (r.classList.contains('open')) { closeRail(); return } r.classList.add('open'); const veil = document.createElement('div'); veil.className = 'rail-veil'; veil.onclick = closeRail; document.querySelector('.app').appendChild(veil) },
    console: () => toggleConsole(),
    'open-import': () => openImportDialog(),
    'ext-link': () => toast('原型演示：示例 BV 号为虚构，不打开外部链接', { kind: 'info' }),
    /* import */
    'imp-tab': el => { IMP.tab = el.dataset.tab; impRefresh(['source', 'submit']) },
    'imp-sample': () => { IMP.url = D.main.url; IMP.err = ''; lastUrlFocus = false; $$('[data-imp-url]').forEach(i => i.value = IMP.url); onUrlInput(IMP.url) },
    'imp-ex': el => { IMP.focus = IMP.focus === el.textContent ? '' : el.textContent; $$('[data-imp-focus]').forEach(t => t.value = IMP.focus); impRefresh(['examples']) },
    'imp-more': () => { IMP.more = !IMP.more; impRefresh(['more']) },
    'imp-opt': el => { IMP[el.dataset.k] = el.dataset.val; if (el.dataset.k === 'mode' && el.dataset.val === 'text') IMP.visual = false; else if (el.dataset.k === 'mode') IMP.visual = true; impRefresh(['more']) },
    'imp-file': el => el.closest('[data-imp="source"]').querySelector('[data-imp-file]').click(),
    'imp-file-clear': () => { IMP.file = null; impRefresh(['source', 'submit']) },
    'imp-submit': () => submitImport(),
    'imp-read': () => { if (dialogEl()) closeDialog(true); Object.assign(IMP, freshImport()); go('#/v/pg') },
    /* library */
    'lib-tag': el => { const f = libQuery(), id = el.dataset.id; setLib({ tags: f.tags.includes(id) ? f.tags.filter(x => x !== id) : [...f.tags, id] }) },
    'lib-tags-more': () => { const f = libQuery(); const tags = Object.values(S().tags).map(t => ({ t, n: C.tagCount(t.id) })).filter(x => x.n > 0 || f.tags.includes(x.t.id)).sort((a, b) => b.n - a.n || a.t.name.localeCompare(b.t.name, 'zh')); window.DesktopLibrary.openTags(tags, f.tags, ids => setLib({ tags: ids })) },
    'lib-match': el => setLib({ match: el.dataset.m }),
    'lib-st': el => setLib({ st: el.dataset.st }),
    'lib-clear': () => { $('[data-lib-search]') && ($('[data-lib-search]').value = ''); setLib({ kw: '', tags: [], match: 'all', st: 'all' }) },
    'lib-retry': el => { const v = C.getV(el.dataset.id); C.retryPipe(v); toast('已重新开始生成，复用已保存的视频和字幕', { kind: 'info' }) },
    'tag-manager': () => openTagManager(),
    'tm-merge': el => { const m = S().mergeSuggestions.find(x => x.id === el.dataset.id); const a = S().tags[m.from].name, b = S().tags[m.to].name; C.mergeTags(m.from, m.to); renderTM(); if (R.name === 'library') renderLibResults(); toast(`已合并：「${esc(a)}」成为「${esc(b)}」的别名`) },
    'tm-dismiss': el => { S().mergeSuggestions = S().mergeSuggestions.filter(x => x.id !== el.dataset.id); C.save(); renderTM(); toast('已忽略这条建议，Agent 不会再提出', { kind: 'info' }) },
    'tm-edit': el => renderTM(el.dataset.id),
    'tm-cancel': () => renderTM(),
    'tm-save': el => { const id = el.dataset.id, val = dialogEl().querySelector(`[data-tm-name="${id}"]`).value; const old = S().tags[id].name; if (C.renameTag(id, val)) { renderTM(); if (R.name === 'library') renderLibResults(); if (val.trim() !== old) toast(`已重命名为「${esc(val.trim())}」，「${esc(old)}」保留为别名`) } },
    'tm-del': el => { if (el.dataset.confirm) { const n = S().tags[el.dataset.id].name; C.deleteTag(el.dataset.id); renderTM(); if (R.name === 'library') { setLib({ tags: libQuery().tags.filter(x => x !== el.dataset.id) }) } toast(`已删除标签「${esc(n)}」`) } else { el.dataset.confirm = 1; el.innerHTML = '确认删除'; el.classList.remove('btn-ic'); el.classList.add('btn-danger'); setTimeout(() => { if (el.isConnected) renderTM() }, 3000) } },
    /* detail */
    view: el => { const v = curV(); S().ui.view[v.id] = el.dataset.view; C.save(); renderDoc(v) },
    'goto-block': el => { const v = curV(); S().ui.view[v.id] = 'rich'; C.save(); renderDoc(v); setTimeout(() => scrollToBlock(el.dataset.block), 30) },
    'read-settings': el => {
      const s = S().reading
      const pop = openPop(el, `<div class="pop-title">${ic('type', 'sm')}阅读设置</div><div class="read-settings"><div class="row"><span>正文字体</span><div class="seg seg-sm"><button class="${s.font === 'serif' ? 'on' : ''}" data-act="rs" data-k="font" data-v="serif">宋体</button><button class="${s.font === 'sans' ? 'on' : ''}" data-act="rs" data-k="font" data-v="sans">黑体</button></div></div><div class="row"><span>字号</span><div class="seg seg-sm"><button class="${s.size === 's' ? 'on' : ''}" data-act="rs" data-k="size" data-v="s">小</button><button class="${s.size === 'm' ? 'on' : ''}" data-act="rs" data-k="size" data-v="m">中</button><button class="${s.size === 'l' ? 'on' : ''}" data-act="rs" data-k="size" data-v="l">大</button></div></div><div class="row"><span>主题</span><div class="seg seg-sm"><button class="${S().theme === 'dark' ? 'on' : ''}" data-act="rs" data-k="theme" data-v="dark">深色</button><button class="${S().theme === 'light' ? 'on' : ''}" data-act="rs" data-k="theme" data-v="light">浅色</button></div></div></div>`, { align: 'left' })
      pop && (pop.style.minWidth = '280px')
    },
    rs: el => { const k = el.dataset.k; if (k === 'theme') S().theme = el.dataset.v; else S().reading[k] = el.dataset.v; C.save(); applyPrefs(); updateShell(); el.parentElement.querySelectorAll('button').forEach(b => b.classList.toggle('on', b === el)); setTimeout(() => { const v = curV(); if (v) renderDoc(v) }, 0) },
    versions: el => {
      const v = curV(), cur = C.curVer(v)
      openPop(el, `<div class="pop-title">${ic('history', 'sm')}版本记录</div>${v.versions.slice().reverse().map(x => `<div class="ver-item${x.id === v.cur ? ' cur' : ''}${x.undone ? ' undone' : ''}"><span class="v">v${x.n}</span><div class="grow"><b>${esc(x.label)}</b>${x.note ? `<small>「${esc(short(x.note, 28))}」</small>` : ''}<small>${clock(x.at)}${x.id === v.cur ? ' · 当前阅读' : x.undone ? ' · 已撤销' : ''}</small></div></div>`).join('')}${cur.by === 'revise' || (cur.base && cur.by === 'agent' && cur.n > 1) ? `<div class="menu-sep"></div><button class="menu-item" data-act="undo">${ic('undo', 'sm')}<span class="grow">撤销到 v${(v.versions.find(x => x.id === cur.base) || {}).n || ''}<small>正文、画面位置和导图一起恢复</small></span></button>` : ''}`, { cls: '', align: 'right' })
    },
    undo: () => { closePop(); doUndo(curV()) },
    'map-toggle': () => { const v = curV(), open = !(S().ui.mapOpen[v.id] ?? (v.prefs.map === 'open')); S().ui.mapOpen[v.id] = open; C.save(); const w = $('#map'); w.outerHTML = mapHTML(v, C.curDoc(v)) },
    'map-zoom': () => { const v = curV(); UIS.zoomFold = {}; const doc = C.curDoc(v); openDialog(`<div class="dlg-head"><span class="dlg-icon">${ic('map')}</span><div class="grow"><h3>${esc(doc.mapTitle)}</h3><p>点击节点回到摘要正文</p></div><button class="btn btn-ghost btn-sm btn-ic" data-act="dlg-close" aria-label="关闭导图">${ic('x')}</button></div><div class="dlg-body map-expanded" id="map-expanded">${window.MindMap.build(doc, { folded: UIS.zoomFold }).svg.replaceAll('data-act="map-fold"', 'data-act="map-dialog-fold"')}</div>`, { width: 1140, cls: 'map-dialog' }) },
    'map-dialog-fold': el => { UIS.zoomFold[el.dataset.block] = !UIS.zoomFold[el.dataset.block]; $('#map-expanded').innerHTML = window.MindMap.build(C.curDoc(curV()), { folded: UIS.zoomFold }).svg.replaceAll('data-act="map-fold"', 'data-act="map-dialog-fold"') },
    'map-expand': () => { const v = curV(); UIS.mapFold[v.id] = {}; $('#map').outerHTML = mapHTML(v, C.curDoc(v)) },
    'map-fold': el => { const v = curV(), f = UIS.mapFold[v.id] = UIS.mapFold[v.id] || {}; f[el.dataset.block] = !f[el.dataset.block]; $('#map').outerHTML = mapHTML(v, C.curDoc(v)); $(`#map [data-act="map-fold"][data-block="${el.dataset.block}"]`)?.focus() },
    'map-go': el => { if (el.closest('.map-dialog')) closeDialog(true); scrollToBlock(el.dataset.block, el.dataset.para) },
    'map-top': () => { $('#blk-overview')?.scrollIntoView({ behavior: 'smooth' }) },
    toc: el => scrollToBlock(el.dataset.block),
    zoom: el => openLightbox(curV(), el.dataset.fig),
    'retry-fig': el => C.retryFig(curV(), el.dataset.fig),
    'skip-fig': el => { C.skipFig(curV(), el.dataset.fig); toast('已跳过这张画面，正文不受影响', { kind: 'info' }) },
    'retry-summary': () => { const v = curV(); C.retryPipe(v); render() },
    'log-toggle': () => { const v = curV(); S().ui.logOpen[v.id] = !S().ui.logOpen[v.id]; C.save(); $('#progress-slot').innerHTML = progressHTML(v) },
    'goto-failed': () => { const v = curV(); const id = Object.keys(v.figs).find(k => v.figs[k] === 'failed'); if (viewOf(v) === 'text') { S().ui.view[v.id] = 'rich'; renderDoc(v) } setTimeout(() => $(`[data-fig-slot="${id}"]`)?.scrollIntoView({ behavior: 'smooth', block: 'center' }), 30) },
    seek: el => {
      const v = curV(); if (!v) return
      if (workspace(v).dock !== 'player' || workspace(v).focus) changeDock(v, 'player')
      C.pLoad(v); C.pSeek(+el.dataset.t)
      const p = $('#side .player'); if (p) { p.classList.remove('flash'); void p.offsetWidth; p.classList.add('flash') }
    },
    'p-toggle': el => { const v = curV() || C.getV(el.closest('[data-vid]')?.dataset.vid); if (v) C.pLoad(v); C.pToggle() },
    'p-rate': () => { P.rate = P.rate === 1 ? 1.5 : P.rate === 1.5 ? 2 : 1; C.pSync() },
    'm-player': () => { const v = curV(); UIS.mPlayer = !UIS.mPlayer; const m = $('#m-player'); m.innerHTML = mPlayerHTML(v); C.bindScrub(m); C.pSync() },
    'float-close': () => { UIS.floatOpen = false; $('#float-slot').innerHTML = ''; C.pPause() },
    more: el => {
      const v = curV()
      openPop(el, `<button class="menu-item" data-act="export" ${readable(v) ? '' : 'disabled'}>${ic('download', 'sm')}<span class="grow">导出 Markdown<small>文字版，图片以图注和时间表示</small></span></button><button class="menu-item" data-act="regen" ${readable(v) ? '' : 'disabled'}>${ic('refresh', 'sm')}<span class="grow">重新生成摘要<small>新原稿发布前，当前版本继续可读</small></span></button><button class="menu-item" data-act="log-open">${ic('list', 'sm')}<span class="grow">处理详情<small>字幕来源、画面挑选、归类记录</small></span></button><div class="menu-sep"></div><button class="menu-item" disabled>${ic('folder', 'sm')}<span class="grow">加入知识库<small>后续开放</small></span></button>`, { align: 'right' })
    },
    'log-open': () => { closePop(); const v = curV(); if (R.tab !== 'summary') { S().ui.logOpen[v.id] = true; go('#/v/' + v.id); return } S().ui.logOpen[v.id] = true; $('#progress-slot').innerHTML = progressHTML(v); content().scrollTo({ top: $('#progress-slot').offsetTop - 80, behavior: 'smooth' }) },
    export: () => { closePop(); exportMd(curV()) },
    regen: () => {
      closePop(); const v = curV()
      toast('正在重新生成摘要，当前版本继续可读…', { kind: 'info' })
      setTimeout(() => {
        const r = C.regenerate(v)
        if (R.id === v.id) { if (r === 'pending') { const s = $('#regen-slot'); if (s) s.innerHTML = regenHTML(v) } else refreshAfterVersion(v) }
        toast(r === 'pending' ? '新原稿已生成。你修改过的版本保持不变，可以选择是否采用' : `已更新为新的摘要原稿 v${C.curVer(v).n}`, { kind: r === 'pending' ? 'info' : '' })
      }, 1600 * (S().speed === 'fast' ? .5 : 1))
    },
    'regen-view': () => {
      const v = curV(), doc = C.curDoc(v), nd = v.pendingRegen.doc, ver = C.curVer(v)
      const ids = Object.keys(D.regenerated.patch).concat(ver.changed || [])
      const rows = ids.map(pid => { const a = C.findPara(doc, pid), b = C.findPara(nd, pid); return a && b && a.para.md !== b.para.md ? { a, b, pid, mine: (ver.changed || []).includes(pid) } : null }).filter(Boolean)
      openDialog(`<div class="dlg-head"><span class="dlg-icon">${ic('eye')}</span><div class="grow"><h3>新原稿与你的版本有 ${rows.length} 处不同</h3><p>${esc(v.pendingRegen.note)}</p></div><button class="btn btn-ghost btn-sm btn-ic" data-act="dlg-close" aria-label="关闭">${ic('x')}</button></div><div class="dlg-body">${rows.map(r => `<div class="diff-block"><div class="diff-title">${esc(r.a.block?.title || '概览')}${r.mine ? '<span class="chip chip-warn" style="height:22px;margin-left:auto">你修改过这一段</span>' : ''}</div><div class="diff-cols"><div class="diff-col before"><div class="lbl">你的版本 · v${ver.n}</div><div class="prose">${md(r.a.para.md)}</div></div><div class="diff-col after"><div class="lbl">Agent 新原稿</div><div class="prose">${md(r.b.para.md)}</div></div></div></div>`).join('')}</div><div class="dlg-foot"><span class="grow">采用新原稿会替换你修改过的段落，之后仍可撤销</span><button class="btn btn-quiet" data-act="regen-keep">保留我的版本</button><button class="btn btn-primary" data-act="regen-adopt">采用新原稿</button></div>`, { width: 820 })
    },
    'regen-keep': () => { const v = curV(); v.pendingRegen = null; C.save(); closeDialog(); $('#regen-slot') && ($('#regen-slot').innerHTML = ''); toast(`已保留你的版本 v${C.curVer(v).n}`) },
    'regen-adopt': () => { const v = curV(); C.adoptRegen(v); closeDialog(); refreshAfterVersion(v); toast(`已采用新原稿，摘要更新为 v${C.curVer(v).n}`, { action: { label: '撤销', fn: () => doUndo(v) } }) },
    'tag-edit': el => openTagEditor(el),
    'te-add': el => { const v = curV(); const t = C.addTag(v, el.dataset.name); if (el.dataset.sugg) v.suggestion = null; if (t) { tagsChanged(v); refreshTE(v) } },
    'te-remove': el => { const v = curV(); const name = S().tags[el.dataset.id].name; const l = C.removeTag(v, el.dataset.id); tagsChanged(v); refreshTE(v); toast(l?.origin === 'auto' ? `已移除「${esc(name)}」，Agent 之后不会再自动添加` : `已移除「${esc(name)}」`, { kind: 'info' }) },
    'te-restore': el => { const v = curV(); v.rejected = v.rejected.filter(x => x !== el.dataset.id); C.addTag(v, S().tags[el.dataset.id].name); tagsChanged(v); refreshTE(v) },
    'te-retag': () => {
      const v = curV(), names = v.rejected.map(id => S().tags[id]?.name).filter(Boolean)
      const r = C.applyTagging(v, true); tagsChanged(v); refreshTE(v)
      toast(`Agent 重新归类完成：新增 ${r.added} 个，保留你维护的 ${r.kept} 个${names.length ? `，没有重新添加你移除的「${names.map(esc).join('」「')}」` : ''}`, { ms: 5200 })
    },
    'te-done': () => closePop(),
    /* chapter actions */
    'block-ask': el => { const v = curV(), doc = C.curDoc(v), b = el.dataset.block; if (addAnno(v, { blockId: b, paraId: b === 'overview' ? 'overview' : doc.blocks.find(x => x.id === b).paras[0].id, quote: blockText(doc, b), kind: 'block' })) afterAnno(v) },
    'fig-ask': el => { const v = curV(), doc = C.curDoc(v), id = el.dataset.fig, f = doc.figures[id], b = doc.blocks.find(x => x.figures.some(y => y.id === id)), place = b.figures.find(x => x.id === id); if (addAnno(v, { blockId: b.id, paraId: place.after || b.paras[0].id, quote: f.caption + '（视频 ' + fmt(f.t) + '）', kind: 'figure', figId: id, figSrc: f.src, figTime: f.t })) afterAnno(v) },
    'block-edit': el => openRevision(curV(), { kind: 'block', blockId: el.dataset.block }, '', { from: R.tab === 'ask' ? 'ask' : 'doc' }),
    'open-edit': () => openRevision(curV(), { kind: 'doc' }),
    /* selection */
    'sel-ask': () => { if (!SEL) return; const v = C.getV(SEL.vid); const s = SEL; getSelection().removeAllRanges(); hideSel(); if (addAnno(v, { blockId: s.blockId, paraId: s.paraId, quote: s.quote, kind: 'selection' })) afterAnno(v) },
    'sel-edit': () => { if (!SEL) return; const v = C.getV(SEL.vid), s = SEL; getSelection().removeAllRanges(); hideSel(); openRevision(v, s.multi ? { kind: 'block', blockId: s.blockId } : { kind: 'para', blockId: s.blockId, paraId: s.paraId, quote: s.quote }, '', { from: s.inAsk ? 'ask' : 'doc' }) },
    'sel-copy': () => { if (!SEL) return; navigator.clipboard?.writeText(SEL.quote).then(() => toast('已复制'), () => toast('复制失败，请手动复制', { kind: 'warn' })); getSelection().removeAllRanges(); hideSel() },
    /* ask */
    'ask-panel': () => togglePanel(!askOpen()),
    'ask-new': () => { cancelQuestion(); const v = curV(); v.chat.msgs.filter(m => ['thinking', 'streaming'].includes(m.status)).forEach(m => AV.finish(m.activity, 'cancelled')); UIS.questionId = null; UIS.busy = false; v.chat.msgs = []; v.chat.topic = null; C.save(); renderThread(v, true); renderDock(v); toast('已开始新对话', { kind: 'info' }) },
    mode: el => { const v = curV(), t = $('#ask-input'); if (t) { if (v.chat.mode === 'edit') v.chat.editDraft = t.value; else v.chat.draft = t.value } v.chat.mode = el.dataset.mode; C.save(); renderComposer(v, true) },
    send: () => {
      const v = curV(), t = $('#ask-input'), q = t.value.trim()
      if (v.chat.mode === 'edit') {
        const a = v.chat.annos[0]
        const scope = a ? (a.kind === 'block' ? { kind: 'block', blockId: a.blockId } : { kind: 'para', blockId: a.blockId, paraId: a.paraId, quote: a.quote }) : { kind: 'doc' }
        v.chat.editDraft = t.value; C.save()
        openRevision(v, scope, q, { auto: !!q, from: 'ask' }); return
      }
      if (!q) { toast(v.chat.annos.length ? '注释已准备好，再输入你的问题' : '先输入你的问题', { kind: 'info' }); t.focus(); return }
      sendQuestion(v, q)
    },
    sugg: el => { const v = curV(); v.chat.mode = 'ask'; v.chat.draft = el.dataset.q; C.save(); renderComposer(v, true) },
    'snap-toggle': el => el.classList.toggle('open'),
    'anno-open': el => { const v = curV(), u = UIS.annoUI[el.dataset.id] = UIS.annoUI[el.dataset.id] || {}; u.open = !u.open; renderComposer(v) },
    'anno-ver': el => { const v = curV(), u = UIS.annoUI[el.dataset.id] = UIS.annoUI[el.dataset.id] || {}; u.ver = !u.ver; renderComposer(v) },
    'anno-remove': el => { const v = curV(); v.chat.annos = v.chat.annos.filter(a => a.id !== el.dataset.id); C.save(); renderComposer(v, true) },
    'anno-clear': () => { const v = curV(); v.chat.annos = []; C.save(); renderComposer(v, true) },
    'anno-goto': el => {
      const v = curV(), a = v.chat.annos.find(x => x.id === el.dataset.id); if (!a) return
      if (workspace(v).focus === 'ask') changeFocus(v, 'restore')
      setTimeout(() => {
        const para = $(`#reading .para[data-para="${a.paraId}"]`) || $(`#reading [data-block-sec="${a.blockId}"] .para`)
        if (!para) return
        para.scrollIntoView({ behavior: 'smooth', block: 'center' })
        const stale = a.ver !== C.curVer(v).id
        if (a.kind === 'block' || !markQuote(para, a.quote) || stale) flashPara(para)
        if (stale) toast(`摘要已更新到 v${C.curVer(v).n}，这段可能已变化，已定位到所在段落`, { kind: 'info' })
      }, 20)
    },
    /* revision dialog */
    'rev-preset': el => { const t = $('#rev-req'); t.value = el.dataset.p; REV.req = t.value; t.focus() },
    'rev-go': () => startRev(),
    'rev-back': () => { REV.step = 'input'; renderRev() },
    'rev-apply': () => applyRev(),
    /* lightbox */
    'lb-close': () => closeLB(),
    'lb-prev': () => { LB.i = (LB.i - 1 + LB.list.length) % LB.list.length; LB.zoom = false; renderLB() },
    'lb-next': () => { LB.i = (LB.i + 1) % LB.list.length; LB.zoom = false; renderLB() },
    'lb-zoom': () => { LB.zoom = !LB.zoom; renderLB() },
    'lb-seek': el => { const t = +el.dataset.t; closeLB(); A.seek({ dataset: { t } }) },
    /* console */
    'con-empty': () => { cancelQuestion(); const s = S(); if (s.videos.length) s.backup = s.videos; s.videos = []; C.save(); $('#console')?.remove(); go('#/library'); toast('已清空视频库，可以在控制台恢复', { kind: 'info' }) },
    'con-restore': () => { cancelQuestion(); const s = S(); if (s.backup?.length) { s.videos = s.backup; s.backup = null } else { const fresh = C.seed(); s.videos = fresh.videos.concat(s.videos.filter(v => v.template === 'pg')); Object.assign(s.tags, fresh.tags) } C.save(); C.resumeAll(); $('#console')?.remove(); render(); toast('已恢复示例视频库') },
    'con-regen': () => { $('#console')?.remove(); const v = C.getV('pg'); go('#/v/pg'); setTimeout(() => { R.id = 'pg'; A.regen() }, 50) },
    'con-replay': () => { cancelQuestion(); $('#console')?.remove(); C.pPause(); let v = C.getV('pg'); if (!v) { v = C.seed().videos.find(x => x.id === 'pg'); S().videos.unshift(v) } v.versions = []; v.cur = null; v.figs = {}; v.tagLinks = []; UIS.workspaces[v.id] = { dock: 'player', focus: '', top: 0, anchor: null, threadTop: 0, threadBottom: true }; S().ui.logOpen[v.id] = false; C.startPipe(v); go('#/v/pg') },
    'con-reset': () => { cancelQuestion(); Object.values(C.timers).forEach(clearTimeout); C.pPause(); localStorage.removeItem(C.KEY); C.setS(C.seed()); C.save(); Object.assign(IMP, freshImport()); UIS.mPlayer = false; UIS.floatOpen = false; UIS.workspaces = {}; UIS.mapFold = {}; UIS.mapChanged = {}; UIS.annoUI = {}; UIS.questionId = null; UIS.busy = false; P.vid = null; applyPrefs(); $('#console')?.remove(); C.resumeAll(); go('#/'); toast('演示数据已重置') }
  }
  document.addEventListener('click', e => {
    const el = e.target.closest('[data-act]')
    if (!el || el.disabled) return
    const f = A[el.dataset.act]; if (!f) return
    if (el.tagName === 'A') e.preventDefault()
    f(el, e)
  })
  document.addEventListener('keydown', e => {
    const t = e.target
    if ((e.key === 'Enter' || e.key === ' ') && t.matches?.('[role="button"][data-act]:not(button)')) { e.preventDefault(); t.dispatchEvent(new MouseEvent('click', { bubbles: true })) }
    if (t.id === 'ask-input' && e.key === 'Enter' && !e.shiftKey && !e.isComposing) { e.preventDefault(); A.send() }
    if (t.id === 'rev-req' && e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); startRev() }
    if (LB) { if (e.key === 'ArrowLeft') A['lb-prev'](); if (e.key === 'ArrowRight') A['lb-next']() }
    if (e.key === 'Escape') {
      if (LB) return closeLB()
      if (dialogEl()) return closeDialog()
      if ($('.popover')) return closePop()
      if (selEl) { hideSel(); return }
      if ($('#console')) return $('#console').remove()
      if ($('#rail.open')) return closeRail()
      const v = curV(); if (v && workspace(v).focus) { changeFocus(v, 'restore'); return }
      if (R.tab === 'ask' && C.isMobile() && askOpen()) return togglePanel(false)
    }
  })
  document.addEventListener('input', e => {
    const t = e.target
    if (t.matches('[data-imp-url]')) { lastUrlFocus = true; onUrlInput(t.value) }
    if (t.matches('[data-imp-focus]')) { IMP.focus = t.value; $$('[data-imp-focus]').forEach(x => { if (x !== t) x.value = t.value }); impRefresh(['examples']) }
    if (t.matches('[data-lib-search]')) { clearTimeout(t._t); t._t = setTimeout(() => setLib({ kw: t.value }), 200) }
    if (t.id === 'ask-input') { const v = curV(); if (v.chat.mode === 'edit') v.chat.editDraft = t.value; else v.chat.draft = t.value; C.save(); autosize() }
    if (t.id === 'rev-req' && REV) REV.req = t.value
  })
  document.addEventListener('change', e => {
    const t = e.target
    if (t.matches('[data-imp-check]')) { IMP[t.dataset.impCheck] = t.checked; impRefresh(['more', 'submit']) }
    if (t.matches('[data-follow]')) { S().ui.follow = t.checked; C.save(); toast(t.checked ? '播放时会自动滚动到对应章节' : '已关闭跟随播放', { kind: 'info' }) }
    if (t.matches('[data-rev-scope]') && REV) { const opts = [REV.initialScope.kind !== 'doc' ? REV.initialScope : null, { kind: 'doc' }].filter(Boolean); REV.req = $('#rev-req').value; REV.scope = opts[+t.dataset.revScope]; renderRev() }
  })
  document.addEventListener('focusout', e => { if (e.target.matches?.('[data-imp-url]')) lastUrlFocus = false })

  /* =========================================================
     Hooks from Core
     ========================================================= */
  window.UI = {
    onVideo(v, parts) {
      if (R.name === 'detail' && R.id === v.id) {
        if (parts.includes('all')) { render(); return }
        if (R.tab !== 'transcript') {
          if (parts.includes('progress')) { const s = $('#progress-slot'); if (s) s.innerHTML = progressHTML(v) }
          if (parts.includes('doc')) renderDoc(v)
          parts.filter(p => p.startsWith('fig:')).forEach(p => {
            const id = p.slice(4); if (v.figs[id] === 'ok') UIS.revealed.add(v.id + id)
            $$(`[data-fig-slot="${id}"]`).forEach(el => { el.outerHTML = figureHTML(v, C.curDoc(v), id) })
          })
        }
        if (parts.includes('doc')) { const tb = $('.tabs-bar'); if (tb) tb.outerHTML = tabsHTML(v) }
        if (parts.includes('doc')) { const ask = $('#side [data-tab="ask"]'); if (ask) ask.disabled = !readable(v) }
        if (parts.includes('side') || parts.includes('doc')) { const t = $('#toc-slot'); if (t) t.innerHTML = tocHTML(v) }
        if (parts.includes('side')) { const sp = $('#side-player'); if (sp && !P.playing) { sp.innerHTML = C.playerHTML(v); C.bindScrub(sp); C.pSync() } }
        if (parts.includes('tags') || parts.includes('progress')) renderTags(v)
      }
      if (R.name === 'library') { const el = $(`[data-card="${v.id}"]`); if (el) el.outerHTML = cardHTML(v, 0, true); if (parts.includes('tags')) { const tb = $('#lib-tagbar'); if (tb) tb.innerHTML = tagbarHTML(libQuery()) } }
      if (R.name === 'home' && (parts.includes('doc') || parts.includes('card'))) { const r = $('#recent-slot'); if (r && readable(v)) r.innerHTML = S().videos.filter(readable).slice().sort((a, b) => b.updatedAt - a.updatedAt).slice(0, 3).map(recentHTML).join('') }
    },
    onTextReady(v) {
      const here = R.name === 'detail' && R.id === v.id
      if (here) toast('摘要已可阅读，画面会继续补充')
      else toast(`「${esc(short(v.title, 16))}」的摘要已可阅读`, { action: { label: '阅读', fn: () => go('#/v/' + v.id) } })
    },
    onDone(v) {
      if (v.template !== 'pg' && v.template !== 'vite' && v.template !== 'loom') return
      const here = R.name === 'detail' && R.id === v.id
      const failed = Object.values(v.figs).filter(x => x === 'failed').length
      if (here) toast(failed ? `处理完成，有 ${failed} 张画面没有补充成功，可以重试` : '摘要已完成：画面和标签都已补充', { kind: failed ? 'warn' : '' })
      else if (v.template === 'pg') toast(`「${esc(short(v.title, 16))}」已处理完成`, { action: { label: '查看', fn: () => go('#/v/' + v.id) } })
    },
    onPlayerTick(v, f) {
      if (R.tab === 'transcript') {
        const cues = C.cuesOf(v); let cur = null; for (const c of cues) { if (c.t <= P.t) cur = c.t; else break }
        $$('.cue-row').forEach(r => r.classList.toggle('on', +r.dataset.cue === cur))
      }
      if (S().ui.follow && P.playing && f.block && R.tab !== 'transcript' && workspace(v).focus !== 'ask' && UIS.lastFollow !== f.block.id) {
        UIS.lastFollow = f.block.id
        $(`#reading [data-block-sec="${f.block.id}"]`)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
      }
    }
  }

  /* =========================================================
     Init
     ========================================================= */
  function init() {
    document.body.insertAdjacentHTML('afterbegin', C.sprite)
    $('#root').innerHTML = shellHTML()
    applyPrefs()
    S().videos.forEach(v => v.chat.msgs.forEach(m => { if (m.role === 'ai' && ['thinking', 'streaming'].includes(m.status)) { m.status = 'cancelled'; AV.finish(m.activity, 'cancelled'); m.text = '（模拟回答在页面刷新时中断，请重新提问）' } }))
    content().addEventListener('scroll', () => { spy(); if (selEl && !matchMedia('(pointer: coarse)').matches) hideSel() }, { passive: true })
    addEventListener('resize', () => { hideSel(); closePop() })
    if (new URLSearchParams(location.search).has('reset')) history.replaceState(null, '', location.pathname + location.hash)
    render()
    C.resumeAll()
  }
  init()
})()
