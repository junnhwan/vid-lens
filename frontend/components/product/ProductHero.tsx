import Link from 'next/link'
import { Icon } from '@/components/ui/Icon'

export function ProductHero({ onImport }: { onImport: () => void }) {
  return <section className="product-hero">
    <div className="product-hero-copy">
      <p className="product-eyebrow">YOUR VIDEO KNOWLEDGE, CONNECTED</p>
      <h1>继续理解，也开始创造。</h1>
      <p>回到正在学习的视频，沿着证据继续提问。<br />让每一次观看，都留下可以重新使用的理解。</p>
      <div className="product-actions"><button className="btn btn-primary" onClick={onImport}><Icon name="plus" />导入一段视频</button><Link className="btn btn-ghost" href="/chat">继续研究<Icon name="chev-r" /></Link></div>
    </div>
    <div className="product-hero-art" aria-hidden="true"><span className="hero-orbit orbit-one" /><span className="hero-orbit orbit-two" /><span className="hero-orbit orbit-three" /><span className="hero-center"><Icon name="layers" /></span><span className="hero-label label-video"><Icon name="video" />原始视频</span><span className="hero-label label-insight"><Icon name="bulb" />新的理解</span><span className="hero-label label-evidence"><Icon name="link" />回到证据</span></div>
  </section>
}
