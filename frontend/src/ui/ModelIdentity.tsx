import type { DetailLevel, GenerationModel } from "../contract/types"
import { ModelIcon } from "./brandMarks"

export { ModelIcon }
export function ModelIdentity({ model, purpose, detailLevel, showHistoryNote = true }: { model?: GenerationModel; purpose?: string; detailLevel?: DetailLevel | ""; showHistoryNote?: boolean }) {
  if (!model) return null
  return (
    <div className="generation-identity">
      <span>
        <ModelIcon brand={model.brand_id} name={model.display_name} />
        <span>{model.display_name}</span>
        <span className="identity-locked">{model.legacy ? "历史记录" : "本次固定"}</span>
        <span className="identity-locked">{detailLevel ? {concise:"精简", standard:"标准", detailed:"详细"}[detailLevel] : "原版解读"}</span>
      </span>
      {purpose ? (
        <p>
          <span>关注点</span>
          {purpose}
        </p>
      ) : null}
      {model.legacy && showHistoryNote ? <small>历史记录按归档信息显示；早期问答的模型可能不同。</small> : null}
    </div>
  )
}
