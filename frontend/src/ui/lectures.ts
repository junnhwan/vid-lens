export type Lecture = {
  id: string
  kind: string
  title: string
  channel: string
  duration: string
  cover: string
  coverAlt: string
  hint: string
  url: string
  purpose: string
}

export const lectures: Lecture[] = [
  {
    id: "poteto-pstack",
    kind: "工程访谈",
    title: "Poteto：数千个 PR 如何交付？",
    channel: "Matt Pocock × Poteto",
    duration: "65:35",
    cover: "/lectures/poteto-pstack.jpg",
    coverAlt: "Poteto 访谈的原视频封面，两位对谈者的卡通形象",
    hint: "聊聊 pstack、AI 编程与工程验证。",
    url: "https://www.youtube.com/watch?v=MN9dGgmLyso",
    purpose: "请完整整理 Poteto 关于 pstack、AI 编程与工程验证的主要观点，解释工作流程、前提和限制，保留具体例子与结尾的讨论。",
  },
  {
    id: "theo-react-native",
    kind: "技术评论",
    title: "AI 改变了 React Native 的选择吗？",
    channel: "Theo · t3.gg",
    duration: "62:52",
    cover: "/lectures/theo-react-native.jpg",
    coverAlt: "Did AI Kill React Native 原视频封面",
    hint: "从 Shopify 的选择，聊到原生、Expo 与 OTA。",
    url: "https://www.youtube.com/watch?v=oPZLPUtmROo",
    purpose: "请完整整理视频对 React Native、原生开发、Expo 与 OTA 的讨论，解释 Shopify 的选择、各方观点及适用条件，保留争议和限制。",
  },
  {
    id: "cmu-agents-intro",
    kind: "CMU · 第 1 讲",
    title: "智能体是什么，又如何工作？",
    channel: "CMU AI Agents · Graham Neubig",
    duration: "67:52",
    cover: "/lectures/cmu-agents-intro.jpg",
    coverAlt: "CMU 智能体课程的标题课件与讲堂画面",
    hint: "模型、工具与循环，从基础概念进入智能体。",
    url: "https://www.youtube.com/watch?v=UwfjzyLnvMg",
    purpose: "请完整整理本讲对智能体的定义、模型与工具如何形成循环、训练方式和课程目标，结合有解释价值的课件，保留概念之间的关系。",
  },
  {
    id: "stanford-verification",
    kind: "Stanford · 第 3 讲",
    title: "怎样验证智能体真的做对了？",
    channel: "Stanford CS329A",
    duration: "72:58",
    cover: "/lectures/stanford-verification.jpg",
    coverAlt: "Stanford 鲁棒验证课程的讲者与课件画面",
    hint: "从训练验证器，到验证器集成与可靠性。",
    url: "https://www.youtube.com/watch?v=p7TdPUcPoik",
    purpose: "请完整解释本讲的鲁棒验证方法，对比训练验证器与验证器集成的思路、实验结论和限制，结合有解释价值的课件。",
  },
]

function videoKey(value: string) {
  const text = value.trim()
  if (!text) return ""
  try {
    const url = new URL(text)
    const host = url.hostname.replace(/^www\./, "")
    if (host === "youtu.be") return url.pathname.split("/").filter(Boolean)[0] ?? ""
    if (host === "youtube.com" || host === "m.youtube.com" || host === "music.youtube.com") return url.searchParams.get("v") ?? ""
  } catch {
    return text
  }
  return text
}

export function sameVideo(link: string, url: string) {
  const key = videoKey(link)
  return key !== "" && key === videoKey(url)
}
