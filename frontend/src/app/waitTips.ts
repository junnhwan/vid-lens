export type WaitTipKind = "agent" | "vidlens" | "craft"

export interface WaitTip {
  text: string
  kind: WaitTipKind
}

export const tipKindLabel: Record<WaitTipKind, string> = {
  agent: "Agent 手记",
  vidlens: "VidLens 做法",
  craft: "工程经验",
}

const statusTips: Record<string, WaitTip[]> = {
  queued: [
    { text: "排队也是流程的一部分：知道前面还有谁，等待就不焦躁。", kind: "craft" },
    { text: "准入不是拖延，是让每一单都有完整的处理窗口。", kind: "vidlens" },
  ],
  retry_wait: [
    { text: "自动恢复不是重来：已经取得的画面和进度会保留。", kind: "vidlens" },
    { text: "重试之前先弄清失败原因，否则只是把同一个错误再犯一次。", kind: "agent" },
  ],
  waiting_dependency: [
    { text: "有些步骤急不得：先把原视频找回来，才谈得上继续核对。", kind: "vidlens" },
    { text: "依赖缺失时，明确等待比硬着头皮往下走更可靠。", kind: "craft" },
  ],
}

const phaseTips: Record<string, WaitTip[]> = {
  prepare_source: [
    { text: "视频先完整落地，画面和文字才有对齐的可能。", kind: "vidlens" },
    { text: "转写是长活。与其催它，不如把进度摊开给人看。", kind: "craft" },
    { text: "来源确认得慢一点，后面的结论就能快一点。", kind: "agent" },
  ],
  overview: [
    { text: "先看全局，再决定哪几秒值得停下来。", kind: "vidlens" },
    { text: "上下文不是越多越好，能在需要时找回来才重要。", kind: "agent" },
    { text: "计划先写出来，执行才有被检查的可能。", kind: "craft" },
  ],
  investigate: [
    { text: "模型说的每一句，最好都能回到某一帧或某一句原话。", kind: "vidlens" },
    { text: "看见像素，比转述像素更接近事实。", kind: "vidlens" },
    { text: "证据不足时，先补证据，不要先补结论。", kind: "agent" },
  ],
  compose: [
    { text: "文章是给人读的，不是把执行日志换个排版。", kind: "vidlens" },
    { text: "结构先立住，句子才有地方安放。", kind: "craft" },
    { text: "让模型负责表达，让程序负责守边界。", kind: "agent" },
  ],
  validate: [
    { text: "写完不算完成，能逐条对回证据才算。", kind: "vidlens" },
    { text: "自检要具体：查引用、查画面、查有没有说过头。", kind: "agent" },
    { text: "能被验证的结论，才值得写进最终答案。", kind: "craft" },
  ],
  publish: [
    { text: "发布之后内容不再改动，所以核对放在最后一刻。", kind: "vidlens" },
    { text: "落库的进度比记忆可靠：失败之后，从事实继续。", kind: "craft" },
    { text: "把一次成功变成可重复的流程，才算真正完成。", kind: "agent" },
  ],
}

const generalTips: WaitTip[] = [
  { text: "好的 Agent 不急着回答，先确认自己看到了什么。", kind: "agent" },
  { text: "把步骤写出来，失败时才有人知道该从哪里接手。", kind: "craft" },
  { text: "工具调用应该是可观察的：做了什么、看了哪里、得到什么。", kind: "agent" },
  { text: "重试要有边界，否则只是在延长一次错误的决定。", kind: "agent" },
  { text: "不确定时明确说不确定，比补全一个漂亮答案更专业。", kind: "agent" },
  { text: "每一步都留下证据，最后才拼得出完整的解释。", kind: "craft" },
  { text: "多一次模型调用，不如多给一张真正相关的画面。", kind: "vidlens" },
  { text: "一条 18 分钟的视频，也值得被认真看完再动笔。", kind: "vidlens" },
  { text: "问答只围绕当前视频，不替整个互联网下结论。", kind: "vidlens" },
  { text: "慢并不可怕，可怕的是不知道慢在哪里。", kind: "craft" },
  { text: "先做可回滚的小步，再谈自动化。", kind: "craft" },
  { text: "进度可见，本身就是一种可靠性。", kind: "craft" },
]

export function waitTips(phase: string | null | undefined, status: string | null | undefined): WaitTip[] {
  return [
    ...(status ? statusTips[status] ?? [] : []),
    ...(phase ? phaseTips[phase] ?? [] : []),
    ...generalTips,
  ]
}
