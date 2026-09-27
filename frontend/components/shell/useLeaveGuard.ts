import { useCallback, useEffect, useRef } from 'react'
import { useBlocker } from 'react-router'

type Guard = () => boolean

// SPA navigation goes through React Router. Browser reloads and external links
// need the separate beforeunload event, whose text is controlled by the browser.
export function useLeaveGuard() {
  const guardRef = useRef<Guard | null>(null)
  const registerLeaveGuard = useCallback((guard: Guard | null) => {
    guardRef.current = guard
  }, [])
  const confirmLeave = useCallback(() => guardRef.current?.() ?? true, [])
  const clearLeaveGuard = useCallback(() => {
    guardRef.current = null
  }, [])

  const blocker = useBlocker(({ currentLocation, nextLocation }) =>
    guardRef.current !== null &&
    (currentLocation.pathname !== nextLocation.pathname ||
      currentLocation.search !== nextLocation.search ||
      currentLocation.hash !== nextLocation.hash),
  )

  useEffect(() => {
    if (blocker.state !== 'blocked') return
    if (confirmLeave()) blocker.proceed()
    else blocker.reset()
  }, [blocker, confirmLeave])

  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (!guardRef.current) return
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', beforeUnload)
    return () => window.removeEventListener('beforeunload', beforeUnload)
  }, [])

  return { registerLeaveGuard, confirmLeave, clearLeaveGuard }
}
