// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { TextSourceRefreshDialog } from './TextSourceRefreshDialog'
import type { VideoTask } from '@/lib/types'

const mocks=vi.hoisted(()=>({profiles:vi.fn(),generation:vi.fn(),refresh:vi.fn()}))
vi.mock('@/lib/api',()=>({api:{listProfiles:mocks.profiles},ApiError:class ApiError extends Error{status=409}}))
vi.mock('@/lib/summaryExperience',()=>({summaryExperienceApi:{generation:mocks.generation,refreshSource:mocks.refresh}}))
vi.mock('@/components/ui/Modal',()=>({Modal:({children,footer}:{children:React.ReactNode;footer:React.ReactNode})=><div>{children}{footer}</div>}))
const task={id:42,active_text_source_id:'old-source',source_type:'url'} as VideoTask
beforeEach(()=>{vi.resetAllMocks();mocks.profiles.mockResolvedValue([{id:3,name:'新的配置'}]);mocks.generation.mockResolvedValue({legacy:false})})
afterEach(cleanup)
test('source refresh is explicit and ambiguous retries reuse one key and frozen body',async()=>{
 const accepted=vi.fn();mocks.refresh.mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce({accepted:true})
 render(<TextSourceRefreshDialog task={task} readOnly={false} onClose={vi.fn()} onAccepted={accepted}/>)
 await waitFor(()=>expect((screen.getByRole('button',{name:'开始刷新'}) as HTMLButtonElement).disabled).toBe(false))
 expect(mocks.refresh).not.toHaveBeenCalled()
 fireEvent.change(screen.getByLabelText('刷新处理配置'),{target:{value:'3'}})
 fireEvent.click(screen.getByRole('button',{name:'开始刷新'}))
 await screen.findByRole('alert')
 const first=mocks.refresh.mock.calls[0]
 expect(first[1]).toEqual({expected_source_id:'old-source',text_source_policy:'prefer_platform',profile_id:3,auto_summary:true})
 fireEvent.click(screen.getByRole('button',{name:'开始刷新'}))
 await waitFor(()=>expect(accepted).toHaveBeenCalledOnce())
 expect(mocks.refresh.mock.calls[1].slice(0,3)).toEqual(first.slice(0,3))
})
test('changing task aborts the old request and never announces its late receipt for the new task',async()=>{
 let resolve:(value:unknown)=>void=()=>{}
 mocks.refresh.mockImplementation(()=>new Promise(run=>{resolve=run}))
 const accepted=vi.fn(),props={task,readOnly:false,onClose:vi.fn(),onAccepted:accepted}
 const view=render(<TextSourceRefreshDialog {...props}/>)
 await waitFor(()=>expect((screen.getByRole('button',{name:'开始刷新'}) as HTMLButtonElement).disabled).toBe(false))
 fireEvent.click(screen.getByRole('button',{name:'开始刷新'}));fireEvent.click(screen.getByRole('button',{name:'正在提交…'}))
 expect(mocks.refresh).toHaveBeenCalledOnce()
 const signal=mocks.refresh.mock.calls[0][3] as AbortSignal
 view.rerender(<TextSourceRefreshDialog {...props} task={{...task,id:43}}/>)
 expect(signal.aborted).toBe(true)
 resolve({accepted:true});await waitFor(()=>expect((screen.getByRole('button',{name:'开始刷新'}) as HTMLButtonElement).disabled).toBe(false))
 expect(accepted).not.toHaveBeenCalled()
})
test('legacy and read-only tasks cannot be upgraded or submitted by opening the dialog',async()=>{
 mocks.generation.mockResolvedValue({legacy:true})
 const props={task,readOnly:false,onClose:vi.fn(),onAccepted:vi.fn()}
 const view=render(<TextSourceRefreshDialog {...props}/>)
 await screen.findByText(/此视频使用较早的处理方式/)
 fireEvent.click(screen.getByRole('button',{name:'开始刷新'}));expect(mocks.refresh).not.toHaveBeenCalled()
 view.unmount();mocks.generation.mockResolvedValue({legacy:false})
 render(<TextSourceRefreshDialog {...props} readOnly/>);await screen.findByRole('option',{name:'新的配置'})
 fireEvent.click(screen.getByRole('button',{name:'开始刷新'}));expect(mocks.refresh).not.toHaveBeenCalled()
})
