import { NextResponse } from 'next/server'

// Guard before App Router streaming begins so production returns an HTTP 404,
// not a streamed 200 containing a not-found page.
export function middleware() {
  if (process.env.NODE_ENV !== 'development') {
    return new NextResponse('Not found', { status: 404 })
  }
  return NextResponse.next()
}

export const config = { matcher: ['/dev/product/:path*'] }
