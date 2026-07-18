import {
  normalizeTextPreviewOptions,
  textForDisplay,
  textNeedsExpansion,
  textPreview,
} from './textPreviewPolicy.js'


const INTERNAL_CITATION_CONTENT = /^[cC]\d+(?:\s*[,，、]\s*[cC]\d+)*$/

function delimiterRunLength(text, start, delimiter) {
  let end = start
  while (text[end] === delimiter) end += 1
  return end - start
}

function lineStart(text, index) {
  return text.lastIndexOf('\n', index - 1) + 1
}

function isFenceOpening(text, start, runLength) {
  if (runLength < 3) return false
  return /^ {0,3}$/.test(text.slice(lineStart(text, start), start))
}

function protectedCodeEnd(text, start) {
  const delimiter = text[start]
  const runLength = delimiterRunLength(text, start, delimiter)
  const fenced = isFenceOpening(text, start, runLength)
  if (delimiter === '~' && !fenced) return start

  const marker = delimiter.repeat(runLength)
  const closingStart = text.indexOf(marker, start + runLength)
  if (closingStart === -1) return fenced ? text.length : start
  return closingStart + runLength
}

function indentedCodeLineEnd(text, start) {
  if (start > 0 && text[start - 1] !== '\n') return start
  if (text[start] !== '\t' && !text.startsWith('    ', start)) return start

  const newline = text.indexOf('\n', start)
  return newline === -1 ? text.length : newline + 1
}

function isEscaped(text, index) {
  let backslashes = 0
  for (let cursor = index - 1; cursor >= 0 && text[cursor] === '\\'; cursor -= 1) {
    backslashes += 1
  }
  return backslashes % 2 === 1
}

function isReferenceDefinition(text, openBracket, closeBracket) {
  if (text[closeBracket + 1] !== ':') return false
  return /^ {0,3}$/.test(text.slice(lineStart(text, openBracket), openBracket))
}

function isReferenceTargetLabel(text, openBracket) {
  if (text[openBracket - 1] !== ']') return false
  const previousOpen = text.lastIndexOf('[', openBracket - 2)
  if (previousOpen === -1) return false

  const previousLabel = text.slice(previousOpen + 1, openBracket - 1)
  return previousLabel.length > 0 && !INTERNAL_CITATION_CONTENT.test(previousLabel)
}

function isMarkdownLinkLabel(text, openBracket, closeBracket) {
  const next = text[closeBracket + 1]
  if (next === '(' || isReferenceDefinition(text, openBracket, closeBracket)) return true
  if (isReferenceTargetLabel(text, openBracket)) return true
  if (next !== '[') return false

  const referenceEnd = text.indexOf(']', closeBracket + 2)
  if (referenceEnd === -1) return false
  const reference = text.slice(closeBracket + 2, referenceEnd)
  return !INTERNAL_CITATION_CONTENT.test(reference)
}

export function stripInternalCitationTokens(value) {
  if (typeof value !== 'string' || !value) return value || ''

  let visible = ''
  let cursor = 0
  while (cursor < value.length) {
    const indentedCodeEnd = indentedCodeLineEnd(value, cursor)
    if (indentedCodeEnd > cursor) {
      visible += value.slice(cursor, indentedCodeEnd)
      cursor = indentedCodeEnd
      continue
    }

    const char = value[cursor]
    if ((char === '`' || char === '~') && !isEscaped(value, cursor)) {
      const codeEnd = protectedCodeEnd(value, cursor)
      if (codeEnd > cursor) {
        visible += value.slice(cursor, codeEnd)
        cursor = codeEnd
        continue
      }
    }

    if (char === '[') {
      const closeBracket = value.indexOf(']', cursor + 1)
      if (closeBracket !== -1) {
        const content = value.slice(cursor + 1, closeBracket)
        const escaped = isEscaped(value, cursor)
        const nested = value[cursor - 1] === '[' || value[closeBracket + 1] === ']'
        if (
          INTERNAL_CITATION_CONTENT.test(content)
          && !nested
          && !escaped
          && !isMarkdownLinkLabel(value, cursor, closeBracket)
        ) {
          cursor = closeBracket + 1
          continue
        }
      }
    }

    visible += char
    cursor += 1
  }

  return visible
}

export function citationDisplayLabel(citationIndex) {
  const index = Number.isInteger(citationIndex) && citationIndex >= 0 ? citationIndex : 0
  return `[${index + 1}]`
}

export function citationDomId(messageId, citationIndex) {
  const stableMessageId = encodeURIComponent(String(messageId ?? 'message'))
  const index = Number.isInteger(citationIndex) && citationIndex >= 0 ? citationIndex : 0
  return `citation-${stableMessageId}-${index + 1}`
}

export const DEFAULT_CITATION_PREVIEW_OPTIONS = {
  maxLines: 3,
  maxChars: 220,
}

function normalizeOptions(options = {}) {
  return normalizeTextPreviewOptions(options, DEFAULT_CITATION_PREVIEW_OPTIONS)
}

export function citationNeedsExpansion(content, options = {}) {
  return textNeedsExpansion(content, normalizeOptions(options))
}

export function citationPreview(content, options = {}) {
  return textPreview(content, normalizeOptions(options))
}

export function citationTextForDisplay(content, expanded, options = {}) {
  return textForDisplay(content, expanded, normalizeOptions(options))
}

export function citationExpansionKey(messageId, citationIndex) {
  return `${messageId}:${citationIndex}`
}

export function areAllExpandableCitationsExpanded(expandedKeys, messageId, citations, options = {}) {
  const expandableIndexes = expandableCitationIndexes(citations, options)
  if (!expandableIndexes.length) return false
  return expandableIndexes.every((index) => expandedKeys.has(citationExpansionKey(messageId, index)))
}

export function setMessageCitationsExpanded(expandedKeys, messageId, citations, expanded, options = {}) {
  const nextKeys = new Set(expandedKeys)
  for (const index of expandableCitationIndexes(citations, options)) {
    const key = citationExpansionKey(messageId, index)
    if (expanded) {
      nextKeys.add(key)
    } else {
      nextKeys.delete(key)
    }
  }
  return nextKeys
}

function expandableCitationIndexes(citations, options) {
  if (!Array.isArray(citations)) return []
  return citations
    .map((citation, index) => (citationNeedsExpansion(citation?.content, options) ? index : null))
    .filter((index) => index !== null)
}
