// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import type { VideoTask } from '@/lib/types'
import { TranscriptPanel } from './TranscriptPanel'

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals() })
beforeEach(() => { vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} }) })
const rows = [0, 1, 2].map(index => ({ id: String(index), modality: 'transcript', start_ms: index * 10000, end_ms: (index + 1) * 10000, time_range_status: 'exact', content: `第${index}段`, paragraphs: [`第${index}段`], source_ids: [String(index)] }))
const props = { task: { id: 42, has_transcription: true } as VideoTask, transcriptAtoms: rows, transcriptRows: rows, visualAtoms: [], timelineMs: 30000, playheadMs: 0, headSnap: false, seek: vi.fn(), children: null }
function pane(liveIndex: number) { return <div className="rail-pane" data-testid="pane"><TranscriptPanel {...props} liveIndex={liveIndex} /></div> }
function geometry() {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    if (this.classList.contains('rail-pane')) return new DOMRect(0, 0, 500, 400)
    const root = this.closest('.rail-pane')
    return new DOMRect(0, (this.textContent?.includes('第0段') ? 80 : 650) - (root?.scrollTop ?? 0), 400, 100)
  })
}

test('manual scrolling pauses following across subsequent sentences and can explicitly resume', () => {
  geometry()
  const { rerender } = render(pane(0))
  const root = screen.getByTestId('pane')
  const scroll = vi.fn((options?: ScrollToOptions | number, y?: number) => { root.scrollTop = typeof options === 'number' ? y ?? 0 : options?.top ?? 0 })
  root.scrollTo = scroll
  fireEvent.wheel(root, { deltaY: -100 })
  rerender(pane(1))
  expect(scroll).not.toHaveBeenCalled()
  expect(screen.getByText('自由阅读中')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '回到当前句' }))
  expect(scroll).toHaveBeenCalledWith(expect.objectContaining({ behavior: 'smooth' }))
  expect(screen.getByRole('button', { name: '暂停跟随' }).getAttribute('aria-pressed')).toBe('true')
})

test('reduced motion scrolls only the reading pane without a smooth transition', () => {
  geometry()
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true })))
  const { rerender } = render(pane(0))
  const root = screen.getByTestId('pane')
  const scroll = vi.fn()
  root.scrollTo = scroll
  rerender(pane(1))
  expect(scroll).toHaveBeenCalledWith(expect.objectContaining({ behavior: 'auto' }))
})

test('keyboard reading pauses following while a replay button still seeks its real timestamp', () => {
  const { rerender } = render(pane(0))
  fireEvent.keyDown(screen.getByLabelText('视频转写'), { key: 'PageUp' })
  rerender(pane(2))
  expect(screen.getByText('自由阅读中')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '回放 00:10' }))
  expect(props.seek).toHaveBeenCalledWith(10000)
})
