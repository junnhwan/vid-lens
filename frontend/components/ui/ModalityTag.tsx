// 证据模态标签:与原型 .tag-modality 一致(transcript / visual_ocr / visual_caption)。
// 未知模态按"转写"的灰色处理,不虚构新颜色。

const MODALITY_VIEW: Record<string, { cls: string; text: string }> = {
  transcript: { cls: 'tag-transcript', text: '转写' },
  visual_ocr: { cls: 'tag-ocr', text: '画面 OCR' },
  visual_caption: { cls: 'tag-caption', text: '画面描述' },
}

export function modalityView(modality?: string): { cls: string; text: string } {
  return MODALITY_VIEW[modality || ''] || MODALITY_VIEW.transcript
}

export function ModalityTag({ modality }: { modality?: string }) {
  const view = modalityView(modality)
  return <span className={`tag-modality ${view.cls}`}>{view.text}</span>
}
