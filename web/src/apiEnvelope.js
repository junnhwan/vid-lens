export function unwrapApiResponse(response) {
  return response?.data?.data
}

export function normalizeListResponse(payload) {
  if (Array.isArray(payload)) {
    return payload
  }
  return payload?.list || []
}
