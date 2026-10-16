import type { GenerationModel } from "../contract/types"

const initials: Record<string, string> = {
  openai: "O",
  qwen: "Q",
  deepseek: "D",
  gemini: "G",
  claude: "C",
}
// Original local monograms, rather than unlicensed brand logos or remote URLs.
export function ModelIcon({ brand = "generic" }: { brand?: string }) {
  return (
    <svg className="model-icon" viewBox="0 0 24 24" role="img" aria-label={brand === "generic" ? "模型" : `${brand} 模型标识`}>
      <rect x="1" y="1" width="22" height="22" rx="6" fill="currentColor" opacity=".1" />
      <path d="M6 5.5h7a5 5 0 0 1 0 13H6z" fill="none" stroke="currentColor" strokeWidth="1.3" opacity={brand === "generic" ? 1 : 0} />
      <text x="12" y="16" textAnchor="middle" fill="currentColor" fontSize="13" fontFamily="serif" fontWeight="600">
        {initials[brand] || ""}
      </text>
    </svg>
  )
}
export function ModelIdentity({ model, purpose }: { model?: GenerationModel; purpose?: string }) {
  if (!model) return null
  return (
    <div className="generation-identity">
      <span>
        <ModelIcon brand={model.brand_id} />
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
