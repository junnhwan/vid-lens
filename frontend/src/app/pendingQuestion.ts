import type { QuestionContext } from "../contract/types"

// Transport state is separate from server turns: no fabricated run or turn IDs.
export interface PendingQuestion {
  question: string
  context: QuestionContext
  phase: "sending" | "failed"
}
