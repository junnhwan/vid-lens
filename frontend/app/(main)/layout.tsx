import AppShell from '@/components/shell/AppShell'

// 登录后的主体区域(带 rail/topbar 外壳);/login 独立于本分组。
export default function MainLayout({ children }: { children: React.ReactNode }) {
  return <AppShell>{children}</AppShell>
}
