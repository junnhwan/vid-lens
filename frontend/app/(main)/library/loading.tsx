import { CardSkeleton } from '@/components/ui/AsyncState'

export default function LibraryLoading() {
  return (
    <div className="page page-wide">
      <CardSkeleton count={8} />
    </div>
  )
}
