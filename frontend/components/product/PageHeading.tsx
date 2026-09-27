import type { ReactNode } from 'react'

export function PageHeading({ eyebrow, title, description, actions }: {
  eyebrow?: string; title: string; description?: string; actions?: ReactNode
}) {
  return <header className="product-heading">
    <div>{eyebrow && <p className="product-eyebrow">{eyebrow}</p>}<h1>{title}</h1>{description && <p className="product-description">{description}</p>}</div>
    {actions && <div className="product-actions">{actions}</div>}
  </header>
}
