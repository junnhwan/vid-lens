import { useEffect, useState } from 'react'
import { api } from '@/lib/api'
import { Icon } from '@/components/ui/Icon'
import { QuestionSuggestionsLoading } from './QuestionSuggestionsLoading'

export function FollowUpQuestions({ sessionId, messageId, onAsk }: { sessionId: number; messageId: number; onAsk: (question: string) => void }) {
  const [questions, setQuestions] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  useEffect(() => {
    let active = true
    setLoading(true); setQuestions([])
    void api.getFollowUpQuestions(sessionId, messageId)
      .then(result => { if (active) setQuestions(result.questions.map(item => item.question)) })
      .catch(() => { /* Suggestions never block the saved answer. */ })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [sessionId, messageId])
  if (!loading && !questions.length) return null
  return <section className="answer-followups" aria-label="继续追问">
    {loading ? <QuestionSuggestionsLoading followup /> : <><small><Icon name="bulb" size="sm" />继续追问</small>{questions.map((question, i) => <button type="button" key={question} style={{ animationDelay: `${i * 35}ms` }} onClick={() => onAsk(question)}><span>{question}</span><Icon name="chev-r" size="sm" /></button>)}</>}
  </section>
}
