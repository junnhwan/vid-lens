export function summaryRevisionFailure(code?: string, fallback?: string): string {
  switch (code) {
    case 'anchor_ambiguous': return 'AI 未能准确定位要修改的文字。请说明所在章节或提供包含该名称的原句，再预览一次。'
    case 'invalid_patch': return 'AI 返回的修改未通过校验，请重新预览。'
    case 'protected_quote': return '修改涉及原话引用或代码。请指定只修改摘要中的叙述文字，再重新预览。'
    case 'nothing_to_change': return 'AI 没有找到可修改的内容。请补充原写法、正确写法或所在章节。'
    case 'version_conflict': return '摘要已有新版本，请刷新后重新预览。'
    case 'budget_exhausted': return '本次修订达到执行额度，请缩小修改范围后重试。'
    case 'profile_changed': return '本次使用的 AI 配置已变化，请重新预览。'
    case 'provider_error': return 'AI 服务暂时未能完成修订，请稍后重新预览。'
    default: return fallback || '摘要修订未完成，请重新预览；若仍失败，请补充所在章节或原句。'
  }
}
