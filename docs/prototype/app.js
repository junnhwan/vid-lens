/* VidLens desktop prototype — one scheme, mock visitor flow, no backend.
   Question: does link → live activity → read-only article with side Q&A feel right? */

const EASE = "cubic-bezier(0.23, 1, 0.32, 1)";
const SAMPLE = "https://www.bilibili.com/video/BV1example";

const FRAMES = [
  { id: "0512", time: "05:12", menu: "closed", short: "展开前", caption: "查看展开前的编辑界面" },
  { id: "0524", time: "05:24", menu: "open", short: "展开后的菜单", caption: "展开菜单，查看可添加的步骤。" },
];

const BEATS = [
  {
    delay: 0,
    title: "正在读取平台字幕",
    purpose: "先用完整字幕建立时间线。",
    range: "00:00 – 35:42",
    step: { id: "subs", label: "读取平台字幕", range: "00:00 – 35:42" },
  },
  {
    delay: 1500,
    complete: "subs",
    outcome: "已取得全片字幕",
    title: "正在梳理视频主线",
    purpose: "找出适合写成步骤的段落。",
    range: "",
    step: { id: "spine", label: "梳理视频主线" },
  },
  {
    delay: 1500,
    complete: "spine",
    outcome: "主线是把重复操作存成流程",
    title: "正在查看菜单展开后的画面",
    purpose: "确认选项名称，补足操作步骤。",
    range: "05:10 – 05:30",
    step: { id: "menu", label: "确认菜单选项", range: "05:10 – 05:30", frames: ["0512", "0524"] },
  },
  { delay: 900, frame: "0512" },
  { delay: 1200, frame: "0524" },
  {
    delay: 1000,
    complete: "menu",
    outcome: "看到添加步骤、保存流程、运行",
    title: "正在组织图文文章",
    purpose: "把已确认的步骤写成可阅读的段落。",
    range: "",
    step: { id: "write", label: "组织图文文章" },
  },
  { delay: 1500, complete: "write", outcome: "已写成可阅读的文章", done: true },
];

const SNAPSHOT = [
  { id: "subs", label: "读取平台字幕", status: "done", outcome: "已取得全片字幕", range: "00:00 – 35:42" },
  { id: "spine", label: "梳理视频主线", status: "done", outcome: "主线是把重复操作存成流程" },
  { id: "menu", label: "确认菜单选项", status: "done", outcome: "看到添加步骤、保存流程、运行", range: "05:10 – 05:30", frames: ["0512", "0524"] },
  { id: "write", label: "组织图文文章", status: "done", outcome: "已写成可阅读的文章" },
];

const state = {
  phase: "input",
  runToken: 0,
  answerToken: 0,
  answering: false,
  historyOpen: false,
  steps: [],
  framesShown: [],
  qaOpen: false,
  messages: [],
  lightbox: { open: false, index: 0, origin: null },
  mapLock: null,
  seq: 0,
};

const pending = new Set();
let titleAnim = null;
let stageMotion = null;
let closingLightbox = false;

const $ = (id) => document.getElementById(id);

function prefersReduced() {
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

function later(fn, ms) {
  const id = setTimeout(() => {
    pending.delete(id);
    fn();
  }, ms);
  pending.add(id);
}

function clearPending() {
  for (const id of pending) clearTimeout(id);
  pending.clear();
}

function frameById(id) {
  return FRAMES.find((frame) => frame.id === id);
}

function createShot(id) {
  const frame = frameById(id);
  const tpl = $(frame.menu === "open" ? "tpl-shot-open" : "tpl-shot-closed");
  return tpl.content.firstElementChild.cloneNode(true);
}

function statusText(status) {
  if (status === "done") return "已完成";
  if (status === "active") return "进行中";
  if (status === "stopped") return "已停止";
  return "";
}

function stepView(step, detailed) {
  const row = document.createElement("div");
  row.className = "step";
  row.dataset.status = step.status;
  if (step.fresh) row.dataset.new = "true";
  row.setAttribute("role", "listitem");

  const mark = document.createElement("span");
  mark.className = "mark";
  mark.setAttribute("aria-hidden", "true");

  const body = document.createElement("div");
  const label = document.createElement("div");
  label.className = "step-label";
  label.textContent = step.label;
  const sr = document.createElement("span");
  sr.className = "sr-only";
  sr.textContent = statusText(step.status);
  label.append(sr);
  body.append(label);

  if (detailed && step.outcome && step.status !== "active") {
    const outcome = document.createElement("div");
    outcome.className = "step-outcome";
    outcome.textContent = step.outcome;
    body.append(outcome);
  }
  if (detailed && step.range) {
    const range = document.createElement("div");
    range.className = "step-range";
    range.textContent = step.range;
    body.append(range);
  }
  if (detailed && step.frames) {
    const shown = step.frames.filter((id) => state.framesShown.includes(id) || state.phase === "reading");
    if (shown.length) {
      const mini = document.createElement("div");
      mini.className = "mini-row";
      for (const id of shown) {
        const button = document.createElement("button");
        button.type = "button";
        button.className = "mini-shot";
        button.dataset.frame = id;
        const host = document.createElement("span");
        host.className = "shot-host";
        host.append(createShot(id));
        const time = document.createElement("span");
        time.className = "mini-time";
        time.textContent = frameById(id).time;
        button.append(host, time);
        mini.append(button);
      }
      body.append(mini);
    }
  }

  row.append(mark, body);
  return row;
}

function renderSteps() {
  const list = $("run-steps");
  const previous = list.scrollTop;
  const nearBottom = list.scrollHeight - list.scrollTop - list.clientHeight < 28;
  const visible = state.historyOpen ? state.steps : state.steps.slice(-3);
  list.classList.toggle("is-open", state.historyOpen);
  list.replaceChildren(...visible.map((step) => stepView(step, state.historyOpen)));
  for (const step of state.steps) step.fresh = false;
  if (state.historyOpen) {
    if (nearBottom) list.scrollTop = list.scrollHeight;
    else list.scrollTop = previous;
  }
  const overflow = state.historyOpen && list.scrollHeight - list.clientHeight > 8;
  $("jump-latest").hidden = !overflow || list.scrollHeight - list.scrollTop - list.clientHeight < 28;
  $("toggle-history").textContent = state.historyOpen ? "收起过程" : "查看全部过程";
  $("toggle-history").setAttribute("aria-expanded", String(state.historyOpen));
}

function renderGenSteps() {
  $("gen-steps").replaceChildren(...state.steps.map((step) => stepView(step, true)));
}

function revealFrame(id) {
  if (state.framesShown.includes(id)) return;
  state.framesShown.push(id);
  const frame = frameById(id);
  const button = document.createElement("button");
  button.type = "button";
  button.className = "frame-card";
  button.dataset.frame = id;
  button.dataset.new = "true";
  const host = document.createElement("span");
  host.className = "shot-host";
  host.append(createShot(id));
  const cap = document.createElement("span");
  cap.className = "frame-cap";
  cap.textContent = `${frame.time}  ${frame.short}`;
  button.append(host, cap);
  $("run-frames").append(button);
}

function setActivity(title, purpose, range, animate) {
  const titleEl = $("activity-title");
  const same = titleEl.dataset.value === title;
  titleEl.dataset.value = title;
  $("activity-purpose").textContent = purpose || "";
  const rangeEl = $("activity-range");
  rangeEl.hidden = !range;
  rangeEl.textContent = range || "";
  if (!same) {
    $("live").textContent = title;
    if (animate && !prefersReduced() && titleEl.textContent) {
      titleAnim?.cancel();
      const out = titleEl.animate(
        [
          { opacity: 1, filter: "blur(0px)", transform: "translateY(0)" },
          { opacity: 0.4, filter: "blur(1.5px)", transform: "translateY(-4px)" },
        ],
        { duration: 120, easing: EASE, fill: "forwards" },
      );
      titleAnim = out;
      out.onfinish = () => {
        if (titleAnim !== out) return;
        titleEl.textContent = title;
        const incoming = titleEl.animate(
          [
            { opacity: 0.4, filter: "blur(1.5px)", transform: "translateY(6px)" },
            { opacity: 1, filter: "blur(0px)", transform: "translateY(0)" },
          ],
          { duration: 200, easing: EASE, fill: "forwards" },
        );
        titleAnim = incoming;
        incoming.onfinish = () => {
          if (titleAnim === incoming) incoming.cancel();
        };
      };
    } else {
      titleAnim?.cancel();
      titleEl.textContent = title;
    }
  }
  if (state.phase === "generating" || state.phase === "holding") {
    document.title = `${title} · VidLens`;
  }
  renderDock();
}

function applyBeat(beat) {
  if (beat.complete) {
    const step = state.steps.find((item) => item.id === beat.complete);
    if (step) {
      step.status = "done";
      step.outcome = beat.outcome || "";
    }
  }
  if (beat.step) {
    state.steps.push({
      id: beat.step.id,
      label: beat.step.label,
      status: "active",
      outcome: "",
      range: beat.step.range || "",
      frames: beat.step.frames || null,
      fresh: true,
    });
  }
  if (beat.frame) revealFrame(beat.frame);
  if (beat.title) setActivity(beat.title, beat.purpose, beat.range, true);
  renderSteps();
  if (beat.done) finishRun();
}

function scheduleBeats() {
  const token = state.runToken;
  let acc = 0;
  for (const beat of BEATS) {
    acc += beat.delay;
    const run = () => {
      if (token !== state.runToken) return;
      applyBeat(beat);
    };
    if (acc === 0) run();
    else later(run, acc);
  }
}

function setPhase(phase) {
  state.phase = phase;
  $("view-input").hidden = phase !== "input";
  $("view-run").hidden = phase !== "generating" && phase !== "holding";
  $("view-read").hidden = phase !== "reading";
  document.body.dataset.phase = phase;
  const busy = phase === "generating";
  $("view-run").querySelector(".activity").setAttribute("aria-busy", String(busy));
  if (phase === "input") document.title = "VidLens";
  if (phase === "reading") document.title = "把重复操作变成可复用流程 · VidLens";
  updateAskEnabled();
  renderDock();
}

function startRun() {
  const link = $("link").value.trim();
  if (!link) {
    $("link-error").hidden = false;
    $("link").setAttribute("aria-invalid", "true");
    $("link").focus();
    return;
  }
  $("link-error").hidden = true;
  $("link").removeAttribute("aria-invalid");
  state.runToken += 1;
  state.answerToken += 1;
  clearPending();
  forceCloseLightbox();
  state.answering = false;
  state.frameChip = null;
  state.passageChip = null;
  state.historyOpen = false;
  state.steps = [];
  state.framesShown = [];
  state.messages = [];
  state.qaOpen = false;
  $("run-frames").replaceChildren();
  $("run-steps").replaceChildren();
  $("qa-log").querySelectorAll(".turn").forEach((node) => node.remove());
  $("qa-empty").hidden = false;
  $("ready-banner").hidden = true;
  $("stop-run").hidden = false;
  $("retry-run").hidden = true;
  $("pulse").hidden = false;
  $("run-kicker").textContent = "正在理解视频";
  $("composer").value = "";
  paintComposer();
  setQa(false);
  setPhase("generating");
  scheduleBeats();
}

function stopRun() {
  state.runToken += 1;
  clearPending();
  $("pulse").hidden = true;
  $("stop-run").hidden = true;
  $("retry-run").hidden = false;
  $("run-kicker").textContent = "已停止";
  const current = state.steps.find((step) => step.status === "active");
  if (current) {
    current.status = "stopped";
    current.outcome = "停在这里";
  }
  renderSteps();
  setActivity("已停止理解", "停在已经取得的内容，文章还没有生成。", $("activity-range").hidden ? "" : $("activity-range").textContent, true);
  $("live").textContent = "已停止理解";
}

function finishRun() {
  $("pulse").hidden = true;
  $("stop-run").hidden = true;
  const engaged = state.lightbox.open || state.historyOpen;
  if (engaged) {
    setPhase("holding");
    $("run-kicker").textContent = "理解已完成";
    $("ready-banner").hidden = false;
    setActivity("已整理成可阅读的文章", "过程先留在这里。", "", true);
    return;
  }
  beginReading();
}

function beginReading() {
  $("ready-banner").hidden = true;
  $("pulse").hidden = true;
  if (!state.steps.length || state.steps.some((step) => step.status !== "done")) {
    state.steps = SNAPSHOT.map((step) => ({ ...step }));
    state.framesShown = ["0512", "0524"];
  }
  renderGenSteps();
  $("gen-panel").dataset.open = "false";
  $("toggle-gen").setAttribute("aria-expanded", "false");
  $("toggle-gen").textContent = "查看生成过程";
  setPhase("reading");
  setQa(false);
  window.scrollTo(0, 0);
  setMapCurrent("article-top");
  if (!prefersReduced()) {
    const anim = $("view-read").animate(
      [
        { opacity: 0, transform: "translateY(8px)" },
        { opacity: 1, transform: "none" },
      ],
      { duration: 280, easing: EASE, fill: "forwards" },
    );
    anim.onfinish = () => anim.cancel();
  }
}

function newVideo() {
  state.runToken += 1;
  state.answerToken += 1;
  clearPending();
  forceCloseLightbox();
  state.answering = false;
  state.messages = [];
  state.frameChip = null;
  state.passageChip = null;
  state.qaOpen = false;
  $("composer").value = "";
  $("qa-log").querySelectorAll(".turn").forEach((node) => node.remove());
  $("qa-empty").hidden = false;
  $("ask-passage").hidden = true;
  setQa(false);
  setPhase("input");
  $("link").focus();
  $("link").select();
}

function jumpKey(phase) {
  if (phase === "reading") return "read";
  if (phase === "input") return "input";
  return "run";
}

function renderDock() {
  const key = jumpKey(state.phase);
  document.querySelectorAll(".proto-dock button").forEach((button) => {
    button.setAttribute("aria-pressed", String(button.dataset.jump === key));
  });
  const title = state.phase === "input"
    ? "等待链接"
    : state.phase === "reading"
      ? "文章"
      : ($("activity-title").dataset.value || "生成");
  $("proto-now").textContent = title;
}

function setMapCurrent(id) {
  document.querySelectorAll(".node").forEach((button) => {
    if (button.dataset.target === id) button.setAttribute("aria-current", "location");
    else button.removeAttribute("aria-current");
  });
  const atlas = document.querySelector(".atlas");
  if (atlas) atlas.dataset.current = id;
}

function setQa(open, focus) {
  if (!open && $("qa").contains(document.activeElement)) $("toggle-qa").focus();
  state.qaOpen = open;
  $("workspace").dataset.qa = open ? "open" : "closed";
  $("qa").setAttribute("aria-hidden", String(!open));
  $("qa").inert = !open;
  $("toggle-qa").setAttribute("aria-pressed", String(open));
  if (open && focus) {
    $("composer").focus();
  }
}

function paintComposer() {
  const wrap = $("chips");
  wrap.replaceChildren();
  if (state.frameChip) wrap.append(makeChip(state.frameChip.label, "移除这张图", () => {
    state.frameChip = null;
    paintComposer();
  }));
  if (state.passageChip) wrap.append(makeChip(clip(state.passageChip, 18), "移除选中段落", () => {
    state.passageChip = null;
    paintComposer();
  }));
  paintSend();
}

function makeChip(label, removeLabel, onRemove) {
  const chip = document.createElement("span");
  chip.className = "chip";
  const text = document.createElement("span");
  text.textContent = label;
  const remove = document.createElement("button");
  remove.type = "button";
  remove.setAttribute("aria-label", removeLabel);
  remove.textContent = "×";
  remove.addEventListener("click", onRemove);
  chip.append(text, remove);
  return chip;
}

function staticChip(label) {
  const chip = document.createElement("span");
  chip.className = "chip static";
  const text = document.createElement("span");
  text.textContent = label;
  chip.append(text);
  return chip;
}

function clip(text, max) {
  const clean = text.replace(/\s+/g, " ").trim();
  return clean.length > max ? `${clean.slice(0, max)}…` : clean;
}

function paintSend() {
  const answering = state.answering;
  $("send").hidden = answering;
  $("stop-answer").hidden = !answering;
  $("send").disabled = $("composer").value.trim().length === 0;
}

function autoSize() {
  const field = $("composer");
  field.style.height = "auto";
  field.style.height = `${Math.min(field.scrollHeight, 120)}px`;
}

function askFrame(id) {
  if (state.phase !== "reading" && state.phase !== "holding") return;
  const frame = frameById(id);
  state.frameChip = { id, label: `${frame.time} · ${frame.short}` };
  $("composer").value = `这张 ${frame.time} 的画面在说明什么？`;
  autoSize();
  const after = () => {
    if (state.phase === "holding") beginReading();
    paintComposer();
    setQa(true, true);
    const field = $("composer");
    field.setSelectionRange(field.value.length, field.value.length);
  };
  if ($("lightbox").open) closeLightbox(after);
  else after();
}

function askPassage(text) {
  state.passageChip = text.slice(0, 240);
  paintComposer();
  setQa(true, true);
}

function buildPlan(message) {
  const q = message.question;
  const steps = [];
  if (message.passage) {
    steps.push({ title: "正在阅读选中的段落", label: "阅读选中的段落", delay: 650, outcome: "已对照文章原文" });
  }
  if (/保存/.test(q) && /运行/.test(q)) {
    steps.push(
      { title: "正在查阅相关字幕", label: "查阅相关字幕", delay: 800, range: "05:10 – 05:30", outcome: "字幕里先保存，再运行" },
      { title: "正在查看操作前后画面", label: "查看操作前后画面", delay: 1000, range: "05:10 – 05:30", frameId: "0524", outcome: "菜单里有保存流程和运行" },
    );
    return {
      summary: "已核对操作顺序",
      steps,
      answer: "先保存可以确认流程配置，再执行操作。05:12 还停在编辑，05:24 的菜单里同时有「保存流程」和「运行」。",
      citations: ["0512", "0524"],
    };
  }
  if (message.frameId || /画面|这张|菜单/.test(q)) {
    const frame = frameById(message.frameId || "0524");
    steps.push(
      { title: `正在查看 ${frame.time} 的画面`, label: "查看这张画面", delay: 850, frameId: frame.id, outcome: frame.caption },
      { title: "正在对照相邻字幕", label: "对照相邻字幕", delay: 750, range: "05:10 – 05:30", outcome: "字幕和菜单名称一致" },
    );
    return {
      summary: "已看过这张画面",
      steps,
      answer: frame.id === "0512"
        ? "05:12 还在编辑流程，主按钮是「添加步骤」，菜单没有展开。"
        : "05:24 菜单已经展开，能看到「添加步骤」「保存流程」和「运行」。这一帧说明的是可添加的步骤，还不是运行结果。",
      citations: [frame.id],
    };
  }
  if (/谁|价格|多少钱|下载|作者|评价|下一集|怎么收费/.test(q)) {
    steps.push(
      { title: "正在阅读文章", label: "阅读文章", delay: 600, outcome: "文章没有这个问题" },
      { title: "正在查找相关字幕", label: "查找相关字幕", delay: 800, outcome: "已查看的片段里没有依据" },
    );
    return {
      summary: "没有找到依据",
      steps,
      answer: "文章和已查看的 05:10–05:30 里没有这个问题的依据。这里不会把示例以外的内容写成视频里说过的话。",
      citations: [],
    };
  }
  steps.push({ title: "正在阅读相关段落", label: "阅读相关段落", delay: 700, outcome: "主线是把重复操作存成流程" });
  return {
    summary: "已根据文章说明",
    steps,
    answer: "这条视频把反复要做的操作收成一套流程：在菜单里添加步骤，保存后再运行。已经核对过的画面是 05:12 和 05:24，没有看到运行完成后的界面。",
    citations: ["0524"],
  };
}

function ensureTurn(message) {
  let turn = document.querySelector(`.turn[data-id="${message.id}"]`);
  if (turn) return turn;
  turn = document.createElement("article");
  turn.className = "turn";
  turn.dataset.id = message.id;
  turn.innerHTML = `
    <h3 class="turn-q"></h3>
    <div class="turn-chips"></div>
    <div class="turn-activity">
      <button type="button" class="turn-summary" hidden></button>
      <div class="disclose" data-open="true">
        <div class="disclose-inner">
          <p class="turn-title"></p>
          <div class="turn-steps"></div>
        </div>
      </div>
    </div>
    <div class="turn-answer" hidden></div>
  `;
  $("qa-log").append(turn);
  return turn;
}

function fillAnswer(node, message) {
  node.replaceChildren();
  const paragraph = document.createElement("p");
  paragraph.textContent = message.answer;
  node.append(paragraph);
  if (!message.citations.length) return;
  const cites = document.createElement("div");
  cites.className = "citations";
  const label = document.createElement("span");
  label.textContent = "来源";
  cites.append(label);
  for (const id of message.citations) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "cite";
    button.dataset.frame = id;
    button.textContent = frameById(id).time;
    cites.append(button);
  }
  node.append(cites);
}

function paintTurn(message) {
  const log = $("qa-log");
  const near = log.scrollHeight - log.scrollTop - log.clientHeight < 80;
  const turn = ensureTurn(message);
  turn.querySelector(".turn-q").textContent = message.question;
  const chips = turn.querySelector(".turn-chips");
  chips.replaceChildren();
  if (message.frameLabel) chips.append(staticChip(message.frameLabel));
  if (message.passage) chips.append(staticChip(clip(message.passage, 18)));

  const summary = turn.querySelector(".turn-summary");
  const running = message.status === "running";
  summary.hidden = running;
  summary.textContent = message.status === "stopped" ? "已停止" : message.summary;
  summary.setAttribute("aria-expanded", String(running || message.open));
  turn.querySelector(".disclose").dataset.open = running || message.open ? "true" : "false";
  turn.querySelector(".turn-title").textContent = message.activityTitle;

  const steps = turn.querySelector(".turn-steps");
  steps.replaceChildren(...message.steps.map((step) => stepView({
    ...step,
    frames: step.frameId ? [step.frameId] : null,
  }, true)));
  message.steps.forEach((step) => { step.fresh = false; });

  const answer = turn.querySelector(".turn-answer");
  if (message.status === "done") {
    answer.hidden = false;
    fillAnswer(answer, message);
  } else if (message.status === "stopped") {
    answer.hidden = false;
    answer.replaceChildren();
    const note = document.createElement("p");
    note.className = "turn-stopped";
    note.textContent = "没有继续生成回答。";
    answer.append(note);
  } else {
    answer.hidden = true;
    answer.replaceChildren();
  }
  $("qa-empty").hidden = state.messages.length > 0;
  if (near) log.scrollTop = log.scrollHeight;
}

function runPlan(message) {
  const token = ++state.answerToken;
  const plan = buildPlan(message);
  message.summary = plan.summary;
  const specs = plan.steps;
  let index = 0;

  const finish = () => {
    if (token !== state.answerToken) return;
    const last = message.steps[message.steps.length - 1];
    if (last?.status === "active") last.status = "done";
    message.status = "done";
    message.open = false;
    message.activityTitle = plan.summary;
    message.answer = plan.answer;
    message.citations = plan.citations;
    state.answering = false;
    paintTurn(message);
    paintSend();
  };

  const activate = () => {
    if (token !== state.answerToken) return;
    if (index >= specs.length) {
      finish();
      return;
    }
    const spec = specs[index];
    const previous = message.steps[message.steps.length - 1];
    if (previous?.status === "active") previous.status = "done";
    message.activityTitle = spec.title;
    message.steps.push({
      label: spec.label,
      status: "active",
      range: spec.range || "",
      frameId: spec.frameId || "",
      outcome: spec.outcome || "",
      fresh: true,
    });
    paintTurn(message);
    later(() => {
      if (token !== state.answerToken) return;
      index += 1;
      activate();
    }, spec.delay);
  };

  activate();
}

function sendQuestion() {
  const text = $("composer").value.trim();
  if (!text || state.answering) return;
  const message = {
    id: `m${++state.seq}`,
    question: text,
    frameId: state.frameChip?.id || "",
    frameLabel: state.frameChip?.label || "",
    passage: state.passageChip || "",
    status: "running",
    activityTitle: "正在准备",
    summary: "",
    steps: [],
    open: true,
    answer: "",
    citations: [],
  };
  state.messages.push(message);
  state.frameChip = null;
  state.passageChip = null;
  state.answering = true;
  $("composer").value = "";
  autoSize();
  paintComposer();
  paintTurn(message);
  runPlan(message);
}

function stopAnswer() {
  state.answerToken += 1;
  state.answering = false;
  const message = [...state.messages].reverse().find((item) => item.status === "running");
  if (message) {
    message.status = "stopped";
    message.open = true;
    message.summary = "已停止";
    message.activityTitle = "已停止";
    message.steps.forEach((step) => {
      if (step.status === "active") step.status = "stopped";
    });
    paintTurn(message);
  }
  paintSend();
}

function updateAskEnabled() {
  const ok = state.phase === "reading" || state.phase === "holding";
  $("lb-ask").disabled = !ok;
  $("lb-ask-note").hidden = ok;
}

function renderLightbox() {
  const frame = FRAMES[state.lightbox.index];
  const stage = $("lb-stage");
  stage.replaceChildren(createShot(frame.id));
  $("lb-caption").textContent = frame.caption;
  $("lb-count").textContent = `${state.lightbox.index + 1} / ${FRAMES.length}`;
  $("lb-prev").disabled = state.lightbox.index === 0;
  $("lb-next").disabled = state.lightbox.index === FRAMES.length - 1;
  $("lb-ask").dataset.ask = frame.id;
  updateAskEnabled();
}

function fly(stage, origin, closing) {
  if (prefersReduced() || !origin || !origin.isConnected) return null;
  const first = origin.getBoundingClientRect();
  const last = stage.getBoundingClientRect();
  if (!last.width || !last.height || !first.width) return null;
  const dx = first.left - last.left;
  const dy = first.top - last.top;
  const sx = first.width / last.width;
  const sy = first.height / last.height;
  const from = `translate(${dx}px, ${dy}px) scale(${sx}, ${sy})`;
  stageMotion?.cancel();
  stageMotion = stage.animate(
    closing
      ? [{ transform: "none" }, { transform: from }]
      : [{ transform: from }, { transform: "none" }],
    { duration: closing ? 220 : 280, easing: "cubic-bezier(0.32, 0.72, 0, 1)", fill: "forwards" },
  );
  return stageMotion;
}

function openLightbox(id, origin) {
  const index = FRAMES.findIndex((frame) => frame.id === id);
  if (index < 0) return;
  const dialog = $("lightbox");
  const wasOpen = dialog.open;
  state.lightbox = { open: true, index, origin: origin || null };
  renderLightbox();
  if (!wasOpen) {
    dialog.showModal();
    if (!prefersReduced()) {
      dialog.animate(
        [{ backgroundColor: "rgba(22, 19, 16, 0)" }, { backgroundColor: "rgba(22, 19, 16, 0.9)" }],
        { duration: 180, easing: EASE },
      );
      requestAnimationFrame(() => {
        const motion = fly($("lb-stage"), origin, false);
        motion?.finished.then(() => {
          if (stageMotion === motion) motion.cancel();
        }).catch(() => {});
      });
    }
  }
}

function forceCloseLightbox() {
  const dialog = $("lightbox");
  closingLightbox = false;
  state.lightbox.open = false;
  if (dialog.open) dialog.close();
}

function closeLightbox(done) {
  const dialog = $("lightbox");
  if (!dialog.open) {
    state.lightbox.open = false;
    done?.();
    return;
  }
  if (closingLightbox) return;
  const stage = $("lb-stage");
  const origin = state.lightbox.origin;
  if (prefersReduced()) {
    forceCloseLightbox();
    done?.();
    return;
  }
  closingLightbox = true;
  const motion = fly(stage, origin, true);
  const fade = dialog.animate(
    [{ backgroundColor: "rgba(22, 19, 16, 0.9)" }, { backgroundColor: "rgba(22, 19, 16, 0)" }],
    { duration: 180, easing: EASE, fill: "forwards" },
  );
  const finish = () => {
    fade.cancel();
    motion?.cancel();
    stageMotion = null;
    dialog.style.backgroundColor = "";
    stage.style.transform = "";
    forceCloseLightbox();
    done?.();
  };
  if (motion) motion.onfinish = finish;
  else fade.onfinish = finish;
}

function showPassage(rect, text) {
  const pop = $("ask-passage");
  pop.hidden = false;
  pop.dataset.text = text;
  const width = 72;
  const left = Math.max(12, Math.min(rect.left + rect.width / 2 - width / 2, window.innerWidth - width - 12));
  let top = rect.top - 38;
  if (top < 64) top = rect.bottom + 8;
  pop.style.left = `${left}px`;
  pop.style.top = `${top}px`;
}

function syncSelection() {
  const pop = $("ask-passage");
  if (state.phase !== "reading") {
    pop.hidden = true;
    return;
  }
  const sel = document.getSelection();
  if (!sel || sel.isCollapsed || !$("prose").contains(sel.anchorNode) || !$("prose").contains(sel.focusNode)) {
    pop.hidden = true;
    return;
  }
  const text = sel.toString().replace(/\s+/g, " ").trim();
  if (text.length < 2) {
    pop.hidden = true;
    return;
  }
  showPassage(sel.getRangeAt(0).getBoundingClientRect(), text);
}

function bind() {
  $("link-form").addEventListener("submit", (event) => {
    event.preventDefault();
    startRun();
  });
  $("use-sample").addEventListener("click", () => {
    $("link").value = SAMPLE;
    $("link-error").hidden = true;
    $("link").removeAttribute("aria-invalid");
    startRun();
  });
  $("new-video").addEventListener("click", newVideo);
  $("stop-run").addEventListener("click", stopRun);
  $("retry-run").addEventListener("click", startRun);
  $("start-reading").addEventListener("click", beginReading);
  $("toggle-history").addEventListener("click", () => {
    const list = $("run-steps");
    const from = list.getBoundingClientRect().height;
    state.historyOpen = !state.historyOpen;
    renderSteps();
    if (prefersReduced()) return;
    const to = list.getBoundingClientRect().height;
    const anim = list.animate([{ height: `${from}px` }, { height: `${to}px` }], { duration: 240, easing: EASE });
    anim.onfinish = () => anim.cancel();
  });
  $("jump-latest").addEventListener("click", () => {
    const list = $("run-steps");
    list.scrollTo({ top: list.scrollHeight, behavior: prefersReduced() ? "auto" : "smooth" });
    $("jump-latest").hidden = true;
  });
  $("run-steps").addEventListener("scroll", () => {
    if (!state.historyOpen) return;
    const list = $("run-steps");
    const near = list.scrollHeight - list.scrollTop - list.clientHeight < 28;
    $("jump-latest").hidden = near || list.scrollHeight <= list.clientHeight + 8;
  });
  $("toggle-gen").addEventListener("click", () => {
    const open = $("gen-panel").dataset.open !== "true";
    $("gen-panel").dataset.open = open ? "true" : "false";
    $("toggle-gen").setAttribute("aria-expanded", String(open));
    $("toggle-gen").textContent = open ? "收起生成过程" : "查看生成过程";
  });
  $("map-toggle").addEventListener("click", () => {
    const open = $("map-panel").dataset.open !== "true";
    $("map-panel").dataset.open = open ? "true" : "false";
    $("map-toggle").textContent = open ? "收起" : "展开";
    $("map-toggle").setAttribute("aria-expanded", String(open));
  });
  document.querySelectorAll(".node").forEach((button) => {
    button.addEventListener("click", () => {
      const id = button.dataset.target;
      const target = $(id);
      state.mapLock = id;
      setMapCurrent(id);
      target.scrollIntoView({ behavior: prefersReduced() ? "auto" : "smooth", block: "start" });
      target.focus({ preventScroll: true });
      setTimeout(() => {
        if (state.mapLock === id) state.mapLock = null;
      }, 900);
    });
  });
  $("toggle-qa").addEventListener("click", () => setQa(!state.qaOpen, !state.qaOpen));
  $("qa-close").addEventListener("click", () => setQa(false));
  $("suggest").addEventListener("click", () => {
    $("composer").value = "为什么这里要先保存，再运行？";
    autoSize();
    paintSend();
    $("composer").focus();
  });
  $("composer-form").addEventListener("submit", (event) => {
    event.preventDefault();
    sendQuestion();
  });
  $("composer").addEventListener("input", () => {
    autoSize();
    paintSend();
  });
  $("composer").addEventListener("keydown", (event) => {
    if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      if (!$("send").hidden && !$("send").disabled) sendQuestion();
    }
  });
  $("stop-answer").addEventListener("click", stopAnswer);
  $("qa-log").addEventListener("click", (event) => {
    const summary = event.target.closest(".turn-summary");
    if (!summary) return;
    const message = state.messages.find((item) => item.id === summary.closest(".turn").dataset.id);
    if (!message || message.status === "running") return;
    message.open = !message.open;
    paintTurn(message);
  });

  document.addEventListener("click", (event) => {
    const ask = event.target.closest("[data-ask]");
    if (ask) {
      askFrame(ask.dataset.ask);
      return;
    }
    const frame = event.target.closest("[data-frame]");
    if (!frame || $("lightbox").contains(frame)) return;
    openLightbox(frame.dataset.frame, frame.querySelector(".shot-host") || frame);
  });

  $("lb-close").addEventListener("click", () => closeLightbox());
  $("lb-prev").addEventListener("click", () => {
    if (state.lightbox.index === 0) return;
    state.lightbox.index -= 1;
    renderLightbox();
  });
  $("lb-next").addEventListener("click", () => {
    if (state.lightbox.index >= FRAMES.length - 1) return;
    state.lightbox.index += 1;
    renderLightbox();
  });
  $("lightbox").addEventListener("cancel", (event) => {
    event.preventDefault();
    closeLightbox();
  });
  $("lightbox").addEventListener("click", (event) => {
    if (event.target === $("lightbox")) closeLightbox();
  });

  document.addEventListener("keydown", (event) => {
    if (!$("lightbox").open) return;
    if (event.key === "ArrowRight" && !$("lb-next").disabled) {
      event.preventDefault();
      state.lightbox.index += 1;
      renderLightbox();
    }
    if (event.key === "ArrowLeft" && !$("lb-prev").disabled) {
      event.preventDefault();
      state.lightbox.index -= 1;
      renderLightbox();
    }
  });

  $("ask-passage").addEventListener("mousedown", (event) => {
    event.preventDefault();
    const text = event.currentTarget.dataset.text || "";
    event.currentTarget.hidden = true;
    document.getSelection()?.removeAllRanges();
    askPassage(text);
  });
  document.addEventListener("mouseup", () => setTimeout(syncSelection, 0));
  document.addEventListener("scroll", () => {
    $("ask-passage").hidden = true;
  }, true);

  document.querySelectorAll(".proto-dock button").forEach((button) => {
    button.addEventListener("click", () => {
      const dest = button.dataset.jump;
      if (dest === "input") newVideo();
      if (dest === "run") {
        if (!$("link").value.trim()) $("link").value = SAMPLE;
        startRun();
      }
      if (dest === "read") {
        state.runToken += 1;
        state.answerToken += 1;
        clearPending();
        forceCloseLightbox();
        state.answering = false;
        state.messages = [];
        state.frameChip = null;
        state.passageChip = null;
        state.steps = [];
        $("qa-log").querySelectorAll(".turn").forEach((node) => node.remove());
        $("qa-empty").hidden = false;
        $("composer").value = "";
        beginReading();
        paintComposer();
      }
    });
  });

  const sections = ["scene", "steps", "cautions"];
  const observer = new IntersectionObserver((entries) => {
    if (state.mapLock || state.phase !== "reading") return;
    const visible = entries
      .filter((entry) => entry.isIntersecting)
      .sort((a, b) => b.intersectionRatio - a.intersectionRatio)[0];
    if (visible) setMapCurrent(visible.target.id);
  }, { rootMargin: "-12% 0px -55% 0px", threshold: [0.15, 0.4, 0.7] });
  for (const id of sections) observer.observe($(id));
}

function init() {
  state.frameChip = null;
  state.passageChip = null;
  $("article-shot").append(createShot("0524"));
  $("qa").inert = true;
  setPhase("input");
  bind();
  $("link").focus();
}

init();
