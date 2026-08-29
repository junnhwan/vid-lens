// 路由级 loading：路由段在加载时显示，避免空白闪屏。
// 与 TaskCardSkeleton 同语汇（shimmer 骨架），不用转圈 spinner。
export default function Loading() {
  return (
    <div className="flex-1 flex flex-col">
      {/* 顶栏占位 */}
      <div className="h-14 border-b border-ink-0/15 bg-paper-0 flex items-center px-6 gap-4">
        <div className="sk h-4 w-20" />
        <div className="flex-1" />
        <div className="sk h-6 w-24" />
      </div>
      <div className="flex-1 px-8 py-7">
        <div className="sk h-7 w-40 mb-6" />
        <div className="space-y-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="flex items-start gap-4 py-3.5">
              <div className="sk h-3 w-7 mt-1" />
              <div className="flex-1">
                <div className="sk h-3 w-24 mb-2" />
                <div className="sk h-4 w-2/3 mb-2" />
                <div className="sk h-2 w-1/2" />
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
