import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router/dom'
import { router } from '@/app/router'
import '@/styles/tokens.css'
import '@/app/globals.css'
import '@/styles/product.css'
import '@/styles/artifacts.css'
import '@/styles/studio.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode><RouterProvider router={router} /></StrictMode>,
)
