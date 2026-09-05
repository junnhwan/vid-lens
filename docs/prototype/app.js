/* ============================================================
   映知 VidLens 原型 · 交互逻辑 (演示用, 非生产代码)
   - hash 路由: #/dashboard #/library #/kb #/kb/:id #/video/:id #/chat/v/:id #/chat/kb/:id #/settings
   - Agent 运行引擎: 模拟 SSE 事件序列 (run_start / step_* / tool_* / retrieve_hits / answer / citations / done)
   - 播放器: 引用跳转 -> 视频寻址 -> 转写高亮 -> 画面帧取样
   ============================================================ */

/* ---------------- 小工具 ---------------- */
const $ = (s, r) => (r || document).querySelector(s);
const $$ = (s, r) => Array.from((r || document).querySelectorAll(s));
const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const ic = (n, cls) => `<svg class="ic ${cls || ""}"><use href="#i-${n}"/></svg>`;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const rid = () => Math.random().toString(16).slice(2, 6) + Math.random().toString(16).slice(2, 6);

function fmtClock(ms) {
  const t = Math.max(0, Math.round(ms / 1000));
  const h = Math.floor(t / 3600), m = Math.floor((t % 3600) / 60), s = t % 60;
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}` : `${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}
function fmtDur(ms) { return fmtClock(ms); }
const videoById = (id) => VIDEOS.find((v) => v.id === Number(id));
const kbById = (id) => KBS.find((k) => k.id === Number(id));

const MODALITY = {
  transcript: { cls: "tag-transcript", text: "转写" },
  visual_ocr: { cls: "tag-ocr", text: "画面 OCR" },
  visual_caption: { cls: "tag-caption", text: "画面描述" },
};
function modalityTag(m) { const x = MODALITY[m] || MODALITY.transcript; return `<span class="tag-modality ${x.cls}">${x.text}</span>`; }

const CLAIM_STATUS = {
  verified: { chip: "chip-ok", icon: "shield-check", text: "已验证" },
  uncertain: { chip: "chip-warn", icon: "alert", text: "不确定" },
  unsupported: { chip: "chip-bad", icon: "x", text: "无证据支撑" },
  corrected: { chip: "chip-info", icon: "pencil", text: "已更正" },
  hypothesized: { chip: "chip-mute", icon: "clock", text: "待核" },
};
function claimChip(st) { const x = CLAIM_STATUS[st] || CLAIM_STATUS.hypothesized; return `<span class="chip ${x.chip}">${ic(x.icon, "ic-sm")}${x.text}</span>`; }

const INSPECT_RESULT = {
  support: { chip: "chip-ok", text: "支持" },
  contradict: { chip: "chip-bad", text: "矛盾" },
  insufficient: { chip: "chip-warn", text: "证据不足" },
};

function claimsHTML(claims, cites) {
  return claims.map((cl, i) => {
    const ins = cl.inspect || {};
    const ir = INSPECT_RESULT[ins.result] || INSPECT_RESULT.insufficient;
    return `
    <div class="claim-card" data-claim="${i}">
      <div class="claim-top">${claimChip(cl.status)}<span class="mono" style="margin-left:auto;font-size:10px;color:var(--tx-4)">置信 ${cl.conf.toFixed(2)}</span></div>
      <div class="claim-text">${esc(cl.text)}</div>
      ${cl.evs.length ? `<div class="claim-evis">${cl.evs.map((n) => {
        const c = (cites || []).find((x) => x.c === n);
        return c ? `<div class="claim-ev" data-ev="${n}"><span class="eid mono">C${n}</span><span style="flex:1;min-width:0">${esc(c.quote.slice(0, 44))}…</span>${modalityTag(c.modality)}</div>` : "";
      }).join("")}</div>` : ""}
      <div class="claim-note">${ic("info", "ic-sm")}<span>${esc(cl.note)}</span></div>
      <div class="inspect-line mono">claim-inspector-v2-pixel · <span class="chip ${ir.chip}" style="height:16px;font-size:9.5px;padding:0 6px">${ir.text}</span> · 反例检索${ins.searchCompleted ? "已完成" : "未完成"}</div>
      ${ins.counterQuery ? `<div class="claim-note" style="margin-top:6px">${ic("search", "ic-sm")}<span class="mono" style="font-size:10.5px">counter_query: ${esc(ins.counterQuery)}</span></div>` : ""}
      ${ins.pixel ? `<div class="claim-note" style="margin-top:6px">${ic("eye", "ic-sm")}<span>像素核验: ${esc(ins.pixel)}</span></div>` : ""}
      ${cl.status !== "verified" ? `<button class="btn btn-sm" style="margin-top:10px" data-correct="${i}">${ic("pencil", "ic-sm")}追加更正</button><div class="correction-box" data-cbox="${i}" style="display:none">
        <textarea placeholder="说明哪里不对,会被追加为新的 Claim 修订,不覆盖历史…"></textarea>
        <div style="display:flex;justify-content:flex-end;gap:8px;margin-top:8px"><button class="btn btn-sm btn-primary" data-csend="${i}">提交更正</button></div>
      </div>` : ""}
    </div>`;
  }).join("");
}

function bindClaimCards(scopeEl, cites) {
  $$("[data-correct]", scopeEl).forEach((b) => b.addEventListener("click", () => {
    const box = $(`[data-cbox="${b.getAttribute("data-correct")}"]`, scopeEl);
    box.style.display = box.style.display === "none" ? "block" : "none";
    $("textarea", box).focus();
  }));
  $$("[data-csend]", scopeEl).forEach((b) => b.addEventListener("click", () => {
    const box = $(`[data-cbox="${b.getAttribute("data-csend")}"]`, scopeEl);
    const val = $("textarea", box).value.trim();
    if (!val) { toast("先写下更正内容"); return; }
    toast("已追加人工更正,将生成新的 Claim 修订 (演示)");
    box.style.display = "none"; $("textarea", box).value = "";
  }));
  $$("[data-ev]", scopeEl).forEach((row) => row.addEventListener("click", () => {
    const cit = (cites || []).find((x) => x.c === Number(row.getAttribute("data-ev")));
    if (cit) openEvidenceDrawer(cit, cites);
  }));
}

function toast(msg, icon) {
  const t = document.createElement("div");
  t.className = "toast";
  t.innerHTML = ic(icon || "check") + esc(msg);
  $("#toasts").appendChild(t);
  setTimeout(() => { t.classList.add("out"); setTimeout(() => t.remove(), 320); }, 2600);
}

/* ---------------- 路由 ---------------- */
let currentDeck = null;
let chatState = null;

function nav(hash) { location.hash = hash; }

document.addEventListener("click", (e) => {
  const nv = e.target.closest("[data-nav]");
  if (nv) { nav(nv.getAttribute("data-nav")); return; }
  if (e.target.closest("[data-open-upload]")) { openUploadModal(); return; }
  if (e.target.closest("#globalSearch")) { nav("#/library"); setTimeout(() => { const i = $("#libFilter"); if (i) i.focus(); }, 60); }
});
$("#demoBarClose").addEventListener("click", () => $("#demoBar").remove());

window.addEventListener("hashchange", render);
window.addEventListener("keydown", (e) => {
  if (e.key === "Escape") { closeOverlay(); }
  if (e.key === "/" && !e.target.closest("input,textarea")) {
    e.preventDefault(); nav("#/library");
    setTimeout(() => { const i = $("#libFilter"); if (i) i.focus(); }, 60);
  }
});
function closeOverlay() {
  const o = $("#overlayRoot");
  if (o) o.innerHTML = "";
}

function render() {
  if (currentDeck) { currentDeck.destroy(); currentDeck = null; }
  closeOverlay();
  const h = (location.hash.replace(/^#\/?/, "") || "dashboard").split("?")[0];
  const parts = h.split("/");
  const page = parts[0] || "dashboard";
  const content = $("#content");
  content.scrollTop = 0;
  $$(".nav-item").forEach((b) => b.classList.remove("active"));

  const markNav = (key) => { const b = $(`.nav-item[data-nav^="#/${key}"]`); if (b) b.classList.add("active"); };

  if (page === "dashboard") { markNav("#/dashboard"); pageDashboard(content); }
  else if (page === "library") { markNav("#/library"); pageLibrary(content); }
  else if (page === "kb" && parts[1]) { markNav("#/kb"); pageChat(content, { type: "kb", id: Number(parts[1]) }); }
  else if (page === "kb") { markNav("#/kb"); pageKbList(content); }
  else if (page === "video") { markNav("#/library"); pageVideo(content, Number(parts[1])); }
  else if (page === "chat" && parts[1] === "v") { markNav("#/dashboard"); pageChat(content, { type: "video", id: Number(parts[2]) }); }
  else if (page === "chat" && parts[1] === "kb") { markNav("#/kb"); pageChat(content, { type: "kb", id: Number(parts[2]) }); }
  else if (page === "settings") { markNav("#/settings"); pageSettings(content); }
  else { pageDashboard(content); }
}

function setCrumb(items, actions) {
  $("#crumb").innerHTML = items
    .map((x, i) => (i === items.length - 1 ? `<b>${esc(x)}</b>` : `<span>${esc(x)}</span><span class="div">/</span>`))
    .join("");
  if (actions) { /* 预留 */ }
}

/* ---------------- 共用片段 ---------------- */
function vcardHTML(v) {
  const state = v.status === "indexed" ? "" : `<span class="vstate chip ${v.statusChip}">${esc(v.stateText)}</span>`;
  const prog = v.status === "transcribing"
    ? `<div class="mini-prog"><div class="row"><b>${esc(v.stageText)}</b><span class="mono">${Math.round(v.progress * 100)}%</span></div><div class="meter"><i style="width:${v.progress * 100}%"></i></div></div>`
    : v.status === "failed"
      ? `<div class="mini-prog"><div class="row"><b style="color:var(--bad)">${esc(v.errText)}</b></div></div>`
      : "";
  const art = v.demo
    ? `<img loading="lazy" src="./assets/poster-oceans.jpg" alt="" onerror="this.parentElement.classList.add('vthumb-art')" />`
    : `<img loading="lazy" src="https://picsum.photos/seed/vidlens-${v.id}/560/315.jpg" alt="" onerror="this.style.display='none';this.parentElement.classList.add('vthumb-art')" />`;
  const sub = v.status === "transcribing"
    ? `<span class="mono">${Math.round(v.progress * 100)}%</span><span>转写中</span>`
    : v.status === "failed"
      ? `<span>需要重试</span>`
      : `<span class="mono">${fmtDur(v.durationMs)}</span><span>${v.hasVisual ? "含画面索引" : "文本索引"}</span>`;
  return `
  <div class="vcard" data-nav="#/video/${v.id}">
    <div class="vthumb">${art}${state}<span class="vlen">${fmtDur(v.durationMs)}</span></div>
    <div class="vmeta">
      <h4>${esc(v.title)}</h4>
      <div class="vsub">${sub}<span style="margin-left:auto">${esc(v.updated)}</span></div>
      ${prog}
    </div>
  </div>`;
}

function kbCardHTML(k) {
  const thumbs = (k.members || []).slice(0, 3).map((id) => {
    const v = videoById(id);
    const inner = v && v.demo
      ? `<img loading="lazy" src="./assets/poster-oceans.jpg" alt="" onerror="this.remove()" />`
      : `<img loading="lazy" src="https://picsum.photos/seed/vidlens-${id}/96/96.jpg" alt="" onerror="this.remove()" />`;
    return `<span class="vthumb-mini">${inner}</span>`;
  }).join("");
  const moreN = Math.max(0, (k.videoCount || 0) - Math.min(3, (k.members || []).length));
  return `
  <div class="kb-card" data-nav="#/chat/kb/${k.id}">
    <div class="kb-top">
      <div class="kb-icon" style="background:var(--acc-dim);color:var(--acc-strong);border-color:var(--acc-line)">${ic("folder")}</div>
      <div><h4>${esc(k.name)}</h4><div class="kb-sub">${k.videoCount} 个视频 · ${esc(k.updated)}更新</div></div>
    </div>
    <div class="kb-desc">${esc(k.desc)}</div>
    <div class="kb-foot">
      <span class="stack-avatars">${thumbs}${moreN > 0 ? `<span class="more-n">+${moreN}</span>` : ""}</span>
      <span style="margin-left:auto;color:var(--tx-3);font-size:12px;display:inline-flex;align-items:center;gap:5px">进入问答 ${ic("chev-r", "ic-sm")}</span>
    </div>
  </div>`;
}

function sessionRowHTML(s) {
  const href = s.kb ? `#/chat/kb/${s.kb}` : `#/chat/v/${s.video}`;
  return `<div class="session-row" data-nav="${href}">
    ${ic("message")}
    <span class="q">${esc(s.q)}</span>
    <span class="where">${esc(s.where)}</span>
    <span class="where">${esc(s.when)}</span>
  </div>`;
}

/* ---------------- 工作台 ---------------- */
function pageDashboard(root) {
  setCrumb(["工作台"]);
  const processing = VIDEOS.filter((v) => v.status === "transcribing" || v.status === "failed");
  root.innerHTML = `
  <div class="page">
    <div class="hero-ask">
      <h1>下午好,周衍。今天想让<em>哪些视频</em>替你说话?</h1>
      <div class="sub">所有回答都带时间点引用,可以一路回溯到画面。</div>
      <div class="ask-bar">
        <textarea id="dashAsk" rows="1" placeholder="向整个知识库提问,或先选一个范围…"></textarea>
        <div class="ask-send" id="dashSend">${ic("send")}</div>
      </div>
      <div class="ask-scope">
        <span style="font-size:12px;color:var(--tx-4)">范围</span>
        <button class="chip chip-acc" id="scopePick">${ic("folder")}AI 前沿追踪</button>
        <button class="chip" data-nav="#/kb">更换范围</button>
      </div>
      <div class="suggest-row">
        <button class="suggest" data-ask="几场演讲分别提到了哪些降低推理成本的思路?">几场演讲提到了哪些降本思路?</button>
        <button class="suggest" data-nav="#/chat/v/12">看看画面证据是怎么用的</button>
      </div>
    </div>

    <div class="section-head"><h2>继续处理</h2><span class="more" data-nav="#/library">全部视频 ${ic("chev-r", "ic-sm")}</span></div>
    <div class="proc-list" style="display:grid;gap:10px">
      ${processing.map((v) => `
        <div class="proc-row" data-nav="#/video/${v.id}" style="cursor:pointer">
          <div class="proc-left">
            <h5>${esc(v.title)}</h5>
            <div class="stage">${v.status === "failed"
              ? `<span style="color:var(--bad)">${esc(v.errText)}</span>`
              : esc(v.stageText)}</div>
          </div>
          <span class="pct">${v.status === "failed" ? "!" : Math.round((v.progress || 0) * 100) + "%"}</span>
          ${v.status === "failed"
            ? `<button class="btn btn-sm" data-toast="已重新入队,消费后会跳过已完成的 5 个分片">重试</button>`
            : `<span class="chip ${v.statusChip}">${esc(v.stateText)}</span>`}
        </div>`).join("") || `<div class="empty"><b>没有进行中的任务</b><p>转写、摘要和索引都在后台排队,这里会显示它们的进度。</p></div>`}
    </div>

    <div class="section-head"><h2>最近视频</h2><span class="more" data-open-upload>${ic("plus", "ic-sm")}上传</span></div>
    <div class="video-grid">${VIDEOS.slice(0, 4).map(vcardHTML).join("")}</div>

    <div class="section-head"><h2>知识库</h2><span class="more" data-nav="#/kb">管理 ${ic("chev-r", "ic-sm")}</span></div>
    <div class="kb-grid">${KBS.map(kbCardHTML).join("")}</div>

    <div class="section-head"><h2>最近会话</h2></div>
    <div class="card card-pad" style="padding:8px">${SESSIONS.map(sessionRowHTML).join("")}</div>
  </div>`;

  const ta = $("#dashAsk");
  ta.addEventListener("keydown", (e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); dashSend(); } });
  $("#dashSend").addEventListener("click", dashSend);
  $$(".suggest[data-ask]", root).forEach((b) => b.addEventListener("click", () => { $("#dashAsk").value = b.getAttribute("data-ask"); dashSend(); }));
  $("#scopePick").addEventListener("click", () => toast("原型里默认指向「AI 前沿追踪」", "folder"));
  function dashSend() {
    const q = $("#dashAsk").value.trim();
    if (!q) { toast("先输入一个问题"); return; }
    nav(`#/chat/kb/3?q=${encodeURIComponent(q)}`);
  }
}

/* ---------------- 视频库 ---------------- */
function pageLibrary(root) {
  setCrumb(["视频库"]);
  root.innerHTML = `
  <div class="page page-wide">
    <div class="section-head" style="margin-top:0"><h2>视频库</h2>
      <span style="font-size:12.5px;color:var(--tx-3)">${VIDEOS.length} 个视频 · 3 个已可问答</span>
      <span class="more" data-open-upload>${ic("plus", "ic-sm")}上传视频</span>
    </div>
    <div class="lib-toolbar">
      <input class="input" id="libFilter" placeholder="按标题过滤…" />
      <div class="seg" id="libSeg">
        <button class="on" data-f="all">全部</button>
        <button data-f="ready">可问答</button>
        <button data-f="processing">处理中</button>
        <button data-f="failed">失败</button>
      </div>
    </div>
    <div class="video-grid" id="libGrid"></div>
  </div>`;
  let filter = "all", kw = "";
  const grid = $("#libGrid");
  function draw() {
    const list = VIDEOS.filter((v) => {
      if (kw && !v.title.toLowerCase().includes(kw)) return false;
      if (filter === "ready") return v.status === "indexed";
      if (filter === "processing") return v.status === "transcribing";
      if (filter === "failed") return v.status === "failed";
      return true;
    });
    grid.innerHTML = list.map(vcardHTML).join("") || `<div class="empty" style="grid-column:1/-1">${ic("search", "ic-lg")}<b>没有匹配的视频</b><p>换个关键词,或清空过滤器。</p></div>`;
  }
  $("#libFilter").addEventListener("input", (e) => { kw = e.target.value.trim().toLowerCase(); draw(); });
  $("#libSeg").addEventListener("click", (e) => {
    const b = e.target.closest("button"); if (!b) return;
    $$("#libSeg button").forEach((x) => x.classList.remove("on")); b.classList.add("on");
    filter = b.getAttribute("data-f"); draw();
  });
  draw();
}

/* ---------------- 知识库列表 ---------------- */
function pageKbList(root) {
  setCrumb(["知识库"]);
  const previewKb = KBS[0];
  const previewMembers = (previewKb.members || []).map((id) => videoById(id)).filter(Boolean);
  root.innerHTML = `
  <div class="page page-wide">
    <div class="section-head" style="margin-top:0"><h2>知识库</h2>
      <span style="font-size:12.5px;color:var(--tx-3)">跨视频提问,回答会注明每个片段来自哪场视频</span>
      <span class="more" id="kbNew">${ic("plus", "ic-sm")}新建知识库</span>
    </div>
    <div class="kb-grid">${KBS.map(kbCardHTML).join("")}</div>

    <div class="section-head"><h2>「${esc(previewKb.name)}」的成员视频</h2><span class="more" data-nav="#/library">在视频库查看全部 ${ic("chev-r", "ic-sm")}</span></div>
    <div class="card card-pad" style="padding:8px">
      ${previewMembers.map((v) => `
        <div class="kb-member-row" data-nav="#/video/${v.id}">
          <span class="vt">${v.demo
            ? `<img src="./assets/poster-oceans.jpg" alt="" />`
            : `<img loading="lazy" src="https://picsum.photos/seed/vidlens-${v.id}/116/68.jpg" alt="" onerror="this.remove()" />`}</span>
          <span class="nm"><b>${esc(v.title)}</b><span class="mono">${fmtDur(v.durationMs)} · ${esc(v.stateText)}</span></span>
          <button class="btn btn-sm btn-ghost" data-nav="#/chat/v/${v.id}">单独提问</button>
          <button class="btn btn-sm" data-nav="#/chat/kb/${previewKb.id}">在库内提问</button>
        </div>`).join("")}
      <div class="empty" style="padding:18px"><p>其余 ${Math.max(0, previewKb.videoCount - previewMembers.length)} 个成员视频已建索引,提问时自动纳入检索范围。</p></div>
    </div>

    <div class="section-head"><h2>知识库里的最近会话</h2></div>
    <div class="card card-pad" style="padding:8px">${SESSIONS.filter((s) => s.kb).map(sessionRowHTML).join("")}</div>
  </div>`;
  $("#kbNew").addEventListener("click", () => toast("新建知识库在原型中未开放", "folder"));
}

/* ---------------- 播放器 ---------------- */
function createDeck(stage, opts = {}) {
  const video = stage.querySelector("video");
  const fill = $(opts.fillSel || ".scrub-fill", stage.closest(".player-card") || stage);
  const cur = $(opts.curSel || ".scrub .cur", stage.closest(".player-card") || stage);
  const timeEl = $(opts.timeSel || ".ptime b", stage.closest(".player-card") || stage);
  const dur = videoById(opts.vid).durationMs;
  let t = opts.startMs || 0, playing = false, raf = null, last = 0, dead = false;
  if (video) {
    video.addEventListener("error", () => stage.classList.add("novideo"));
    setTimeout(() => { if (!dead && video.readyState === 0) stage.classList.add("novideo"); }, 8000);
  } else {
    stage.classList.add("novideo");
  }

  function paint() {
    if (fill) fill.style.width = (t / dur) * 100 + "%";
    if (cur) cur.style.left = (t / dur) * 100 + "%";
    if (timeEl) timeEl.textContent = fmtClock(t);
  }
  function loop(ts) {
    if (dead) return;
    if (playing) {
      const dt = last ? ts - last : 16; last = ts;
      t = Math.min(dur, t + dt);
      if (video && Math.abs(video.currentTime * 1000 - t) > 500) { try { video.currentTime = t / 1000; } catch (e) {} }
      if (t >= dur) { playing = false; syncIcon(); }
      paint(); opts.onTick && opts.onTick(t, playing);
    }
    raf = requestAnimationFrame(loop);
  }
  function syncIcon() {
    const b = $(".pp-btn", stage.closest(".player-card") || stage);
    if (b) b.innerHTML = ic(playing ? "pause" : "play");
    if (video) { if (playing) { video.play().catch(() => {}); } else video.pause(); }
  }
  const api = {
    seek(ms, autoplay) {
      t = Math.max(0, Math.min(dur - 800, ms));
      if (video) { try { video.currentTime = t / 1000; } catch (e) {} }
      if (autoplay && !playing) { playing = true; syncIcon(); }
      last = 0; paint(); opts.onTick && opts.onTick(t, playing);
    },
    toggle() { playing = !playing; if (playing) last = 0; syncIcon(); },
    get playing() { return playing; },
    get time() { return t; },
    video, paint, dur,
    destroy() { dead = true; if (raf) cancelAnimationFrame(raf); video && video.pause(); },
  };
  const pp = $(".pp-btn", stage.closest(".player-card") || stage);
  pp && pp.addEventListener("click", api.toggle);
  const scrub = $(".scrub", stage.closest(".player-card") || stage);
  if (scrub) {
    const seekTo = (e) => {
      const r = scrub.getBoundingClientRect();
      api.seek(((e.clientX - r.left) / r.width) * dur, api.playing);
    };
    scrub.addEventListener("pointerdown", (e) => { seekTo(e); const mv = (ev) => seekTo(ev); const up = () => { window.removeEventListener("pointermove", mv); window.removeEventListener("pointerup", up); }; window.addEventListener("pointermove", mv); window.addEventListener("pointerup", up); });
  }
  raf = requestAnimationFrame(loop);
  paint();
  return api;
}

/* 画面帧取样: 串行队列, 等待元数据就绪, 失败时画占位 */
let snapChain = Promise.resolve();
function snapFrame(deck, canvas, tMs, label) {
  snapChain = snapChain.then(async () => {
    const ctx = canvas.getContext("2d");
    const w = (canvas.width = 320), h = (canvas.height = 180);
    const video = deck && deck.video;
    if (!video) { placeholder(); return; }
    /* 元数据未就绪时先等待, 否则 currentTime 赋值不会触发 seeked */
    if (video.readyState < 1) {
      await new Promise((res) => {
        const to = setTimeout(res, 6000);
        video.addEventListener("loadedmetadata", () => { clearTimeout(to); res(); }, { once: true });
      });
    }
    if (video.readyState < 1) { placeholder(); return; }
    const ok = await new Promise((res) => {
      let settled = false;
      const done = (good) => { if (settled) return; settled = true; clearInterval(poll); clearTimeout(to); video.removeEventListener("seeked", onSeek); video.removeEventListener("error", onFail); if (!good) { res(false); return; } try { ctx.drawImage(video, 0, 0, w, h); res(true); } catch (e) { res(false); } };
      const onSeek = () => done(true);
      const onFail = () => done(false);
      const to = setTimeout(() => done(false), 5000);
      /* seeked 事件的兜底: 轮询播放头是否到位 */
      const poll = setInterval(() => {
        if (Math.abs(video.currentTime * 1000 - tMs) < 150) done(true);
      }, 120);
      video.addEventListener("seeked", onSeek);
      video.addEventListener("error", onFail);
      try { video.pause(); video.currentTime = tMs / 1000; } catch (e) { done(false); }
    });
    /* 取样完把播放头还原到当前时间, 避免画面与时间码对不上 */
    if (ok && deck && typeof deck.time === "number") {
      try { video.currentTime = deck.time / 1000; } catch (e) {}
    }
    if (!ok) placeholder();
    function placeholder() {
      const g = ctx.createLinearGradient(0, 0, w, h);
      g.addColorStop(0, "#241f18"); g.addColorStop(1, "#171310");
      ctx.fillStyle = g; ctx.fillRect(0, 0, w, h);
      ctx.fillStyle = "rgba(226,168,75,.85)"; ctx.font = "600 13px 'JetBrains Mono', monospace";
      ctx.fillText(fmtClock(tMs), 14, 26);
      ctx.fillStyle = "rgba(237,232,222,.5)"; ctx.font = "11px sans-serif";
      ctx.fillText("帧预览需要网络加载视频源", 14, 48);
    }
  });
}

/* ---------------- 视频工作台 ---------------- */
function pageVideo(root, id) {
  const v = videoById(id);
  if (!v) { nav("#/library"); return; }
  setCrumb(["视频库", v.title]);
  const canPlay = !!v.demo;
  root.innerHTML = `
  <div class="page page-wide">
    <div class="ws">
      <div>
        <div class="player-card">
          <div class="player-stage" id="stage">
            ${canPlay ? `<video src="${DEMO_VIDEO_URL}" playsinline preload="metadata"></video>` : ""}
            <div class="player-fallback">
              <div style="position:absolute;inset:0;display:grid;place-items:center;text-align:center;color:var(--tx-3)">
                <div>${ic("video", "ic-lg")}<div style="margin-top:8px;font-size:12.5px">演示原型只内置《海洋》素材的播放源</div></div>
              </div>
            </div>
            <div class="player-hud mono" id="hud">00:00 · ${esc(v.title.slice(0, 18))}</div>
          </div>
          <div class="player-controls">
            <button class="pp-btn">${ic("play")}</button>
            <div class="scrub"><div class="scrub-track"><div class="scrub-fill" style="width:0%"></div></div><div class="cur" style="left:0%"></div></div>
            <div class="ptime mono"><b>00:00</b> <span>/ ${fmtDur(v.durationMs)}</span></div>
          </div>
        </div>
        <div class="ws-actions">
          ${v.hasSummary ? `<button class="btn" id="actSummary">${ic("file", "ic-sm")}查看摘要</button>` : `<button class="btn" id="actSummary">${ic("wand", "ic-sm")}生成摘要</button>`}
          ${v.hasTranscript ? `<button class="btn">${ic("refresh", "ic-sm")}重新转写</button>` : `<button class="btn" disabled>${ic("activity", "ic-sm")}等待转写</button>`}
          ${v.status === "indexed" ? `<button class="btn">${ic("layers", "ic-sm")}重建索引</button>` : `<button class="btn" id="actIndex">${ic("layers", "ic-sm")}建立索引</button>`}
          <button class="btn btn-ghost">${ic("download", "ic-sm")}下载音频</button>
          <span style="flex:1"></span>
          <button class="btn btn-primary" data-nav="#/chat/v/${v.id}">${ic("message", "ic-sm")}进入问答</button>
        </div>
        ${v.status === "failed" ? `
        <div class="card card-pad" style="margin-top:14px;border-color:rgba(224,131,115,.35)">
          <div style="display:flex;gap:10px;align-items:flex-start">
            <span style="color:var(--bad)">${ic("alert")}</span>
            <div style="flex:1">
              <b style="font-size:13px">转写失败,已停在分片 6</b>
              <p style="font-size:12px;color:var(--tx-3);margin-top:4px">${esc(v.errText)}。已完成的 5 个分片不会重复调用,重试只补失败片。</p>
              <button class="btn btn-sm" style="margin-top:10px" data-toast="已重新入队,消费后会跳过已完成的 5 个分片">重试转写</button>
            </div>
          </div>
        </div>` : ""}
        <div class="summary-block" id="summaryBlock">
          ${v.hasSummary ? `
          <div class="card card-pad">
            <h3>${ic("file", "ic-sm")}AI 摘要<span style="margin-left:auto;font-size:11px;color:var(--tx-4);font-weight:500">glm-4.6 · 2 小时前</span></h3>
            <div class="summary-body">${summaryHTML(SUMMARY12)}</div>
          </div>` : `
          <div class="empty card">
            ${ic("wand", "ic-lg")}<b>还没有摘要</b><p>摘要由 LLM 基于转写生成,生成后可以在这里直接阅读。</p>
            <button class="btn btn-sm" data-toast="已加入处理队列,完成后会出现在这里">生成摘要</button>
          </div>`}
        </div>
      </div>

      <div class="card" style="overflow:hidden">
        <div class="rail-tabs" style="padding:10px 14px 0">
          <button class="rail-tab on" data-tab="tl">转写时间轴</button>
          <button class="rail-tab" data-tab="vf">画面证据</button>
          <button class="rail-tab" data-tab="idx">检索索引</button>
        </div>
        <div class="rail-body" id="wsTabBody" style="padding:16px 18px 26px"></div>
      </div>
    </div>
  </div>`;

  currentDeck = canPlay ? createDeck($("#stage"), { vid: v.id }) : null;
  $("#hud").textContent = "00:00 / " + fmtDur(v.durationMs);
  const hudBase = v.title.replace(" (示例素材)", "");
  if (currentDeck) {
    const paint = () => { $("#hud").textContent = fmtClock(currentDeck.time) + " · " + hudBase; };
    setInterval(() => { if (currentDeck) paint(); }, 500);
  }

  /* Tabs */
  const body = $("#wsTabBody");
  function tab(name) {
    $$(".rail-tab", root).forEach((b) => b.classList.toggle("on", b.getAttribute("data-tab") === name));
    if (name === "tl") renderTL(); else if (name === "vf") renderVF(); else renderIDX();
  }
  $$(".rail-tab", root).forEach((b) => b.addEventListener("click", () => tab(b.getAttribute("data-tab"))));

  function renderTL() {
    if (!v.hasTranscript) {
      body.innerHTML = `<div class="empty">${ic("activity", "ic-lg")}<b>转写完成后就能看到时间轴</b><p>时间轴会把解说与画面帧排在同一条可回放的时间线上。</p></div>`;
      return;
    }
    const segs = TRANSCRIPT12.map((r) => `<div class="tl-seg t-transcript" style="left:${(r.startMs / v.durationMs) * 100}%;width:${((r.endMs - r.startMs) / v.durationMs) * 100}%"></div>`).join("")
      + FRAMES12.map((f) => `<div class="tl-seg ${f.ocr ? "t-ocr" : "t-caption"}" style="left:${(f.timeMs / v.durationMs) * 100}%;width:max(6px, ${((f.endMs - f.timeMs) / v.durationMs) * 100}%)" title="${esc(f.caption)}"></div>`).join("");
    body.innerHTML = `
      <div class="tl-rail" id="tlRail">${segs}<div class="tl-head" id="tlHead" style="left:0%"></div></div>
      <div class="tl-scale mono"><span>00:00</span><span>00:10</span><span>00:20</span><span>00:30</span><span>00:40</span><span>${fmtDur(v.durationMs)}</span></div>
      <div class="tl-legend">
        <span><i style="background:rgba(154,145,127,.5)"></i>解说转写</span>
        <span><i style="background:rgba(226,168,75,.6)"></i>画面 OCR</span>
        <span><i style="background:rgba(134,180,201,.55)"></i>画面描述</span>
        <span style="margin-left:auto;color:var(--tx-4)">点击任意位置跳转</span>
      </div>
      <div class="transcript-list" id="tlList">
        ${TRANSCRIPT12.map((r, i) => `
          <div class="t-row" data-i="${i}" data-t="${r.startMs}">
            <span class="ts">${fmtClock(r.startMs)}</span>
            <span class="tx">${esc(r.text)}</span>
          </div>`).join("")}
      </div>`;
    const rail = $("#tlRail");
    rail.addEventListener("pointerdown", (e) => {
      const r = rail.getBoundingClientRect();
      if (currentDeck) currentDeck.seek(((e.clientX - r.left) / r.width) * v.durationMs, true);
      else toast("演示原型只内置《海洋》素材的播放源", "info");
    });
    $$("#tlList .t-row").forEach((row) => row.addEventListener("click", () => {
      if (currentDeck) currentDeck.seek(Number(row.getAttribute("data-t")), true);
      else toast("演示原型只内置《海洋》素材的播放源", "info");
    }));
    markLive();
  }
  function markLive() {
    if (!currentDeck) return;
    const t = currentDeck.time;
    const head = $("#tlHead"); if (head) head.style.left = (t / v.durationMs) * 100 + "%";
    let live = -1;
    TRANSCRIPT12.forEach((r, i) => { if (t >= r.startMs && t < r.endMs) live = i; });
    $$("#tlList .t-row").forEach((row, i) => row.classList.toggle("live", i === live));
  }
  if (currentDeck) setInterval(() => { if (currentDeck) markLive(); }, 400);

  function renderVF() {
    if (!v.hasVisual) {
      body.innerHTML = `<div class="empty">${ic("eye", "ic-lg")}<b>没有画面索引</b><p>视觉分支会与转写并行:关键帧 OCR 与画面描述分开入索引,失败不影响文本问答。</p></div>`;
      return;
    }
    body.innerHTML = `
      <p style="font-size:12px;color:var(--tx-3);margin-bottom:12px">关键帧经感知哈希去重后,OCR 与画面描述分别入索引。下面 ${FRAMES12.length} 帧都带稳定时间戳,可直接回放。</p>
      <div class="frames-grid">
        ${FRAMES12.map((f) => `
          <div class="frame-card">
            <canvas data-t="${f.timeMs}"></canvas>
            <div class="fc-body">
              <div class="row"><span class="fc-time mono" data-jump="${f.timeMs}">${fmtClock(f.timeMs)}</span>${f.ocr ? modalityTag("visual_ocr") : modalityTag("visual_caption")}</div>
              ${f.ocr ? `<div class="fc-text mono" style="font-size:10.5px;letter-spacing:.02em">${esc(f.ocr)}</div>` : ""}
              <div class="fc-text" ${f.ocr ? 'style="margin-top:5px"' : ""}>${esc(f.caption)}</div>
            </div>
          </div>`).join("")}
      </div>`;
    $$("canvas[data-t]", body).forEach((c) => snapFrame(currentDeck, c, Number(c.getAttribute("data-t"))));
    $$("[data-jump]", body).forEach((b) => b.addEventListener("click", () => {
      if (currentDeck) currentDeck.seek(Number(b.getAttribute("data-jump")), true);
      else toast("演示原型只内置《海洋》素材的播放源", "info");
    }));
  }
  function renderIDX() {
    const st = v.status === "indexed" ? INDEX12 : null;
    if (!st) {
      body.innerHTML = `<div class="empty">${ic("layers", "ic-lg")}<b>还没有检索索引</b><p>索引把转写与画面切片成带时间范围的证据块,是问答的检索来源。</p>
        <button class="btn btn-sm" data-toast="已开始构建索引,可重建投影而不重做转写">建立索引</button></div>`;
      return;
    }
    body.innerHTML = `
      <div class="idx-list">
        <div class="idx-row"><span class="k">状态</span><span class="v"><span class="chip chip-ok">${esc(st.stateText)}</span></span></div>
        <div class="idx-row"><span class="k">构建版本</span><span class="v mono">${esc(st.build)} · ${esc(st.sourceMap)}</span></div>
        <div class="idx-row"><span class="k">切片器</span><span class="v mono" style="font-size:11.5px">${esc(st.chunker)}</span></div>
        <div class="idx-row"><span class="k">证据块</span><span class="v mono">${st.chunks} 块</span></div>
        <div class="idx-row"><span class="k">向量模型</span><span class="v mono">${esc(st.model)} · ${st.dims} 维</span></div>
      </div>
      <div class="field-label" style="margin-top:16px">按模态</div>
      ${st.modalities.map((m) => `
        <div style="display:flex;align-items:center;gap:10px;margin-bottom:8px">
          <span style="width:110px">${modalityTag(m.k)}</span>
          <span class="mono" style="font-size:11px;color:var(--tx-3);width:56px">${m.n} 块</span>
          <span style="flex:1;height:3px;border-radius:99px;background:var(--bg-3);position:relative"><i style="position:absolute;left:0;top:0;bottom:0;width:${m.pct}%;border-radius:99px;background:var(--acc);opacity:.75"></i></span>
        </div>`).join("")}
      <p style="font-size:11.5px;color:var(--tx-4);margin-top:10px">旧版本索引会显示 needs_rebuild,重建只重做投影,不重做转写。</p>`;
  }
  tab("tl");

  $("#actSummary") && $("#actSummary").addEventListener("click", () => toast("摘要已加入队列 (演示)", "wand"));
  $("#actIndex") && $("#actIndex").addEventListener("click", () => toast("索引已开始构建 (演示)", "layers"));
}

function summaryHTML(md) {
  const lines = md.split("\n");
  let html = "", inList = false;
  const inline = (s) => esc(s).replace(/\*\*(.+?)\*\*/g, "<b>$1</b>");
  for (const ln of lines) {
    if (ln.startsWith("- ")) { if (!inList) { html += "<ul>"; inList = true; } html += `<li>${inline(ln.slice(2))}</li>`; continue; }
    if (inList) { html += "</ul>"; inList = false; }
    if (ln.trim() === "") continue;
    html += `<p>${inline(ln)}</p>`;
  }
  if (inList) html += "</ul>";
  return html;
}

/* ---------------- 问答页 ---------------- */
function pageChat(root, scope) {
  const isVideo = scope.type === "video";
  const v = isVideo ? videoById(scope.id) : null;
  const kb = isVideo ? null : kbById(scope.id);
  if (isVideo && !v) { nav("#/library"); return; }
  if (!isVideo && !kb) { nav("#/kb"); return; }
  const scopeName = isVideo ? v.title : kb.name;
  setCrumb([isVideo ? "视频库" : "知识库", scopeName, "问答"]);
  chatState = { scope, isVideo, v, kb, messages: [], mode: isVideo ? "agent" : "strict", runNo: 0 };

  const suggestions = isVideo ? (v.demo ? SUGGEST_VIDEO : SUGGEST_VIDEO_GENERIC) : SUGGEST_KB;

  root.innerHTML = `
  <div class="chat-wrap">
    <div class="chat-col">
      <div class="chat-scroll" id="chatScroll"><div class="chat-inner" id="chatInner"></div></div>
      <div class="composer">
        <div class="composer-inner">
          <div class="mode-row">
            <button class="mode-pill ${chatState.mode === "strict" ? "on" : ""}" data-mode="strict">${ic("bolt", "ic-sm")}快速问答</button>
            ${isVideo ? `
            <button class="mode-pill ${chatState.mode === "agent" ? "on" : ""}" data-mode="agent">${ic("target", "ic-sm")}Agent 检证</button>
            <button class="mode-pill" data-mode="research">${ic("zoom-scan", "ic-sm")}深入研究<span class="chip chip-mute" style="height:18px;font-size:10px;padding:0 6px;margin-left:2px">实验</span></button>
            <button class="mode-pill" data-mode="funnel">${ic("filter", "ic-sm")}证据漏斗<span class="chip chip-mute" style="height:18px;font-size:10px;padding:0 6px;margin-left:2px">实验</span></button>` : `
            <button class="mode-pill" data-mode="agent" disabled title="知识库范围的 Agent 尚未开放">${ic("target", "ic-sm")}Agent 检证</button>
            <button class="mode-pill" data-mode="research" disabled title="研究模式仅支持单视频会话">${ic("zoom-scan", "ic-sm")}深入研究</button>`}
            <span class="mode-note" id="modeNote"></span>
          </div>
          <div class="ask-bar" style="margin-top:0">
            <textarea id="chatInput" rows="1" placeholder="${isVideo ? "问这段视频…回答会标注口述还是画面" : "向整个知识库提问…回答会注明每个片段来自哪场视频"}"></textarea>
            <div class="ask-send" id="chatSend">${ic("send")}</div>
          </div>
          <div class="chat-status" id="chatStatus"></div>
          <div class="suggest-row" style="margin-top:2px" id="chatSuggests">
            ${suggestions.map((s) => `<button class="suggest" data-s="${esc(s.text)}" ${s.mode ? `data-mode="${s.mode}"` : ""}>${esc(s.text)}</button>`).join("")}
          </div>
        </div>
      </div>
    </div>

    <aside class="rail-panel">
      ${isVideo && v.demo ? `
      <div class="mini-player player-card" style="margin:14px 14px 0">
        <div class="player-stage" id="miniStage">
          <video src="${DEMO_VIDEO_URL}" playsinline preload="metadata"></video>
          <div class="player-hud mono" id="miniHud">00:00 · ${esc(v.title.replace(" (示例素材)", ""))}</div>
        </div>
        <div class="player-controls" style="padding:9px 12px">
          <button class="pp-btn" style="width:32px;height:32px">${ic("play")}</button>
          <div class="scrub"><div class="scrub-track"><div class="scrub-fill" style="width:0%"></div></div><div class="cur" style="left:0%"></div></div>
          <div class="ptime mono"><b>00:00</b> <span>/ ${fmtDur(v.durationMs)}</span></div>
        </div>
      </div>` : ""}
      <div class="rail-tabs">
        <button class="rail-tab on" data-rt="run">执行过程</button>
        <button class="rail-tab" data-rt="ev">证据账本<span class="badge" id="evBadge">0</span></button>
      </div>
      <div class="rail-body" id="railBody"></div>
    </aside>
  </div>`;

  /* 迷你播放器: 引用跳转在这里寻址 */
  if (isVideo && v.demo) {
    currentDeck = createDeck($("#miniStage"), { vid: v.id });
    const miniBase = v.title.replace(" (示例素材)", "");
    setInterval(() => {
      if (currentDeck && $("#miniHud")) $("#miniHud").textContent = fmtClock(currentDeck.time) + " · " + miniBase;
    }, 500);
  }

  /* 历史会话 (演示预置一轮) */
  if (isVideo && v.demo) {
    pushUserMsg("这段素材里的鱼球是怎么回事?", false);
    const histSc = SCEN_OCEAN_CHAIN;
    pushAgentMsg("解说轨把它解释成沙丁鱼的防御行为:聚成致密的鱼球,让捕食者难以锁定单条目标;但鱼球也会引来更多捕食者,鸟群和鲨鱼都是冲着它来的。",
      [histSc.citations[1], histSc.citations[2]],
      { mode: "strict", ms: 3.2, steps: 2, history: true });
  }
  if (!isVideo && kb.id === 3) {
    pushUserMsg("发布会里提到的新芯片能效提升是多少?", false);
    pushAgentMsg("GTC 2026 主题演讲的已完成分片里提到,新芯片在 FP4 量化与 KV 缓存复用下的能效提升约为前代的两倍,但这段转写还在进行中,完整数字建议等索引完成后再次确认。",
      [SCEN_KB_COST.citations[0]], { mode: "strict", ms: 2.1, steps: 1, history: true });
  }
  if (chatState.messages.length === 0) {
    $("#chatInner").innerHTML = `
      <div class="chat-empty">
        <div class="hello"><div class="brand-mark"></div><h2>${isVideo ? "问这段视频" : "问「" + esc(kb.name) + "」"}</h2></div>
        <p>每个回答都带可回放的时间点引用。点下面的问题,或直接输入。</p>
      </div>`;
  }
  updateModeNote();
  bindChat();
  renderRailEmpty();

  /* 演示参数: ?q= 自动提问; ?ev=C4 打开某条引用的证据详情; ?ledger=1 跑完后打开证据账本 */
  const demoParams = new URLSearchParams(location.hash.split("?")[1] || "");
  const qparam = demoParams.get("q");
  if (qparam) {
    setTimeout(async () => {
      /* 与点击建议问题一致: 同步它自带的模式 */
      const hit = (chatState.isVideo ? SUGGEST_VIDEO : SUGGEST_KB).find((s) => s.text === qparam);
      if (hit && hit.mode) {
        chatState.mode = hit.mode;
        $$(".mode-pill[data-mode]", root).forEach((x) => x.classList.toggle("on", x.getAttribute("data-mode") === hit.mode));
        updateModeNote();
      }
      const i = $("#chatInput"); i.value = qparam;
      await send();
      const last = [...chatState.messages].reverse().find((m) => m.__claims);
      if (demoParams.get("ledger") && last) openLedgerDrawer(last.__claims, last.__cites, { mode: chatState.mode, runId: "run-" + rid() });
    }, 600);
  } else if (demoParams.get("ev")) {
    setTimeout(() => {
      const no = Number(demoParams.get("ev"));
      const last = [...chatState.messages].reverse().find((m) => m.__cites);
      const cit = last && last.__cites.find((x) => x.c === no);
      if (cit) openEvidenceDrawer(cit, last.__cites);
    }, 400);
  }

  function updateModeNote() {
    const note = {
      strict: "一次检索,直接给出带引用的回答",
      agent: "检索后生成回答,答案经独立证据核验",
      research: "受限 Planner 循环,可做查询时像素核验",
      funnel: "固定八步漏斗,逐步收窄证据范围",
    }[chatState.mode];
    $("#modeNote").textContent = note || "";
  }

  function bindChat() {
    const input = $("#chatInput");
    input.addEventListener("keydown", (e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send(); } });
    $("#chatSend").addEventListener("click", send);
    $$(".mode-pill[data-mode]", root).forEach((b) => b.addEventListener("click", () => {
      if (b.disabled) {
        toast(b.getAttribute("data-mode") === "research"
          ? "研究模式当前仅支持单视频会话,知识库范围会直接被拒绝"
          : "知识库范围的 Agent 尚未开放,当前只能用快速问答", "info");
        return;
      }
      chatState.mode = b.getAttribute("data-mode");
      $$("[data-mode]", root).forEach((x) => x.classList.toggle("on", x === b));
      updateModeNote();
    }));
    $$("#chatSuggests .suggest").forEach((b) => b.addEventListener("click", () => {
      const m = b.getAttribute("data-mode");
      if (m && chatState.mode !== m) {
        chatState.mode = m;
        $$("[data-mode].mode-pill", root).forEach((x) => x.classList.toggle("on", x.getAttribute("data-mode") === m));
        updateModeNote();
      }
      input.value = b.getAttribute("data-s"); send();
    }));
    $$(".rail-tab", root).forEach((b) => b.addEventListener("click", () => {
      $$(".rail-tab", root).forEach((x) => x.classList.toggle("on", x === b));
      const rt = b.getAttribute("data-rt");
      if (rt === "run") renderRailRun(); else renderRailEv();
    }));
  }

  /* ---- 消息渲染 ---- */
  function pushUserMsg(text, scroll = true) {
    chatState.messages.push({ role: "user", text });
    const el = document.createElement("div");
    el.className = "msg msg-user";
    el.innerHTML = `<div class="bubble">${esc(text)}</div>`;
    $("#chatInner").appendChild(el);
    if (scroll) scrollBottom();
  }

  function pushAgentMsg(answerTokens, citations, meta) {
    chatState.messages.push({ role: "agent" });
    const el = document.createElement("div");
    el.className = "msg msg-agent";
    el.innerHTML = `
      <div class="who"><span class="agent-mark">${ic("bolt")}</span>映知${meta.history ? " · 历史" : ""}<span style="color:var(--tx-4)">${meta.mode === "strict" ? "快速问答" : meta.mode === "funnel" ? "证据漏斗" : meta.mode === "research" ? "深入研究" : "Agent 检证"}</span></div>
      <div class="answer" data-ans></div>
      <div class="cite-list" data-cites></div>
      <div class="answer-meta" data-meta></div>`;
    $("#chatInner").appendChild(el);
    renderAnswerInto($("[data-ans]", el), answerTokens, true);
    renderCites($("[data-cites]", el), citations, meta.history);
    const m = $("[data-meta]", el);
    m.innerHTML = `
      ${meta.blocked ? `<span class="chip chip-warn">${ic("shield", "ic-sm")}核验未通过,阻断发布</span>` : ""}
      ${meta.claims ? `<button class="meta-link acc" data-ledger>${ic("shield-check", "ic-sm")}证据账本 · ${meta.claims.length} 条 claim</button>` : ""}
      <span class="chip chip-mute">${meta.mode === "strict" ? "strict_rag" : meta.mode === "funnel" ? "evidence_funnel" : meta.mode === "research" ? "research" : "agent"}</span>
      <span class="chip chip-mute mono">${meta.steps} 步 · ${meta.ms}s</span>
      <button class="meta-link" data-copy>${ic("file", "ic-sm")}复制回答</button>`;
    if (meta.claims) $("[data-ledger]", m).addEventListener("click", () => openLedgerDrawer(meta.claims, citations, meta));
    $("[data-copy]", m).addEventListener("click", () => { navigator.clipboard && navigator.clipboard.writeText(answerTokens.filter((t) => typeof t === "string").join("")); toast("回答已复制"); });
    scrollBottom();
    return el;
  }

  function renderAnswerInto(box, tokens, withChips) {
    box.innerHTML = "";
    let p = document.createElement("p");
    box.appendChild(p);
    for (const tk of tokens) {
      if (typeof tk === "string") {
        tk.split("\n").forEach((seg, i) => {
          if (i > 0) { p = document.createElement("p"); box.appendChild(p); }
          if (seg) p.appendChild(document.createTextNode(seg));
        });
      } else if (withChips && tk.c) {
        const c = document.createElement("span");
        c.className = "cite"; c.textContent = tk.c; c.setAttribute("data-c", tk.c);
        p.appendChild(c);
      }
    }
    if (withChips) bindCiteChips(box);
  }

  function bindCiteChips(box) {
    $$(".cite", box).forEach((c) => c.addEventListener("click", () => {
      const no = Number(c.getAttribute("data-c"));
      const msg = c.closest(".msg");
      const cites = msg.__cites || [];
      const cit = cites.find((x) => x.c === no);
      if (cit) openEvidenceDrawer(cit, cites);
    }));
  }

  function renderCites(box, citations, isHistory) {
    box.__cites = citations;
    const msg = box.closest(".msg"); if (msg) msg.__cites = citations;
    box.innerHTML = citations.map((c, i) => {
      const vid = videoById(c.task);
      const title = c.videoTitle || (vid ? vid.title : "未知视频");
      const canJump = vid && vid.demo;
      return `
      <div class="cite-card" data-c="${c.c}" style="animation-delay:${Math.min(i * 60, 300)}ms">
        <span class="cno">${c.c}</span>
        <div class="cbody">
          <div class="chead">
            <span class="cvideo">${esc(title)}</span>
            <span class="ctime mono">${fmtClock(c.startMs)} - ${fmtClock(c.endMs)}</span>
            ${modalityTag(c.modality)}
            ${c.status === "coarse" ? `<span class="chip chip-mute" style="height:20px;font-size:10px">粗粒度时间</span>` : ""}
          </div>
          <div class="cquote">${esc(c.quote)}</div>
        </div>
        <button class="btn btn-sm cjump ${canJump ? "" : ""}" data-jump="${c.startMs}" data-can="${canJump ? 1 : 0}">${ic("play", "ic-sm")}${canJump ? "回放" : "查看"}</button>
      </div>`;
    }).join("");
    $$(".cite-card", box).forEach((card) => card.addEventListener("click", (e) => {
      if (e.target.closest("[data-jump]")) return;
      const cit = citations.find((x) => x.c === Number(card.getAttribute("data-c")));
      if (cit) openEvidenceDrawer(cit, citations);
    }));
    $$("[data-jump]", box).forEach((b) => b.addEventListener("click", (e) => {
      e.stopPropagation();
      const can = b.getAttribute("data-can") === "1";
      const cit = citations.find((x) => x.c === Number(b.closest(".cite-card").getAttribute("data-c")));
      if (!can) { openEvidenceDrawer(cit, citations); return; }
      jumpToCitation(cit);
      flashCite(b.closest(".msg"), cit.c);
    }));
  }

  function scrollBottom() {
    const sc = $("#chatScroll");
    sc.scrollTop = sc.scrollHeight;
  }

  /* ---- 引用跳转 ---- */
  function jumpToCitation(cit) {
    const vid = videoById(cit.task);
    if (!vid || !vid.demo) { toast("演示原型只内置《海洋》素材的播放源", "info"); return; }
    if (chatState.isVideo && currentDeck) {
      currentDeck.seek(cit.startMs, true);
      highlightTranscript(cit.startMs);
    } else {
      toast(`跳转 ${fmtClock(cit.startMs)} · ${vid.title}`, "play");
    }
  }
  function flashCite(msgEl, no) {
    const chip = $(`.cite[data-c="${no}"]`, msgEl);
    if (chip) { chip.classList.remove("flash"); void chip.offsetWidth; chip.classList.add("flash"); }
  }
  function highlightTranscript(t) {
    const row = $$("#wsTabBody .t-row").find((r) => Number(r.getAttribute("data-t")) <= t)
      || $$("#wsTabBody .t-row")[0];
    if (!row) return;
    $$("#wsTabBody .t-row.live").forEach((r) => r.classList.remove("live"));
    row.classList.add("live");
    row.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }

  /* ---- 右栏: 执行过程 ---- */
  function renderRailEmpty() {
    $("#railBody").innerHTML = `
      <div class="rail-empty" style="padding-top:44px">
        ${ic("target", "ic-lg")}
        <p style="margin-top:10px">发起一次提问后,<br/>每一步检索与核对都会出现在这里。</p>
      </div>`;
  }
  function renderRailRun() { /* 当前 run 的 DOM 保留在 railBody 中, 无需重建 */ }
  function renderRailEv() {
    const last = [...chatState.messages].reverse().find((m) => m.__claims);
    const claims = last && last.__claims;
    const cites = last && last.__cites;
    if (!claims) {
      $("#railBody").innerHTML = `<div class="rail-empty" style="padding-top:44px">${ic("shield-check", "ic-lg")}<p style="margin-top:10px">完成一次 Agent 或漏斗问答后,<br/>这里的账本会列出每条事实的支撑情况。</p></div>`;
      return;
    }
    $("#railBody").innerHTML = claimsHTML(claims, cites);
    bindClaimCards($("#railBody"), cites);
  }

  /* ---- 发送与运行 ---- */
  let running = false;
  function setStatus(html) { $("#chatStatus").innerHTML = html; }

  async function send() {
    const input = $("#chatInput");
    const q = input.value.trim();
    if (!q || running) { if (!q) toast("先输入一个问题"); return; }
    running = true;
    input.value = ""; input.style.height = "auto";
    pushUserMsg(q);
    const empty = $(".chat-empty"); if (empty) empty.remove();

    const mode = chatState.mode;
    const sc = pickScenario(q, chatState.isVideo ? SUGGEST_VIDEO : SUGGEST_KB)
      || (chatState.isVideo ? genericVideoAnswer(q, chatState.v.id) : genericKbAnswer(q));

    if (mode === "strict") {
      setStatus(`<span class="pulse"></span><span>检索中,稍等…</span>`);
      await runStrict(q, sc);
    } else if (mode === "agent") {
      await runAgent(q, sc);
    } else if (mode === "research") {
      await runResearch(q, sc);
    } else {
      await runFunnel(q, sc);
    }
    running = false;
  }

  async function runStrict(q, sc) {
    const t0 = performance.now();
    // 右栏: 简化过程。真实 SSE 只发 answer/citations/done, 此面板为前端推断 (与现网行为一致)
    const rail = $("#railBody");
    rail.innerHTML = `
      <div class="run-meta">
        <span class="chip chip-mute mono">strict_rag</span>
        <span class="rid mono">ret-${rid()}</span>
      </div>
      <p style="font-size:11px;color:var(--tx-4);margin-bottom:10px">实际流事件只有 answer / citations / done,以下检索过程由前端推断展示。</p>
      <div class="steps" id="runSteps"></div>`;
    const stepsEl = $("#runSteps");
    for (const st of sc.steps) {
      const stepEl = addStepEl(stepsEl, st.label);
      await sleep(st.ms * 0.6);
      if (st.hits) { $(".step-body", stepEl).innerHTML += hitsHTML(st.hits); scrollBottom(); }
      finishStepEl(stepEl, st.ms);
      await sleep(st.ms * 0.4);
    }
    const ms = ((performance.now() - t0) / 1000).toFixed(1);
    setStatus(`<span class="pulse"></span><span>正在生成回答…</span>`);
    const el = pushAgentMsg([], sc.citations, { mode: "strict", ms, steps: sc.steps.length });
    await streamAnswer($("[data-ans]", el), sc.answer);
    const lastMsg = chatState.messages[chatState.messages.length - 1];
    lastMsg.__claims = null; lastMsg.__cites = sc.citations;
    $("#evBadge").textContent = "0";
    setStatus(`<span style="color:var(--ok)">${ic("check", "ic-sm")}</span><span>已完成 · ${ms}s</span>`);
    // 证据 tab 若激活则刷新
    if ($('.rail-tab[data-rt="ev"]').classList.contains("on")) renderRailEv();
  }

  async function runAgent(q, sc) {
    const t0 = performance.now();
    chatState.runNo++;
    const runId = "run-" + rid();
    const rail = $("#railBody");
    rail.innerHTML = `
      <div class="run-meta">
        <span class="chip chip-acc">${ic("target", "ic-sm")}Agent 检证</span>
        <span class="rid mono">${runId}</span>
      </div>
      <p style="font-size:11px;color:var(--tx-4);margin-bottom:10px">模板 direct_qa:先检索,再构建引用回答;保存后由独立核验决定是否发布。</p>
      <div class="steps" id="runSteps"></div>`;
    const stepsEl = $("#runSteps");
    const addStep = (label) => addStepEl(stepsEl, label);

    for (const st of sc.steps) {
      const s = addStep(st.label);
      await sleep(st.ms * 0.55);
      if (st.hits) { $(".step-body", s).innerHTML += hitsHTML(st.hits); scrollBottom(); }
      else { $(".step-body", s).innerHTML += toolHTML(st); }
      if (st.frames && chatState.isVideo && chatState.v.demo) {
        $(".step-body", s).innerHTML += frameStripHTML(st);
      }
      finishStepEl(s, st.ms);
      await sleep(st.ms * 0.45);
    }

    const genStep = addStep("构建引用回答 (build_cited_answer)");
    setStatus(`<span class="pulse"></span><span>正在生成回答并等待独立核验…</span>`);
    const ms = ((performance.now() - t0) / 1000).toFixed(1);
    const el = pushAgentMsg([], sc.citations, { mode: "agent", ms, steps: sc.steps.length + 1, claims: sc.claims, runId, blocked: sc.blocked });
    await streamAnswer($("[data-ans]", el), sc.answer);
    finishStepEl(genStep, 0);
    const lastMsg = chatState.messages[chatState.messages.length - 1];
    lastMsg.__claims = sc.claims; lastMsg.__cites = sc.citations;
    $("#evBadge").textContent = (sc.claims || []).length;
    setStatus(`<span style="color:var(--ok)">${ic("check", "ic-sm")}</span><span>已完成 · ${sc.steps.length + 1} 步 · ${ms}s</span>`);
    if ($('.rail-tab[data-rt="ev"]').classList.contains("on")) renderRailEv();
    scrollBottom();
  }

  /* 深入研究: 受限 Planner 循环 (MaxSteps 8 / MaxReplans 2, 可断点恢复), 演示查询时像素核验与发布阻断 */
  async function runResearch(q, sc) {
    const t0 = performance.now();
    const runId = "research-" + rid();
    const rail = $("#railBody");
    rail.innerHTML = `
      <div class="run-meta">
        <span class="chip chip-acc">${ic("zoom-scan", "ic-sm")}深入研究</span>
        <span class="chip chip-mute mono">MaxSteps 8 · MaxReplans 2</span>
        <span class="rid mono">${runId}</span>
      </div>
      <p style="font-size:11px;color:var(--tx-4);margin-bottom:10px">Planner 只能从工具白名单中选择动作;investigate_visual 会在已定位的时间窗内按硬预算读取少量原始帧。</p>
      <div class="steps" id="runSteps"></div>`;
    const stepsEl = $("#runSteps");

    for (const st of sc.steps) {
      const s = addStepEl(stepsEl, st.label);
      await sleep(st.ms * 0.55);
      if (st.hits) { $(".step-body", s).innerHTML += hitsHTML(st.hits); scrollBottom(); }
      else { $(".step-body", s).innerHTML += toolHTML(st); }
      if (st.frames && chatState.isVideo && chatState.v.demo) {
        $(".step-body", s).innerHTML += frameStripHTML(st);
      }
      finishStepEl(s, st.ms);
      await sleep(st.ms * 0.45);
    }

    const ms = ((performance.now() - t0) / 1000).toFixed(1);
    setStatus(`<span class="pulse"></span><span>正在等待独立核验…</span>`);
    const el = pushAgentMsg([], sc.citations, { mode: "research", ms, steps: sc.steps.length, claims: sc.claims, runId, blocked: sc.blocked });
    await streamAnswer($("[data-ans]", el), sc.answer);
    const lastMsg = chatState.messages[chatState.messages.length - 1];
    lastMsg.__claims = sc.claims; lastMsg.__cites = sc.citations;
    $("#evBadge").textContent = (sc.claims || []).length;
    if (sc.blocked) {
      setStatus(`<span style="color:var(--warn)">${ic("shield", "ic-sm")}</span><span>核验未通过,回答被阻断发布</span>`);
    } else {
      setStatus(`<span style="color:var(--ok)">${ic("check", "ic-sm")}</span><span>已完成 · ${sc.steps.length} 步 · ${ms}s</span>`);
    }
    if ($('.rail-tab[data-rt="ev"]').classList.contains("on")) renderRailEv();
    scrollBottom();
  }

  async function runFunnel(q, sc) {
    const t0 = performance.now();
    const runId = "funnel-" + rid();
    const rail = $("#railBody");
    rail.innerHTML = `
      <div class="run-meta">
        <span class="chip chip-acc">${ic("filter", "ic-sm")}证据漏斗</span>
        <span class="chip chip-mute">固定八步</span>
        <span class="rid mono">${runId}</span>
      </div>
      <p style="font-size:11.5px;color:var(--tx-4);margin-bottom:12px">顺序与预算由服务端固定,Planner 只能在有限候选里选择补哪个缺口。</p>
      <div class="funnel-track" id="funnelTrack">
        ${FUNNEL_STEPS.map((n, i) => `<div class="funnel-step" data-f="${i}"><span class="fn mono">${i + 1}</span><span>${esc(n)}</span><span class="fd mono" data-fd="${i}"></span></div>`).join("")}
      </div>`;
    const track = $("#funnelTrack");
    const durs = [420, 780, 300, 260, 520, 980, 300, 620];
    for (let i = 0; i < FUNNEL_STEPS.length; i++) {
      const row = $(`[data-f="${i}"]`, track);
      row.classList.add("active");
      setStatus(`<span class="pulse"></span><span>${FUNNEL_STEPS[i]}…</span>`);
      await sleep(durs[i] * (i === 5 ? 0.9 : 0.62));
      $(`[data-fd="${i}"]`, row).textContent = durs[i] + "ms";
      row.classList.remove("active"); row.classList.add("ok");
      if (i === 1) { // transcript 检索完成, 附命中
        const st = sc.steps[0];
        if (st && st.hits) {
          row.insertAdjacentHTML("afterend", `<div style="margin:2px 0 8px 23px">${hitsHTML(st.hits)}</div>`);
        }
      }
      if (i === 5) { // 视觉确认
        const st = sc.steps.find((x) => x.frames);
        if (st && chatState.isVideo && chatState.v.demo) {
          row.insertAdjacentHTML("afterend", `<div style="margin:2px 0 8px 23px">${frameStripHTML(st)}</div>`);
        }
      }
    }
    const ms = ((performance.now() - t0) / 1000).toFixed(1);
    setStatus(`<span class="pulse"></span><span>正在生成引用回答…</span>`);
    const el = pushAgentMsg([], sc.citations, { mode: "funnel", ms, steps: 8, claims: sc.claims, runId });
    await streamAnswer($("[data-ans]", el), sc.answer);
    const lastMsg = chatState.messages[chatState.messages.length - 1];
    lastMsg.__claims = sc.claims; lastMsg.__cites = sc.citations;
    $("#evBadge").textContent = sc.claims.length;
    setStatus(`<span style="color:var(--ok)">${ic("check", "ic-sm")}</span><span>漏斗完成 · ${ms}s</span>`);
    if ($('.rail-tab[data-rt="ev"]').classList.contains("on")) renderRailEv();
    scrollBottom();
  }

  async function streamAnswer(box, tokens) {
    // 展开为带段落结构的 token 流
    const paras = [[]];
    for (const tk of tokens) {
      if (typeof tk === "string") {
        const parts = tk.split("\n");
        parts.forEach((seg, i) => {
          if (i > 0) paras.push([]);
          if (seg) {
            for (let j = 0; j < seg.length; j += 7) paras[paras.length - 1].push(seg.slice(j, j + 7));
          }
        });
      } else paras[paras.length - 1].push(tk);
    }
    const cursor = document.createElement("span");
    cursor.className = "stream-cursor";
    let p = document.createElement("p");
    box.appendChild(p); box.appendChild(cursor);
    for (const para of paras) {
      for (const tk of para) {
        if (typeof tk === "string") {
          p.appendChild(document.createTextNode(tk));
        } else {
          const chip = document.createElement("span");
          chip.className = "cite"; chip.textContent = tk.c; chip.setAttribute("data-c", tk.c);
          p.appendChild(chip);
          chip.addEventListener("click", () => {
            const msg = box.closest(".msg");
            const cites = msg.__cites || [];
            const cit = cites.find((x) => x.c === tk.c);
            if (cit) openEvidenceDrawer(cit, cites);
          });
        }
        await sleep(22 + Math.random() * 26);
      }
      scrollBottom();
      p = document.createElement("p");
      box.insertBefore(p, cursor);
    }
    cursor.remove();
    scrollBottom();
    box.querySelectorAll("p").forEach((pp) => { if (!pp.textContent && !pp.childElementCount) pp.remove(); });
  }

  function addStepEl(stepsEl, label) {
    const el = document.createElement("div");
    el.className = "step running";
    el.innerHTML = `<div class="step-dot"></div><div class="step-head"><span class="step-label">${esc(label)}</span><span class="step-dur mono"></span></div><div class="step-body"></div>`;
    stepsEl.appendChild(el);
    return el;
  }
  function finishStepEl(el, ms) {
    el.classList.remove("running"); el.classList.add("done");
    $(".step-dur", el).textContent = ms ? (ms >= 1000 ? (ms / 1000).toFixed(1) + "s" : ms + "ms") : "";
  }
  function toolHTML(st) {
    return `<div class="tool-card"><div class="tool-head"><span class="tool-name mono">${esc(st.tool)}</span><span class="tool-ms">${st.ms}ms</span></div><div class="tool-out">${esc(st.out || "")}</div></div>`;
  }
  function hitsHTML(h) {
    return `<div class="hits-card">
      <div class="hits-q">query <b>${esc(h.q)}</b>${h.cross ? ` · <span style="color:var(--acc-strong)">跨 ${h.cross} 个视频</span>` : ""} · ${h.n} 条命中</div>
      ${h.rows.map((r) => `<div class="hit-row"><span class="hs mono">${r.s}</span><span class="hv">${esc(r.v)}</span>${r.t && r.t !== "transcript" ? modalityTag(r.t) : ""}<span class="ht mono">${r.at}</span></div>`).join("")}
    </div>`;
  }
  function frameStripHTML(st) {
    const ids = st.frameIds || ["f-004", "f-031", "f-043"];
    const frames = FRAMES12.filter((f) => ids.includes(f.id));
    return `<div style="display:flex;gap:8px;margin-top:2px">
      ${frames.map((f) => `<div style="flex:1;min-width:0"><canvas data-snap="${f.timeMs}" style="width:100%;aspect-ratio:16/9;border-radius:8px;border:1px solid var(--line);background:var(--bg-3)"></canvas><div class="mono" style="font-size:9.5px;color:var(--tx-4);margin-top:4px">${fmtClock(f.timeMs)}</div></div>`).join("")}
    </div>
    <div style="font-size:10.5px;color:var(--tx-4)">${esc(st.out || "均来自已持久化的关键帧 observation,未触发在线视觉调用。")}</div>`;
  }
  // 运行中出现的帧画布取样
  new MutationObserver(() => {
    $$("canvas[data-snap]", $("#railBody")).forEach((c) => {
      if (c.__snapped) return; c.__snapped = true;
      snapFrame(currentDeck, c, Number(c.getAttribute("data-snap")));
    });
  }).observe($("#railBody"), { childList: true, subtree: true });
}

/* ---------------- 设置 ---------------- */
function pageSettings(root) {
  setCrumb(["设置"]);
  root.innerHTML = `
  <div class="page">
    <div class="section-head" style="margin-top:0"><h2>设置</h2></div>
    <div class="settings-grid">
      <div class="settings-nav">
        <button class="on" data-s="ai">AI 服务</button>
        <button data-s="mem">记忆治理</button>
      </div>
      <div id="settingsBody"></div>
    </div>
  </div>`;
  const body = $("#settingsBody");
  $$(".settings-nav button", root).forEach((b) => b.addEventListener("click", () => {
    $$(".settings-nav button", root).forEach((x) => x.classList.toggle("on", x === b));
    if (b.getAttribute("data-s") === "ai") renderAI(); else renderMem();
  }));

  function renderAI() {
    body.innerHTML = `
      <h3 style="font-size:14px;font-weight:600;margin-bottom:4px">AI 服务 (BYOK)</h3>
      <p style="font-size:12.5px;color:var(--tx-3);margin-bottom:16px">按能力分别配置,API Key 加密保存在服务端;一个中转是否支持某能力,以实际 endpoint 为准。</p>
      ${AI_PROFILES.map((p) => `
        <div class="profile-card">
          <div class="profile-icon">${ic(p.icon)}</div>
          <div class="pc-body">
            <div class="t"><b>${esc(p.cap)}</b>
              ${p.state === "ok" ? `<span class="chip chip-ok">${ic("check", "ic-sm")}${esc(p.stateText)}</span>` : `<span class="chip chip-mute">${esc(p.stateText)}</span>`}
            </div>
            <div class="d">${esc(p.model)} · ${esc(p.base)}</div>
            <div class="d" style="color:var(--tx-4)">${esc(p.meta)}</div>
          </div>
          <button class="btn btn-sm" data-test="${esc(p.cap)}">测试</button>
          <button class="btn btn-sm btn-ghost">${ic("pencil", "ic-sm")}</button>
        </div>`).join("")}
      <div class="card card-pad" style="margin-top:18px">
        <div style="display:flex;align-items:center;gap:10px">
          ${ic("shield")}<b style="font-size:13px">密钥安全</b>
        </div>
        <p style="font-size:12px;color:var(--tx-3);margin-top:6px;line-height:1.7">用户级配置会覆盖服务端默认策略;密钥以加密形式落库,日志与账本都不记录密钥原文。切换模型不会重做已完成的转写,但更换向量模型会触发索引 needs_rebuild。</p>
      </div>`;
    $$("[data-test]", body).forEach((b) => b.addEventListener("click", () => toast(`${b.getAttribute("data-test")}连通性正常 (演示)`, "check")));
  }

  function renderMem() {
    body.innerHTML = `
      <h3 style="font-size:14px;font-weight:600;margin-bottom:4px">记忆治理</h3>
      <p style="font-size:12.5px;color:var(--tx-3);margin-bottom:16px">记忆按用户 / 视频 / 知识库 / 会话隔离,写入异步进行;撤回不会删除历史,只是不再参与召回。</p>
      <div class="pref-row">
        <div class="pr-body"><b>会话记忆偏好</b><span>回答时参考最近的会话上下文与召回的长期记忆</span></div>
        <span class="switch on" data-sw></span>
      </div>
      <div class="pref-row">
        <div class="pr-body"><b>自动总结为记忆</b><span>在会话结束后异步抽取偏好与事实,冲突时保留版本并降低置信度</span></div>
        <div class="switch on" data-sw></div>
      </div>
      <div class="section-head" style="margin-top:24px"><h2 style="font-size:14px">已有记忆</h2><span style="font-size:12px;color:var(--tx-3)">${MEMORIES.length} 条</span></div>
      ${MEMORIES.map((m) => `
        <div class="memory-item ${m.status !== "active" ? "withdrawn" : ""}" data-mi="${m.id}">
          <div class="mi-body">
            <div class="mi-text">${esc(m.text)}</div>
            <div class="mi-meta">
              <span class="chip chip-mute">${esc(m.scopeText)}</span>
              ${m.status === "conflicted" ? `<span class="chip chip-warn">冲突待确认</span>` : ""}
              ${m.status !== "active" ? `<span class="chip chip-mute">已撤回</span>` : `<span class="chip chip-mute">重要度 ${esc(m.importance)}</span>`}
              <span class="mi-when">${esc(m.when)}</span>
            </div>
          </div>
          ${m.status === "active" ? `<button class="btn btn-sm btn-ghost" data-withdraw="${m.id}">撤回</button>` : ""}
        </div>`).join("")}`;
    $$("[data-sw]", body).forEach((s) => s.addEventListener("click", () => {
      s.classList.toggle("on");
      toast(s.classList.contains("on") ? "已开启" : "已关闭,仅在当前会话内生效");
    }));
    $$("[data-withdraw]", body).forEach((b) => b.addEventListener("click", () => {
      const item = b.closest(".memory-item");
      item.classList.add("withdrawn");
      b.remove();
      toast("已撤回,该记忆不再参与召回 (演示)");
    }));
  }

  renderAI();
}

/* ---------------- 抽屉: 引用证据 ---------------- */
function openEvidenceDrawer(cit, cites) {
  const vid = videoById(cit.task);
  const title = cit.videoTitle || (vid ? vid.title : "未知视频");
  const canJump = vid && vid.demo;
  const root = $("#overlayRoot");
  root.innerHTML = `
  <div class="drawer-veil" data-close></div>
  <div class="drawer">
    <div class="drawer-head">
      <span class="cno" style="width:26px;height:26px;border-radius:8px;display:grid;place-items:center;font-family:var(--font-mono);font-size:11px;font-weight:700;background:var(--acc-dim);color:var(--acc-strong);border:1px solid var(--acc-line)">C${cit.c}</span>
      <h3>证据详情</h3>
      <button class="btn btn-ic btn-ghost" style="margin-left:auto" data-close>${ic("x")}</button>
    </div>
    <div class="drawer-body">
      <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap">
        <span class="cvideo" style="font-size:13px;font-weight:600">${esc(title)}</span>
        <span class="ctime mono" style="font-size:11.5px;color:var(--acc-strong)">${fmtClock(cit.startMs)} - ${fmtClock(cit.endMs)}</span>
      </div>
      <div class="ev-quote">${esc(cit.quote)}</div>
      ${cit.frame ? `<div style="margin:12px 0"><canvas data-frame="${cit.frame}" style="width:100%;aspect-ratio:16/9;border-radius:11px;border:1px solid var(--line-strong);background:var(--bg-3)"></canvas>
      <p style="font-size:10.5px;color:var(--tx-4);margin-top:6px">该帧取自播放源对应时间点;正式环境里这里显示索引时保存的原始帧。</p></div>` : ""}
      <div class="ev-meta-grid">
        <div class="ev-meta-cell"><div class="k">证据模态</div><div class="v">${MODALITY[cit.modality] ? MODALITY[cit.modality].text : cit.modality}</div></div>
        <div class="ev-meta-cell"><div class="k">时间状态</div><div class="v">${cit.status === "exact" ? "精确 (毫秒)" : "粗粒度"}</div></div>
        <div class="ev-meta-cell"><div class="k">证据 ID</div><div class="v mono">${esc(cit.ev)}</div></div>
        <div class="ev-meta-cell"><div class="k">召回通道</div><div class="v mono">${esc(cit.src)}</div></div>
        <div class="ev-meta-cell"><div class="k">相关度</div><div class="v mono">${esc(cit.score)}</div></div>
        <div class="ev-meta-cell"><div class="k">来源映射</div><div class="v">mapped</div></div>
      </div>
      <div class="field-label">展示上下文</div>
      <p style="font-size:12px;color:var(--tx-2);line-height:1.7">${esc(cit.ctx)}</p>
      <div style="display:flex;gap:9px;margin-top:18px">
        <button class="btn btn-primary" data-jump ${canJump ? "" : "disabled title='演示原型只内置《海洋》素材的播放源'"}>${ic("play", "ic-sm")}跳转 ${fmtClock(cit.startMs)} 回放</button>
        <button class="btn" data-copyev>${ic("file", "ic-sm")}复制引用</button>
      </div>
    </div>
  </div>`;
  root.querySelector("[data-close]").addEventListener("click", closeOverlay);
  root.querySelector(".drawer [data-close]").addEventListener("click", closeOverlay);
  const jb = root.querySelector("[data-jump]");
  if (canJump) jb.addEventListener("click", () => {
    if (chatState && chatState.isVideo && currentDeck) {
      currentDeck.seek(cit.startMs, true);
    } else {
      nav(`#/video/${cit.task}`);
    }
    toast(`已跳转 ${fmtClock(cit.startMs)}`, "play");
  });
  root.querySelector("[data-copyev]").addEventListener("click", () => {
    navigator.clipboard && navigator.clipboard.writeText(`[${title} ${fmtClock(cit.startMs)}] ${cit.quote}`);
    toast("引用文本已复制");
  });
  const fc = root.querySelector("[data-frame]");
  if (fc) snapFrame(currentDeck, fc, Number(fc.getAttribute("data-frame")));
}

/* ---------------- 抽屉: 证据账本 ---------------- */
function openLedgerDrawer(claims, cites, meta) {
  const root = $("#overlayRoot");
  root.innerHTML = `
  <div class="drawer-veil" data-close></div>
  <div class="drawer" style="width:520px">
    <div class="drawer-head">
      ${ic("shield-check")}<h3>证据账本</h3>
      <span class="rid mono" style="font-size:10px;color:var(--tx-4)">${meta.runId || "run-" + rid()}</span>
      <button class="btn btn-ic btn-ghost" style="margin-left:auto" data-close>${ic("x")}</button>
    </div>
    <div class="drawer-body">
      <div class="run-meta" style="margin-bottom:14px">
        <span class="chip chip-mute mono">${meta.mode === "strict" ? "strict_rag" : meta.mode === "funnel" ? "evidence_funnel" : meta.mode === "research" ? "research" : "agent"}</span>
        <span class="chip chip-mute">${claims.length} 条 claim · ${(cites || []).length} 条证据</span>
        <span class="chip chip-mute">追加式,不覆盖历史</span>
      </div>
      <p style="font-size:12px;color:var(--tx-3);margin-bottom:14px">每条回答事实都会与证据显式绑定。「已验证」表示来源与时间范围可回放核对,不代表语义真值;你可以对不确定的判断追加人工更正。</p>
      ${claimsHTML(claims, cites)}
    </div>
  </div>`;
  root.querySelector("[data-close]").addEventListener("click", closeOverlay);
  root.querySelector(".drawer [data-close]").addEventListener("click", closeOverlay);
  bindClaimCards(root, cites);
}

/* ---------------- 上传模态 ---------------- */
function openUploadModal() {
  const root = $("#overlayRoot");
  root.innerHTML = `
  <div class="overlay" data-close>
    <div class="modal">
      <div class="modal-head"><h3>上传视频</h3><button class="btn btn-ic btn-ghost" data-close>${ic("x")}</button></div>
      <div class="modal-body">
        <div class="seg" style="margin-bottom:14px" id="upSeg">
          <button class="on" data-up="file">本地文件</button>
          <button data-up="url">视频链接</button>
        </div>
        <div id="upFile">
          <div class="dropzone" id="dz">
            ${ic("upload")}<b>拖入视频文件,或点击选择</b>
            <span>支持分片上传与断点续传 · 单文件 2 GB 以内</span>
          </div>
          <div id="upRows" style="margin-top:6px"></div>
        </div>
        <div id="upUrl" style="display:none">
          <label class="field-label">视频页面链接</label>
          <input class="input" id="urlInput" placeholder="https://www.bilibili.com/video/… 或任意可下载地址" />
          <p class="field-help">服务端会用 yt-dlp 拉取,下载阶段单独排队,失败可独立重试。</p>
          <button class="btn btn-primary" style="margin-top:12px" id="urlGo">创建下载任务</button>
        </div>
      </div>
    </div>
  </div>`;
  root.querySelector(".overlay").addEventListener("click", (e) => { if (e.target.hasAttribute("data-close")) closeOverlay(); });
  root.querySelector(".modal [data-close]").addEventListener("click", closeOverlay);
  $("#upSeg").addEventListener("click", (e) => {
    const b = e.target.closest("button"); if (!b) return;
    $$("#upSeg button").forEach((x) => x.classList.toggle("on", x === b));
    const k = b.getAttribute("data-up");
    $("#upFile").style.display = k === "file" ? "" : "none";
    $("#upUrl").style.display = k === "url" ? "" : "none";
  });
  const dz = $("#dz"), rows = $("#upRows");
  const fakeUpload = (name, size) => {
    const row = document.createElement("div");
    row.className = "uprow";
    row.innerHTML = `<div class="un"><b>${esc(name)}</b><span>${size} · 上传中</span></div><div class="meter"><i style="width:4%"></i></div>`;
    rows.appendChild(row);
    let p = 4;
    const iv = setInterval(() => {
      p += 7 + Math.random() * 14;
      if (p >= 100) {
        p = 100; clearInterval(iv);
        $("span", row).textContent = `${size} · 已上传,等待处理`;
        toast("上传完成,任务已创建并进入队列");
      }
      $(".meter i", row).style.width = p + "%";
    }, 300);
  };
  dz.addEventListener("click", () => fakeUpload(["发布会集锦 4K.mp4", "内部分享剪辑.mov", "公开课录制 03.mkv"][Math.floor(Math.random() * 3)], ["812 MB", "1.1 GB", "356 MB"][Math.floor(Math.random() * 3)]));
  dz.addEventListener("dragover", (e) => { e.preventDefault(); dz.classList.add("over"); });
  dz.addEventListener("dragleave", () => dz.classList.remove("over"));
  dz.addEventListener("drop", (e) => { e.preventDefault(); dz.classList.remove("over"); fakeUpload((e.dataTransfer.files[0] && e.dataTransfer.files[0].name) || "拖入的视频.mp4", "690 MB"); });
  $("#urlGo").addEventListener("click", () => {
    const val = $("#urlInput").value.trim();
    if (!val) { toast("先粘贴一个视频链接"); return; }
    toast("下载任务已创建,可关闭窗口继续工作");
    closeOverlay();
  });
}

/* 全局委托: data-toast */
document.addEventListener("click", (e) => {
  const b = e.target.closest("[data-toast]");
  if (b) toast(b.getAttribute("data-toast"));
});

/* 启动 */
render();
