// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { VideoPlayer } from './VideoPlayer'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  Reflect.deleteProperty(document, 'fullscreenElement')
})

function mockPlayback(video: HTMLVideoElement) {
  let paused = true
  Object.defineProperty(video, 'paused', { configurable: true, get: () => paused })
  const play = vi.spyOn(video, 'play').mockImplementation(() => { paused = false; return Promise.resolve() })
  const pause = vi.spyOn(video, 'pause').mockImplementation(() => { paused = true })
  return { play, pause }
}

test('lets the viewer adjust volume and mute or restore sound', () => {
  render(<VideoPlayer src="/sample.mp4" />)

  const volume = screen.getByRole('slider', { name: '音量' }) as HTMLInputElement
  fireEvent.change(volume, { target: { value: '0.35' } })
  expect(volume.value).toBe('0.35')

  fireEvent.click(screen.getByRole('button', { name: '静音' }))
  expect(screen.getByRole('button', { name: '取消静音' })).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '取消静音' }))
  expect(screen.getByRole('button', { name: '静音' })).toBeTruthy()
})

test('requests fullscreen for the whole player', () => {
  const { container } = render(<VideoPlayer src="/sample.mp4" />)
  const player = container.querySelector('.player-card') as HTMLDivElement
  const requestFullscreen = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(player, 'requestFullscreen', { configurable: true, value: requestFullscreen })

  fireEvent.click(screen.getByRole('button', { name: '全屏' }))
  expect(requestFullscreen).toHaveBeenCalledOnce()

  Object.defineProperty(document, 'fullscreenElement', { configurable: true, value: player })
  fireEvent(document, new Event('fullscreenchange'))
  expect(player.classList.contains('is-fullscreen')).toBe(true)
})

test('space plays and pauses without scrolling or toggling repeatedly while held', () => {
  const { container } = render(<VideoPlayer src="/sample.mp4" />)
  const media = mockPlayback(container.querySelector('video')!)
  const space = new KeyboardEvent('keydown', { key: ' ', code: 'Space', bubbles: true, cancelable: true })

  fireEvent(document.body, space)
  expect(media.play).toHaveBeenCalledOnce()
  expect(space.defaultPrevented).toBe(true)
  fireEvent.keyDown(document.body, { key: ' ', code: 'Space', repeat: true })
  expect(media.pause).not.toHaveBeenCalled()
  fireEvent.keyDown(document.body, { key: ' ', code: 'Space' })
  expect(media.pause).toHaveBeenCalledOnce()
})

test('space controls playback when the fullscreen button retains focus', () => {
  const { container } = render(<VideoPlayer src="/sample.mp4" />)
  const player = container.querySelector('.player-card') as HTMLDivElement
  const media = mockPlayback(container.querySelector('video')!)
  Object.defineProperty(document, 'fullscreenElement', { configurable: true, value: player })
  fireEvent(document, new Event('fullscreenchange'))
  const fullscreenButton = screen.getByRole('button', { name: '退出全屏' })
  fullscreenButton.focus()
  const space = new KeyboardEvent('keydown', { key: ' ', code: 'Space', bubbles: true, cancelable: true })

  fireEvent(fullscreenButton, space)
  expect(media.play).toHaveBeenCalledOnce()
  expect(space.defaultPrevented).toBe(true)
  expect(document.fullscreenElement).toBe(player)
})

test('space remains available for typing and buttons outside the player', () => {
  const { container } = render(<><VideoPlayer src="/sample.mp4" /><input /><textarea /><div contentEditable /><button>外部操作</button></>)
  const media = mockPlayback(container.querySelector('video')!)
  const targets = [...container.querySelectorAll('input:not([type="range"]), textarea, [contenteditable]'), screen.getByRole('button', { name: '外部操作' })]
  for (const target of targets) {
    const space = new KeyboardEvent('keydown', { key: ' ', code: 'Space', bubbles: true, cancelable: true })
    fireEvent(target, space)
    expect(space.defaultPrevented).toBe(false)
  }
  expect(media.play).not.toHaveBeenCalled()
})

test('space controls only the focused player when multiple videos are mounted', () => {
  const { container } = render(<><VideoPlayer src="/one.mp4" /><VideoPlayer src="/two.mp4" /></>)
  const videos = container.querySelectorAll('video')
  const first = mockPlayback(videos[0])
  const second = mockPlayback(videos[1])
  fireEvent.keyDown(document.body, { key: ' ', code: 'Space' })
  expect(first.play).not.toHaveBeenCalled()
  expect(second.play).not.toHaveBeenCalled()

  const secondPlayer = container.querySelectorAll<HTMLDivElement>('.player-card')[1]
  secondPlayer.focus()
  fireEvent.keyDown(secondPlayer, { key: ' ', code: 'Space' })
  expect(second.play).toHaveBeenCalledOnce()
  expect(first.play).not.toHaveBeenCalled()
})
