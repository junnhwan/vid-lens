import { useRef, useState } from 'react'

export function IntroPromo() {
  const player = useRef<HTMLVideoElement>(null)
  const [started, setStarted] = useState(false)
  const [failed, setFailed] = useState(false)

  const play = async () => {
    const video = player.current
    if (!video) return
    setStarted(true)
    try {
      await video.play()
    } catch {
      setFailed(true)
    }
  }

  return (
    <div className="intro-promo">
      <div className="intro-promo-heading" aria-hidden="true">
        <span>01 / 看见知识如何留下来</span>
        <span>18 秒产品演示</span>
      </div>
      <div className="intro-promo-screen">
        <video
          ref={player}
          poster="/intro-promo-poster.jpg"
          preload="none"
          playsInline
          controls={started}
          onPlay={() => setStarted(true)}
          onError={() => setFailed(true)}
          aria-label="映知产品介绍视频：课程问答、引用回看与知识卡片"
          aria-describedby="intro-promo-description"
        >
          <source src="/intro-promo.mp4" type="video/mp4" />
          当前浏览器无法播放此视频。
        </video>
        {!started && !failed && (
          <button type="button" className="intro-promo-play" onClick={() => void play()} aria-label="播放 18 秒产品介绍视频">
            <span className="intro-promo-play-icon" aria-hidden="true">▶</span>
            <span>播放介绍短片 <small>00:18</small></span>
          </button>
        )}
        {failed && <p className="intro-promo-error" role="alert">视频暂时无法播放。<a href="/intro-promo.mp4">打开 MP4 文件</a></p>}
      </div>
      <div className="intro-promo-bottom">
        <p id="intro-promo-description">从一段课程里提问，沿引用回到原画面，再把要点留成可复盘的笔记。</p>
        <details className="intro-promo-summary">
          <summary>阅读视频内容</summary>
          <p>演示提出“为什么测试框架会影响模型表现”，引用定位到课程视频 02:04 的框架对比画面，随后把“保留推理消息、压缩旧消息”的要点整理成带来源的知识卡片。问答与卡片文字依据画面改写，并非逐字转录。</p>
        </details>
      </div>
    </div>
  )
}
