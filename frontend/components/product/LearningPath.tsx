import { Icon, type IconName } from '@/components/ui/Icon'

const steps: { icon: IconName; title: string; detail: string }[] = [
  { icon: 'video', title: '导入视频', detail: '把课程与灵感收进来' },
  { icon: 'file', title: '阅读内容', detail: '沿着时间轴找到重点' },
  { icon: 'message', title: '带着问题看', detail: '结合原文理解答案' },
  { icon: 'layers', title: '留下笔记', detail: '把理解连成一张图' },
]

/** An explanatory route through the product, never a processing progress bar. */
export function LearningPath({ compact = false }: { compact?: boolean }) {
  return <ol className={`learning-path${compact ? ' compact' : ''}`} aria-label="从视频到笔记的学习流程">
    {steps.map((step, index) => <li key={step.title}>
      <span className="learning-path-icon"><Icon name={step.icon} /></span>
      <div><span className="learning-path-number">0{index + 1}</span><strong>{step.title}</strong><p>{step.detail}</p></div>
    </li>)}
  </ol>
}
