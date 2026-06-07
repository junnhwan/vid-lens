export function shouldShowInitialTaskSkeleton(tasks, initialLoading) {
  return initialLoading === true && Array.isArray(tasks) && tasks.length === 0
}
