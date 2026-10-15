import markup from "./lens.svg?raw"

export function Lens() {
  return <div className="lens" aria-hidden="true" dangerouslySetInnerHTML={{ __html: markup }} />
}
