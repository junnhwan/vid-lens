// @vitest-environment jsdom
import { useEffect, useState } from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { transferableAbortController } from 'node:util'

// React Router uses Node's native Request; its signal must come from the same
// realm rather than JSDOM's AbortController. Browser behavior stays unchanged.
beforeEach(() => vi.stubGlobal('AbortController', transferableAbortController))
import { createMemoryRouter, Link, Outlet, RouterProvider, useLocation, useNavigate } from 'react-router'
import { useLeaveGuard } from './useLeaveGuard'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

function GuardedLayout() {
  const [dirty, setDirty] = useState(false)
  const navigate = useNavigate()
  const location = useLocation()
  const { registerLeaveGuard } = useLeaveGuard()

  useEffect(() => {
    registerLeaveGuard(dirty ? () => window.confirm('Discard changes?') : null)
    return () => registerLeaveGuard(null)
  }, [dirty, registerLeaveGuard])

  return (
    <>
      <output data-testid="location">{location.pathname}{location.search}</output>
      <button onClick={() => setDirty(true)}>Edit draft</button>
      <button onClick={() => setDirty(false)}>Save draft</button>
      <Link to="/next">Follow link</Link>
      <button onClick={() => navigate('/next')}>Open page</button>
      <button onClick={() => navigate('/next?view=detail')}>Change query</button>
      <button onClick={() => navigate(-1)}>Go back</button>
      <Outlet />
    </>
  )
}

function renderRoutes() {
  const router = createMemoryRouter([{
    element: <GuardedLayout />,
    children: [{ path: '/draft', element: <p>Draft</p> }, { path: '/next', element: <p>Next</p> }],
  }], { initialEntries: ['/draft'] })
  render(<RouterProvider router={router} />)
}

test('protects edits from links, programmatic navigation, query changes and back', async () => {
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
  renderRoutes()
  fireEvent.click(screen.getByText('Edit draft'))

  fireEvent.click(screen.getByText('Follow link'))
  await waitFor(() => expect(confirm).toHaveBeenCalledTimes(1))
  expect(screen.getByTestId('location').textContent).toBe('/draft')

  fireEvent.click(screen.getByText('Open page'))
  await waitFor(() => expect(confirm).toHaveBeenCalledTimes(2))
  expect(screen.getByTestId('location').textContent).toBe('/draft')

  confirm.mockReturnValue(true)
  fireEvent.click(screen.getByText('Open page'))
  await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('/next'))

  confirm.mockReturnValue(false)
  fireEvent.click(screen.getByText('Change query'))
  await waitFor(() => expect(confirm).toHaveBeenCalledTimes(4))
  expect(screen.getByTestId('location').textContent).toBe('/next')

  fireEvent.click(screen.getByText('Go back'))
  await waitFor(() => expect(confirm).toHaveBeenCalledTimes(5))
  expect(screen.getByTestId('location').textContent).toBe('/next')

  confirm.mockReturnValue(true)
  fireEvent.click(screen.getByText('Go back'))
  await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('/draft'))
})

test('warns on reload only while a draft is dirty and releases the guard after saving', async () => {
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
  renderRoutes()

  const cleanReload = new Event('beforeunload', { cancelable: true })
  window.dispatchEvent(cleanReload)
  expect(cleanReload.defaultPrevented).toBe(false)

  fireEvent.click(screen.getByText('Edit draft'))
  const dirtyReload = new Event('beforeunload', { cancelable: true })
  window.dispatchEvent(dirtyReload)
  expect(dirtyReload.defaultPrevented).toBe(true)

  fireEvent.click(screen.getByText('Save draft'))
  fireEvent.click(screen.getByText('Follow link'))
  await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('/next'))
  expect(confirm).not.toHaveBeenCalled()
})
