import { RetrievalWorkbench } from '@/components/knowledge/RetrievalWorkbench'

export default function RetrievalPage({ params }: { params: { id:string } }) {
  return <RetrievalWorkbench kbId={Number(params.id)} />
}
