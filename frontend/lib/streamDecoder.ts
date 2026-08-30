export interface DecodedSSEEvent {
  event: string
  data: unknown
}

/** Incremental text/event-stream decoder shared by every conversation transport. */
export class SSEStreamDecoder {
  private readonly text = new TextDecoder()
  private buffer = ''

  push(chunk: Uint8Array): DecodedSSEEvent[] {
    this.buffer += this.text.decode(chunk, { stream: true })
    return this.drain(false)
  }

  finish(): DecodedSSEEvent[] {
    this.buffer += this.text.decode()
    return this.drain(true)
  }

  private drain(includeRemainder: boolean): DecodedSSEEvent[] {
    const events: DecodedSSEEvent[] = []
    let boundary = findBoundary(this.buffer)
    while (boundary) {
      const raw = this.buffer.slice(0, boundary.index)
      this.buffer = this.buffer.slice(boundary.index + boundary.length)
      const event = decodeFrame(raw)
      if (event) events.push(event)
      boundary = findBoundary(this.buffer)
    }
    if (includeRemainder) {
      const event = decodeFrame(this.buffer)
      this.buffer = ''
      if (event) events.push(event)
    }
    return events
  }
}

function findBoundary(value: string): { index: number; length: number } | undefined {
  const match = /\r?\n\r?\n/.exec(value)
  return match ? { index: match.index, length: match[0].length } : undefined
}

function decodeFrame(raw: string): DecodedSSEEvent | undefined {
  let event = 'message'
  const dataParts: string[] = []
  for (const line of raw.split(/\r?\n/)) {
    if (line.startsWith('event:')) event = line.slice(6).trim()
    else if (line.startsWith('data:')) dataParts.push(line.slice(5).trim())
  }
  const encoded = dataParts.join('\n')
  if (!encoded) return undefined
  try {
    return { event, data: JSON.parse(encoded) as unknown }
  } catch {
    return { event, data: encoded }
  }
}
