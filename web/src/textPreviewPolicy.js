const TRUNCATION_SUFFIX = '...'

export function normalizeTextPreviewOptions(options = {}, defaults = {}) {
  return {
    maxLines: Number.isFinite(options.maxLines)
      ? Math.max(1, Math.floor(options.maxLines))
      : defaults.maxLines,
    maxChars: Number.isFinite(options.maxChars)
      ? Math.max(1, Math.floor(options.maxChars))
      : defaults.maxChars,
  }
}

function textToChars(text) {
  return Array.from(String(text ?? ''))
}

export function textNeedsExpansion(content, options) {
  const text = String(content ?? '')
  if (!text) return false

  const lines = text.split(/\r?\n/)
  return lines.length > options.maxLines || textToChars(text).length > options.maxChars
}

export function textPreview(content, options) {
  const text = String(content ?? '')
  if (!textNeedsExpansion(text, options)) return text

  const lines = text.split(/\r?\n/)
  let preview = text
  let truncated = false

  if (lines.length > options.maxLines) {
    preview = lines.slice(0, options.maxLines).join('\n')
    truncated = true
  }

  const chars = textToChars(preview)
  if (chars.length > options.maxChars) {
    preview = chars.slice(0, options.maxChars).join('').trimEnd()
    truncated = true
  }

  return truncated ? `${preview}${TRUNCATION_SUFFIX}` : preview
}

export function textForDisplay(content, expanded, options) {
  return expanded ? String(content ?? '') : textPreview(content, options)
}
