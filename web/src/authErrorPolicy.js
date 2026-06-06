const AUTH_ENDPOINTS = new Set(['/user/login', '/user/register'])

export function shouldResetSessionOnUnauthorized(requestUrl = '') {
  const normalizedPath = normalizeApiPath(requestUrl)
  return !AUTH_ENDPOINTS.has(normalizedPath)
}

function normalizeApiPath(requestUrl) {
  try {
    const parsed = new URL(requestUrl, 'http://vidlens.local')
    return parsed.pathname.replace(/^\/api\/v1/, '')
  } catch {
    return String(requestUrl).split('?')[0].replace(/^\/api\/v1/, '')
  }
}
