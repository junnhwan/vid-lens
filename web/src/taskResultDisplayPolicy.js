import {
  normalizeTextPreviewOptions,
  textForDisplay,
  textNeedsExpansion,
} from './textPreviewPolicy.js'

export const DEFAULT_TRANSCRIPTION_PREVIEW_OPTIONS = {
  maxLines: 14,
  maxChars: 1800,
}

export const DEFAULT_SUMMARY_PREVIEW_OPTIONS = {
  maxLines: 12,
  maxChars: 1400,
}

/** 专注阅读时预览更长，少打断 */
export const FOCUS_TRANSCRIPTION_PREVIEW_OPTIONS = {
  maxLines: 28,
  maxChars: 4200,
}

export const FOCUS_SUMMARY_PREVIEW_OPTIONS = {
  maxLines: 24,
  maxChars: 3200,
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

/** 展开控件文案：全文 / 剩余字数 */
export function taskResultExpandMeta(content) {
  const text = String(content ?? '')
  const chars = Array.from(text).length
  const lines = text ? text.split(/\r?\n/).length : 0
  if (!chars) {
    return { chars: 0, lines: 0, label: '', shortLabel: '' }
  }
  const label =
    lines > 1
      ? `全文约 ${chars.toLocaleString('zh-CN')} 字 · ${lines} 行`
      : `全文约 ${chars.toLocaleString('zh-CN')} 字`
  return {
    chars,
    lines,
    label,
    shortLabel: `${chars.toLocaleString('zh-CN')} 字`,
  }
}

export function taskResultExpandButtonLabel(expanded, content) {
  if (expanded) return '收起'
  const meta = taskResultExpandMeta(content)
  if (!meta.chars) return '展开全部'
  return `展开全部 · ${meta.shortLabel}`
}
