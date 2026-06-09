import {
  normalizeTextPreviewOptions,
  textForDisplay,
  textNeedsExpansion,
} from './textPreviewPolicy.js'

export const DEFAULT_TRANSCRIPTION_PREVIEW_OPTIONS = {
  maxLines: 10,
  maxChars: 1200,
}

export const DEFAULT_SUMMARY_PREVIEW_OPTIONS = {
  maxLines: 8,
  maxChars: 900,
}

function normalizeOptions(options = {}) {
  return normalizeTextPreviewOptions(options, DEFAULT_TRANSCRIPTION_PREVIEW_OPTIONS)
}

export function taskResultNeedsExpansion(content, options = {}) {
  return textNeedsExpansion(content, normalizeOptions(options))
}

export function taskResultTextForDisplay(content, expanded, options = {}) {
  return textForDisplay(content, expanded, normalizeOptions(options))
}
