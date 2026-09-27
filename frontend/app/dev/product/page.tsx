import { notFound } from 'next/navigation'

export default async function ProductPreviewPage({ searchParams }: { searchParams: { view?: string } }) {
  if (process.env.NODE_ENV !== 'development') notFound()
  const { ProductPreview } = await import('@/dev/ProductPreview')
  return <ProductPreview view={searchParams.view || 'notes'} />
}
