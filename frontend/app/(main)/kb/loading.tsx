import { CardSkeleton } from '@/components/ui/AsyncState'

export default function KBLoading() {
  return (
    <div className="page page-wide">
      <CardSkeleton count={3} gridClass="kb-grid" />
    </div>
  )
}
