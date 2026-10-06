import React from 'react'

// Custom Markdown renderers own their children. Traverse intrinsic nodes only
// so processing a paragraph inside a list cannot wrap itself recursively.
export function renderCitationNodes(children: React.ReactNode, onCite?: (n: number) => void, activeCite?: number): React.ReactNode {
  return React.Children.map(children, child => {
    if (React.isValidElement<{ children?: React.ReactNode }>(child)) {
      if (child.props.children == null || typeof child.type !== 'string' || ['code', 'a', 'button'].includes(child.type)) return child
      return React.cloneElement(child, {}, renderCitationNodes(child.props.children, onCite, activeCite))
    }
    if (typeof child !== 'string') return child
    return child.split(/\[C(\d+)\]/g).map((part, index) => index % 2
      ? React.createElement(onCite ? 'button' : 'span', {
          className: `cite${activeCite === Number(part) ? ' selected' : ''}`, key: index,
          ...(onCite ? { type: 'button', 'aria-pressed': activeCite === Number(part), title: '查看证据详情', onClick: () => onCite(Number(part)) } : {}),
        }, `C${part}`)
      : part)
  })
}
