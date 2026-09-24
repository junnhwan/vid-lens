import { CardSkeleton } from '@/components/ui/AsyncState'

// 路由级 loading:视频卡网格骨架，贴合工作台/各页主布局。
export default function Loading() {
  return (
    <div className="page">
      <CardSkeleton count={4} />
    </div>
  )
}
