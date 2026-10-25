export interface BlockHit {
  sectionId: string
  blockId: string
  quote: string
}

export function selectionPopover(rect: Pick<DOMRect, "left" | "width" | "top" | "bottom">, viewportWidth: number, viewportHeight: number) {
  const left = Math.max(12, Math.min(rect.left + rect.width / 2 - 48, viewportWidth - 108))
  const preferredTop = rect.top >= 106 ? rect.top - 38 : rect.bottom + 8
  const top = Math.max(72, Math.min(preferredTop, viewportHeight - 44))
  return { left, top }
}

function quoteIn(block: HTMLElement, range: Range): string {
  // Keep the DOM projection aligned with blockPlainText: UI controls are not
  // article text, and list/table cell boundaries are whitespace separators.
  const groups = block.matches("table") ? [...block.querySelectorAll<HTMLElement>("th, td")]
    : block.matches("ol, ul") ? [...block.children].filter((child): child is HTMLElement => child instanceof HTMLElement && child.matches("li"))
    : block.matches("figure") ? [...block.querySelectorAll<HTMLElement>("figcaption > span:first-child")]
    : [block]
  return groups.map((group) => quoteInGroup(group, range)).filter((quote) => quote.trim()).join("\n")
}

function quoteInGroup(group: HTMLElement, range: Range): string {
  const walker = document.createTreeWalker(group, NodeFilter.SHOW_TEXT)
  let text = ""
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    if ((node as Text).parentElement?.closest("[data-citation], .cite-cluster, button")) continue
    if (!range.intersectsNode(node)) continue
    const content = node.textContent ?? ""
    if (node === range.startContainer && node === range.endContainer) text += content.slice(range.startOffset, range.endOffset)
    else if (node === range.startContainer) text += content.slice(range.startOffset)
    else if (node === range.endContainer) text += content.slice(0, range.endOffset)
    else text += content
  }
  return text
}

export function hitsInSelection(root: HTMLElement): { hits: BlockHit[]; rect: DOMRect | null } {
  const selection = document.getSelection()
  if (!selection || selection.isCollapsed || selection.rangeCount === 0) return { hits: [], rect: null }
  const range = selection.getRangeAt(0)
  if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return { hits: [], rect: null }
  const hits: BlockHit[] = []
  for (const block of root.querySelectorAll<HTMLElement>("[data-block-id]")) {
    if (!range.intersectsNode(block)) continue
    const quote = quoteIn(block, range)
    if (!quote.trim()) continue
    hits.push({ sectionId: block.dataset.sectionId || "", blockId: block.dataset.blockId || "", quote })
  }
  return { hits, rect: hits.length ? range.getBoundingClientRect() : null }
}
