// 路由级 loading:卡片骨架占位。
export default function Loading() {
  return (
    <div className="page">
      <div style={{ display: 'grid', gap: 12 }}>
        <div className="card" style={{ height: 120 }} />
        <div className="card" style={{ height: 64 }} />
        <div className="card" style={{ height: 64 }} />
        <div className="card" style={{ height: 64 }} />
      </div>
    </div>
  )
}
