import { useRef, useState } from "react"
import { ApiError, sessionLost } from "../api/error"
import type {
  ArticleResponse,
  Frame
} from "../contract/types"
import type { VisitorContext } from "./visitorContext"
import { errorText } from "./visitorContract"
export function useVisitorArticle(context: VisitorContext, absorb: (frames: Frame[]) => void) {
  const workspace = context.workspace
  const { lost: markLost } = context.messages
  function ownsRequest(gen: number, id: string) { return workspace.ownsRequest(gen, id, context.request()?.request.id) }

  const [article, setArticle] = useState<ArticleResponse | null>(null)
  const [articleError, setArticleError] = useState("")
  const articleRef = useRef<ArticleResponse | null>(null)
  const loadedArticle = useRef("")
  const articleToken = useRef(0)
  articleRef.current = article
  async function loadArticle(id: string, force: boolean) {
    const rt = context.runtime()
    if (!rt) return
    if (!force && loadedArticle.current === id) return
    const token = ++articleToken.current
    const gen = workspace.epoch
    try {
      const body = await rt.api.article(id)
      if (token !== articleToken.current || !ownsRequest(gen, id)) return
      loadedArticle.current = id
      articleRef.current = body
      setArticle(body)
      setArticleError("")
      absorb(body.frames ?? [])
    } catch (error) {
      if (token !== articleToken.current || !ownsRequest(gen, id)) return
      if (sessionLost(error)) markLost(error instanceof ApiError ? error.detail : "匿名会话已失效，请重新开始。")
      else setArticleError(errorText(error))
    }
  }
  function reset() { loadedArticle.current = ""; articleToken.current += 1; articleRef.current = null; setArticle(null); setArticleError("") }
  return { article, articleError, current: () => articleRef.current, load: loadArticle, reset }
}
