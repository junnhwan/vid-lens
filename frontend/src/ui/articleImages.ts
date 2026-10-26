import type { Article, Frame } from "../contract/types"

export interface ArticleImage {
  frameId: string
  frame: Frame | undefined
  caption: string
  sectionId: string
  blockId: string
}

export interface ArticleImageGroup {
  sectionId: string
  sectionTitle: string
  chapter: number
  images: ArticleImage[]
}

// The frame map also contains unused candidates and private question screenshots.
// Only published figure references belong in the reading gallery.
export function articleImages(sections: Article["sections"], frames: Map<string, Frame>): ArticleImageGroup[] {
  const seen = new Set<string>()
  return sections.flatMap((section, index) => {
    const images: ArticleImage[] = []
    for (const block of section.blocks) {
      if (block.type !== "figure") continue
      for (const id of block.frame_ids ?? []) {
        if (seen.has(id)) continue
        seen.add(id)
        const frame = frames.get(id)
        images.push({ frameId: id, frame, caption: block.caption?.trim() || frame?.caption?.trim() || "视频画面", sectionId: section.id, blockId: block.id })
      }
    }
    return images.length ? [{ sectionId: section.id, sectionTitle: section.title, chapter: index + 1, images }] : []
  })
}
