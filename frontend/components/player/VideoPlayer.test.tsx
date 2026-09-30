// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { VideoPlayer } from './VideoPlayer'

afterEach(() => { cleanup(); vi.restoreAllMocks() })

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
})
