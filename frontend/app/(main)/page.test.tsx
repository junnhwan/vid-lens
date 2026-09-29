// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, test, vi } from 'vitest'
import { api } from '@/lib/api'
import { artifactApi } from '@/lib/artifacts/api'
import type { VideoTask } from '@/lib/types'
import DashboardPage from './page'

vi.mock('@/lib/router',()=>({default:({href,children,...props}:React.AnchorHTMLAttributes<HTMLAnchorElement>)=><a href={href} {...props}>{children}</a>,useRouter:()=>({push:vi.fn()})}))
vi.mock('@/components/shell/AppShell',()=>({useCrumb:vi.fn(),useShell:()=>({uploadRevision:0,openUpload:vi.fn()})}))
vi.mock('@/components/Toast',()=>({useToast:()=>({success:vi.fn(),error:vi.fn()})}))
vi.mock('@/components/VideoCard',()=>({VideoCard:({task}:{task:VideoTask})=><div>{task.title}</div>}))
vi.mock('@/components/artifacts/RecentProductWork',()=>({RecentProductWork:()=>null}))
afterEach(()=>{cleanup();vi.restoreAllMocks()})

test('history failure keeps videos and caps the action summary at three',async()=>{
  vi.spyOn(api,'listTasks').mockResolvedValue({list:Array.from({length:6},(_,i)=>({id:i+1,title:'待处理视频'+i,status:0,has_transcription:false,stage:'uploaded',visual_status:'not_started'} as VideoTask)),total:6,page:1,page_size:50})
  vi.spyOn(api,'listSessions').mockRejectedValue(new Error('offline'))
  vi.spyOn(artifactApi,'position').mockResolvedValue(null)
  const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
  render(<QueryClientProvider client={client}><DashboardPage /></QueryClientProvider>)
  expect(await screen.findByText('会话加载失败')).toBeTruthy()
  expect(screen.getByRole('heading',{name:'最近视频'})).toBeTruthy()
  expect(screen.getAllByRole('link',{name:'选择处理方式'})).toHaveLength(3)
  expect(screen.getAllByText('待处理视频0').length).toBeGreaterThan(0)
  client.clear()
})

test('a refresh error keeps the last loaded video and session visible',async()=>{
  vi.spyOn(api,'listTasks').mockRejectedValue(new Error('offline'))
  vi.spyOn(api,'listSessions').mockRejectedValue(new Error('offline'))
  vi.spyOn(artifactApi,'position').mockResolvedValue(null)
  const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
  client.setQueryData(['home-videos',0],{list:[{id:42,title:'上次读到的视频',status:3,has_transcription:true} as VideoTask],total:1})
  client.setQueryData(['home-sessions'],[{id:1,title:'上次读到的会话',task_id:42,scope_type:'video',updated_at:'2026-09-29T00:00:00Z'}])
  render(<QueryClientProvider client={client}><DashboardPage /></QueryClientProvider>)
  expect(await screen.findByText('视频资料加载失败')).toBeTruthy()
  expect(screen.getAllByText('上次读到的视频')).toHaveLength(2)
  expect(screen.getByRole('link',{name:'上次读到的会话'})).toBeTruthy()
  client.clear()
})

test('dashboard names an independent summary job in the action list', async () => {
  const task = { id: 91, title: '摘要中的视频', status: 3, stage: 'none', has_transcription: true,
    summary_job: { id: 92, job_type: 'summary', status: 1, stage: 'summarizing' } } as VideoTask
  vi.spyOn(api, 'listTasks').mockResolvedValue({ list: [task], total: 1, page: 1, page_size: 50 })
  vi.spyOn(api, 'listSessions').mockResolvedValue([])
  vi.spyOn(artifactApi, 'position').mockResolvedValue(null)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><DashboardPage /></QueryClientProvider>)
  expect(await screen.findByText('摘要排队中')).toBeTruthy()
  expect(screen.getByText(/近期有 1 个后台任务正在处理/)).toBeTruthy()
  expect(screen.getByRole('link', { name: '查看摘要进度' })).toBeTruthy()
  client.clear()
})
