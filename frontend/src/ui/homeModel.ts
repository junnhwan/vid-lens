import type { GenerationModel } from "../contract/types"
import type { ChoiceOption } from "./ChoiceMenu"

const recommendedMark = { label: "推荐", note: "当前更稳定" }

export function homeModelOption(model: GenerationModel): ChoiceOption {
  return {
    id: model.id,
    label: model.display_name,
    hint: model.description,
    brand: model.brand_id,
    mark: model.recommended ? recommendedMark : undefined,
  }
}
