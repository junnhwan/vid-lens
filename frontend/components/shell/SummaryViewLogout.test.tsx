// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { transferableAbortController } from 'node:util'
import AppShell from './AppShell'
import { clearSummaryViewState, getSummarySessionView, patchSummarySessionView, setSummaryViewOwner } from '@/lib/summaryViewState'

const mocks = vi.hoisted(() => ({ clearToken: vi.fn() }))
vi.mock('@/lib/api', () => ({ api: { profile: vi.fn().mockResolvedValue({ id: 7, username: 'owner', role: 'USER' }) }, getToken: () => 'test-auth', clearToken: mocks.clearToken }))
vi.mock('./ShellFrame', () => ({ ShellFrame: ({ children, onLogout, user }: { children: React.ReactNode; onLogout: () => void; user: { name: string } }) => <div>{user.name}<button onClick={onLogout}>注销</button>{children}</div> }))
vi.mock('@/components/UploadModal', () => ({ default: () => null }))
vi.mock('@/components/artifacts/ArtifactQueryProvider', () => ({ ArtifactQueryProvider: ({ children }: { children: React.ReactNode }) => children }))
vi.mock('@/components/settings/VideoAIPreflight', () => ({ useVideoAIPreflight: () => ({ request: vi.fn(), dialog: null }) }))
beforeEach(() => vi.stubGlobal('AbortController', transferableAbortController))
afterEach(() => { cleanup(); clearSummaryViewState(); vi.clearAllMocks(); vi.unstubAllGlobals() })
test('actual shell logout clears temporary views before redirect, including same-account re-login', async () => {
  const router = createMemoryRouter([{ path: '/video/42', element: <AppShell><p>视频工作区</p></AppShell> }, { path: '/login', element: <p>登录页</p> }], { initialEntries: ['/video/42'] })
  render(<RouterProvider router={router} />)
  await screen.findByText('owner')
  await waitFor(() => { patchSummarySessionView(7, 42, 9, { draft: '私有草稿' }); expect(getSummarySessionView(7, 42, 9).draft).toBe('私有草稿') })
  fireEvent.click(screen.getByRole('button', { name: '注销' }))
  expect(mocks.clearToken).toHaveBeenCalledOnce()
  await screen.findByText('登录页')
  expect(router.state.location.pathname).toBe('/login')
  patchSummarySessionView(7, 42, 9, { draft: '迟到写入' })
  setSummaryViewOwner(7)
  expect(getSummarySessionView(7, 42, 9).draft).toBe('')
})
