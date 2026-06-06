export function buildStoredUser(user, token) {
  return { ...user, token }
}

export function getStoredAuthToken(storage = localStorage) {
  const directToken = storage.getItem('token')
  if (directToken) return directToken

  try {
    const user = JSON.parse(storage.getItem('user') || '{}')
    return user.token || ''
  } catch {
    return ''
  }
}
