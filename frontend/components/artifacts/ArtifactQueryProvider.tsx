import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useState } from 'react'

// Mounted under the authenticated shell and keyed by owner. No cache survives an account switch.
export function ArtifactQueryProvider({ children }: { children: React.ReactNode }) {
  const [client] = useState(() => new QueryClient({ defaultOptions: {
    queries: { staleTime: 10_000, retry: false, refetchOnWindowFocus: true },
    mutations: { retry: false },
  } }))
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}
