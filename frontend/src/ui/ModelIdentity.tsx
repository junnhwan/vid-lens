import type { GenerationModel } from "../contract/types"
import { ModelIcon } from "./brandMarks"

export { ModelIcon }
export function ModelIdentity({ model, purpose }: { model?: GenerationModel; purpose?: string }) {
  if (!model) return null
  return (
    <div className="generation-identity">
      <span>
        <ModelIcon brand={model.brand_id} name={model.display_name} />
        <span>{model.display_name}</span>
        <span className="identity-locked">{model.legacy ? "历史记录" : "本次固定"}</span>
      </span>
      {purpose ? (
        <p>
          <span>关注点</span>
          {purpose}
        </p>
      ) : null}
      {model.legacy ? <small>历史记录按归档信息显示；早期问答的模型可能不同。</small> : null}
    </div>
  )
}
