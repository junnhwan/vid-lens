// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { VideoStill } from '@/components/VideoPoster'
import { VideoPlayer } from '@/components/player/VideoPlayer'
import { useStudyPosition } from '@/lib/artifacts/useStudyPosition'

const f = vi.hoisted(() => ({ playback: vi.fn(), position: vi.fn(), save: vi.fn() }))
vi.mock('@/lib/api', () => ({ api: { playbackSrc: f.playback } }))
vi.mock('@/lib/artifacts/api', () => ({ artifactApi: { position: f.position, savePosition: f.save } }))
beforeEach(() => {
  f.playback.mockReset(); f.position.mockReset(); f.save.mockReset()
  vi.stubGlobal('IntersectionObserver', class {
    constructor(private callback: (entries: unknown[]) => void) {}
    observe() { this.callback([{ isIntersecting: true }]) }
    disconnect() {}
  })
})
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

test('poster retries a temporary playback lookup failure after remount', async () => {
  f.playback.mockRejectedValueOnce(new Error('temporary network failure')).mockResolvedValue('/fresh-source')
  const a = render(<VideoStill taskId={980001} fallbackTitle="probe" />)
  await waitFor(() => expect(f.playback).toHaveBeenCalledTimes(1))
  a.unmount()
  const b = render(<VideoStill taskId={980001} fallbackTitle="probe" />)
  await waitFor(() => expect(f.playback).toHaveBeenCalledTimes(2))
  await waitFor(() => expect(b.container.querySelector('video')?.getAttribute('src')).toBe('/fresh-source'))
})

test('poster evicts an expired media source on video error', async () => {
  f.playback.mockResolvedValueOnce('/expired-source').mockResolvedValue('/fresh-source')
  const a = render(<VideoStill taskId={980002} fallbackTitle="probe" />)
  await waitFor(() => expect(a.container.querySelector('video')).toBeTruthy())
  fireEvent.error(a.container.querySelector('video')!)
  a.unmount()
  const b = render(<VideoStill taskId={980002} fallbackTitle="probe" />)
  await waitFor(() => expect(b.container.querySelector('video')?.getAttribute('src')).toBe('/fresh-source'))
  expect(f.playback).toHaveBeenCalledTimes(2)
})

test('player offers retry after its initial source request fails', async () => {
  const refresh=vi.fn().mockResolvedValue('/restored-source')
  render(<VideoPlayer src={null} onNeedRefresh={refresh} />)
  expect((screen.getByRole('button', {name:'播放'}) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.click(screen.getByRole('button', { name: '重新读取播放源' }))
  await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1))
  await waitFor(() => expect((screen.getByRole('button', {name:'播放'}) as HTMLButtonElement).disabled).toBe(false))
})

function StudyProbe() {
  const study=useStudyPosition()
  return <><output>{study.error}</output><button onClick={() => study.record({ task_id: 63, artifact_id: '', version_id: '', block_id: '', time_ms: 60000 })}>Record</button><button onClick={() => void study.flush()}>Flush</button></>
}
test('learning position recovers initial read and saves subsequent progress', async () => {
  f.position.mockRejectedValueOnce(new Error('temporary network failure')).mockResolvedValue(null)
  f.save.mockResolvedValue({revision:1})
  render(<StudyProbe />)
  await waitFor(() => expect(screen.getByText('学习位置暂时无法同步')).toBeTruthy())
  fireEvent.click(screen.getByText('Record'))
  fireEvent.click(screen.getByText('Flush'))
  await waitFor(() => expect(f.position).toHaveBeenCalledTimes(2))
  await waitFor(() => expect(f.save).toHaveBeenCalledWith(expect.objectContaining({ task_id: 63, time_ms: 60000, expected_revision: 0 })))
})
