import type { Metadata } from 'next'
import Link from 'next/link'
import { BrandMark } from '@/components/ui/BrandMark'
import { Icon } from '@/components/ui/Icon'
import { IntroMotion } from './IntroMotion'
import { IntroStage } from './IntroStage'
import { RevealText } from './RevealText'
import './intro.css'

export const metadata: Metadata = {
  title: '映知 VidLens · 让视频成为可检索、可追问的知识库',
  description: '自动转写语音、识别画面、建立语义索引；提问后每条引用都能跳回原画面核对。',
}

const FLOW = [
  { icon: 'upload', title: '上传或导入', desc: '本地文件分片上传、断点续传；也可粘贴视频链接导入。重复文件自动复用已有结果。' },
  { icon: 'activity', title: '转写与画面证据', desc: '长音频分片转写，逐片可见进度与文字；可选生成关键帧 OCR 与画面描述。' },
  { icon: 'layers', title: '摘要与索引', desc: '长视频分段生成全片摘要；建立语义索引后，片段才可被检索与问答引用。' },
  { icon: 'message', title: '提问与追问', desc: '对单视频、整个视频库或知识库提问；Chat 直接作答，Agent 自主检索与跨视频比较。' },
  { icon: 'play', title: '回放验证', desc: '每条引用都带视频与时间，点击跳回原画面，回答可核对、可复盘。' },
] as const

const CAPABILITIES = [
  { icon: 'search', title: '多模态检索', desc: '转写、OCR、画面描述共同构成检索证据；引用保留来源与时间精度。' },
  { icon: 'target', title: 'Agent 问答', desc: '按预算自主调用工具、逐步分析；执行过程与证据覆盖实时可见。' },
  { icon: 'folder', title: '知识库', desc: '把视频编成主题分组，作为独立的检索与问答边界。' },
  { icon: 'cpu', title: '自有模型配置', desc: '对话、语音、向量、视觉四项能力分别接入自己的服务商，密钥加密保存。' },
  { icon: 'shield-check', title: '数据边界', desc: '检索、工具调用与发布均校验用户与成员范围；长期记忆可查看、可撤回。' },
  { icon: 'wand', title: '提示词偏好', desc: '查看各功能实际使用的指令，按功能保存自己的风格偏好。' },
] as const

export default function IntroPage() {
  return (
    <IntroMotion>
      <header className="intro-top">
        <Link href="/intro" className="intro-brand">
          <BrandMark size={30} />
          <span>
            <b>映知</b>
            <i>VIDLENS</i>
          </span>
        </Link>
        <nav className="intro-topnav">
          <Link href="/docs">文档</Link>
          <Link href="/docs/changelog">更新日志</Link>
          <Link href="/" className="intro-cta-sm">
            进入工作台
            <Icon name="arrow-r" size="sm" />
          </Link>
        </nav>
      </header>

      <main>
        <section className="intro-hero">
          <div className="intro-hero-copy rv">
            <p className="intro-kicker">观之以映 · 释之以知</p>
            <h1>
              <RevealText delay={120}>
                让视频成为可检索、可追问、<em>可回放验证</em>的知识库
              </RevealText>
            </h1>
            <p className="intro-lead">
              映知 VidLens 把你的视频变成可以对话的内容：自动转写语音、识别画面、建立语义索引；随后直接提问，或让
              Agent 跨视频检索比较——每个结论都能沿着引用跳回原画面核对。
            </p>
            <div className="intro-actions">
              <Link href="/" className="btn btn-primary intro-cta">
                进入工作台
                <Icon name="arrow-r" size="sm" />
              </Link>
              <Link href="/docs" className="btn intro-cta-ghost">
                阅读使用文档
              </Link>
            </div>
          </div>
          <div className="intro-hero-stage rv">
            <IntroStage />
          </div>
        </section>

        <section className="intro-sec rv">
          <div className="intro-sec-head">
            <h2 className="intro-h2">它解决什么</h2>
            <p className="intro-sec-lead">
              看过的课程、会议和访谈散落在视频文件里，想找某句话只能凭记忆拖动进度条。映知把「看」变成「问」：内容先被整理成带时间的文字与画面证据，之后用自然语言检索和追问，答案始终指向原片位置。
            </p>
          </div>
        </section>

        <section className="intro-sec rv">
          <h2 className="intro-h2">典型使用流程</h2>
          <div className="intro-flow-wrap">
            <svg className="intro-flow-link" viewBox="0 0 1000 44" preserveAspectRatio="none" aria-hidden="true">
              <path pathLength={1} d="M 70 36 C 180 36, 200 6, 268 6 C 336 6, 344 36, 468 36 C 560 36, 566 6, 668 6 C 770 6, 786 36, 930 36" />
            </svg>
            <ol className="intro-flow">
              {FLOW.map((s, i) => (
                <li key={s.title} className="rv-item">
                  <span className="intro-flow-no mono">{String(i + 1).padStart(2, '0')}</span>
                  <span className="intro-flow-icon">
                    <Icon name={s.icon} />
                  </span>
                  <b>{s.title}</b>
                  <p>{s.desc}</p>
                </li>
              ))}
            </ol>
          </div>
          <p className="intro-note">
            上传后处理不会自动开始：进入视频详情手动启动转写，之后按需生成摘要、建立索引。每一步的状态和等待原因都会显示。
          </p>
        </section>

        <section className="intro-sec rv">
          <h2 className="intro-h2">核心能力</h2>
          <div className="intro-grid">
            {CAPABILITIES.map(c => (
              <article key={c.title} className="intro-card rv-item">
                <span className="intro-card-icon">
                  <Icon name={c.icon} />
                </span>
                <h3>{c.title}</h3>
                <p>{c.desc}</p>
              </article>
            ))}
          </div>
          <p className="intro-note">
            完整功能说明见 <Link href="/docs/features">文档 · 功能说明</Link>。
          </p>
        </section>

        <section className="intro-sec intro-final rv">
          <h2 className="intro-h2">现在开始</h2>
          <p className="intro-sec-lead">登录工作台上传第一段视频，或先阅读快速开始了解配置方式。</p>
          <div className="intro-actions">
            <Link href="/" className="btn btn-primary intro-cta">
              进入工作台
              <Icon name="arrow-r" size="sm" />
            </Link>
            <Link href="/docs" className="btn intro-cta-ghost">
              快速开始
            </Link>
          </div>
        </section>
      </main>

      <footer className="intro-foot">
        <span>映知 VidLens · 视频知识库与问答</span>
        <span className="intro-foot-links">
          <Link href="/docs">文档</Link>
          <Link href="/docs/config">配置与常见问题</Link>
          <Link href="/docs/changelog">更新日志</Link>
        </span>
      </footer>
    </IntroMotion>
  )
}
