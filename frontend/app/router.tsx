import { Component, lazy, Suspense, useEffect, type ReactNode } from 'react'
import { createBrowserRouter, createRoutesFromElements, Outlet, Route, useLocation, useParams } from 'react-router'
import { ToastProvider } from '@/components/Toast'
import { IconSprite } from '@/components/ui/Icon'
import { LoadingBlock } from '@/components/ui/AsyncState'
import { ThemeProvider } from '@/components/theme/ThemeProvider'
import AppShell from '@/components/shell/AppShell'
import DashboardPage from '@/app/(main)/page'
import LoginPage from '@/app/login/page'
import IntroPage from '@/app/intro/page'
import DocsLayout from '@/app/docs/layout'
import DocsPage from '@/app/docs/page'
import DocsFeaturesPage from '@/app/docs/features/page'
import DocsConfigPage from '@/app/docs/config/page'
import DocsChangelogPage from '@/app/docs/changelog/page'
import NotFound from '@/app/not-found'
import RouteError from '@/app/error'

// Public pages load with the initial bundle; workspace pages load on demand.
const LibraryPage = lazy(() => import('@/app/(main)/library/page'))
const VideoPage = lazy(() => import('@/app/(main)/video/[id]/page'))
const KnowledgePage = lazy(() => import('@/app/(main)/kb/page'))
const KnowledgeDetailPage = lazy(() => import('@/app/(main)/kb/[id]/page'))
const RetrievalPage = lazy(() => import('@/app/(main)/kb/[id]/retrieval/page'))
const ChatPage = lazy(() => import('@/app/(main)/chat/page'))
const ChatLibraryPage = lazy(() => import('@/app/(main)/chat/library/page'))
const VideoChatPage = lazy(() => import('@/app/(main)/chat/v/[id]/page'))
const KnowledgeChatPage = lazy(() => import('@/app/(main)/chat/kb/[kbId]/page'))
const ArtifactsPage = lazy(() => import('@/app/(main)/artifacts/page'))
const ArtifactPage = lazy(() => import('@/app/(main)/artifacts/[id]/page'))
const TasksPage = lazy(() => import('@/app/(main)/tasks/page'))
const SettingsPage = lazy(() => import('@/app/(main)/settings/page'))
const ProductPreview = import.meta.env.DEV
  ? lazy(() => import('@/dev/ProductPreview').then(module => ({ default: module.ProductPreview })))
  : null

function useRouteSearchParams() {
  return Object.fromEntries(new URLSearchParams(useLocation().search))
}

function MainLayout() {
  const location = useLocation()

  return (
    <AppShell>
      <PageBoundary key={location.pathname}>
        <Suspense fallback={<div className="page"><LoadingBlock label="正在打开页面…" /></div>}>
          <Outlet />
        </Suspense>
      </PageBoundary>
    </AppShell>
  )
}

function DocsShell() {
  return <DocsLayout><Outlet /></DocsLayout>
}

// The old page components accept props. These small route adapters keep their
// contracts explicit while URL parsing stays in one place during migration.
function VideoRoute() {
  const { id = '' } = useParams()
  return <VideoPage params={{ id }} searchParams={useRouteSearchParams()} />
}

function KnowledgeDetailRoute() {
  const { id = '' } = useParams()
  return <KnowledgeDetailPage params={{ id }} />
}

function RetrievalRoute() {
  const { id = '' } = useParams()
  return <RetrievalPage params={{ id }} />
}

function VideoChatRoute() {
  const { id = '' } = useParams()
  return <VideoChatPage params={{ id }} searchParams={useRouteSearchParams()} />
}

function KnowledgeChatRoute() {
  const { kbId = '' } = useParams()
  return <KnowledgeChatPage params={{ kbId }} />
}

function ArtifactsRoute() {
  return <ArtifactsPage searchParams={useRouteSearchParams()} />
}

function ArtifactRoute() {
  const { id = '' } = useParams()
  return <ArtifactPage params={{ id }} searchParams={useRouteSearchParams()} />
}

function TasksRoute() {
  return <TasksPage searchParams={useRouteSearchParams()} />
}

function PreviewRoute() {
  const query = useRouteSearchParams()
  if (!ProductPreview) return <NotFound />
  return (
    <Suspense fallback={<div className="page">正在加载预览…</div>}>
      <ProductPreview view={query.view || 'notes'} />
    </Suspense>
  )
}

// Keep a route-level recovery UI for render errors in the SPA.
class PageBoundary extends Component<{ children: ReactNode }, { failed: Error | null }> {
  state: { failed: Error | null } = { failed: null }
  static getDerivedStateFromError(error: Error) {
    return { failed: error }
  }

  render(): ReactNode {
    if (!this.state.failed) return this.props.children
    return <RouteError error={this.state.failed} reset={() => this.setState({ failed: null })} />
  }
}

function DocumentTitle() {
  const location = useLocation()
  useEffect(() => {
    const path = location.pathname
    const section = path.startsWith('/docs') ? '使用文档'
      : path === '/intro' ? '项目介绍'
      : path === '/login' ? '登录'
      : path.startsWith('/video') || path === '/library' ? '视频库'
      : path.startsWith('/kb') ? '知识库'
      : path.startsWith('/chat') ? '问答'
      : path.startsWith('/artifacts') ? '成果'
      : path.startsWith('/tasks') ? '任务'
      : path.startsWith('/settings') ? '设置'
      : '工作台'
    document.title = `${section} · 映知 VidLens`
  }, [location.pathname])
  return null
}

function RootLayout() {
  return (
    <ThemeProvider>
      <ToastProvider>
        <IconSprite />
        <DocumentTitle />
        <Outlet />
      </ToastProvider>
    </ThemeProvider>
  )
}

const routes = createRoutesFromElements(
  <Route path="/" element={<RootLayout />}>
    <Route element={<MainLayout />}>
      <Route index element={<DashboardPage />} />
      <Route path="library" element={<LibraryPage />} />
      <Route path="video/:id" element={<VideoRoute />} />
      <Route path="kb" element={<KnowledgePage />} />
      <Route path="kb/:id" element={<KnowledgeDetailRoute />} />
      <Route path="kb/:id/retrieval" element={<RetrievalRoute />} />
      <Route path="chat" element={<ChatPage />} />
      <Route path="chat/library" element={<ChatLibraryPage />} />
      <Route path="chat/v/:id" element={<VideoChatRoute />} />
      <Route path="chat/kb/:kbId" element={<KnowledgeChatRoute />} />
      <Route path="artifacts" element={<ArtifactsRoute />} />
      <Route path="artifacts/:id" element={<ArtifactRoute />} />
      <Route path="tasks" element={<TasksRoute />} />
      <Route path="settings" element={<SettingsPage />} />
    </Route>
    <Route path="login" element={<LoginPage />} />
    <Route path="intro" element={<IntroPage />} />
    <Route path="docs" element={<DocsShell />}>
      <Route index element={<DocsPage />} />
      <Route path="features" element={<DocsFeaturesPage />} />
      <Route path="config" element={<DocsConfigPage />} />
      <Route path="changelog" element={<DocsChangelogPage />} />
    </Route>
    {import.meta.env.DEV && <Route path="dev/product" element={<PreviewRoute />} />}
    <Route path="*" element={<NotFound />} />
  </Route>,
)

export const router = createBrowserRouter(routes)
