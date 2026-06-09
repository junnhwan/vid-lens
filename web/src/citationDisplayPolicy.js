import {
  normalizeTextPreviewOptions,
  textForDisplay,
  textNeedsExpansion,
  textPreview,
} from './textPreviewPolicy.js'

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
