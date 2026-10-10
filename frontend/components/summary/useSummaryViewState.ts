import { useCallback, useEffect, useSyncExternalStore } from 'react'
import { getSummarySessionView, getSummaryTaskView, patchSummarySessionView, patchSummaryTaskView, setSummaryViewOwner, subscribeSummaryViewState, type SummarySessionView, type SummaryTaskView } from '@/lib/summaryViewState'

export function useSummaryTaskView(userID: number | undefined, taskID: number) {
  useEffect(() => {
    if (!userID) return
    setSummaryViewOwner(userID)
    const raw = new URLSearchParams(window.location.search).get('session')
    const id = raw ? Number(raw) : 0
    if (Number.isSafeInteger(id) && id > 0) patchSummaryTaskView(userID, taskID, { sessionID: id })
  }, [userID, taskID])
  const snapshot = useCallback(() => getSummaryTaskView(userID || 0, taskID), [userID, taskID])
  const state = useSyncExternalStore(subscribeSummaryViewState, snapshot, snapshot)
  const patch = useCallback((value: Partial<SummaryTaskView>) => { if (userID) patchSummaryTaskView(userID, taskID, value) }, [userID, taskID])
  return [state, patch] as const
}

export function useSummarySessionView(userID: number | undefined, taskID: number, sessionID: number | null) {
  const snapshot = useCallback(() => getSummarySessionView(userID || 0, taskID, sessionID), [userID, taskID, sessionID])
  const state = useSyncExternalStore(subscribeSummaryViewState, snapshot, snapshot)
  const patch = useCallback((value: Partial<SummarySessionView>) => { if (userID) patchSummarySessionView(userID, taskID, sessionID, value) }, [userID, taskID, sessionID])
  return [state, patch] as const
}
