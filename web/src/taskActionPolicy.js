export const TASK_STATUS = {
  PENDING: 0,
  QUEUED: 1,
  RUNNING: 2,
  COMPLETED: 3,
  FAILED: 4,
}

export function isTaskActionDisabled(task, isLoading = false) {
  return isLoading || task?.status === TASK_STATUS.QUEUED || task?.status === TASK_STATUS.RUNNING
}
