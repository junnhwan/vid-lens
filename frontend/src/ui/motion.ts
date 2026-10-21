export const EASE = "cubic-bezier(0.23, 1, 0.32, 1)"

const generations = new WeakMap<HTMLElement, number>()

export function prefersReduced(): boolean {
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches
}

function bump(el: HTMLElement): number {
  const next = (generations.get(el) ?? 0) + 1
  generations.set(el, next)
  return next
}

function clearPosition(el: HTMLElement) {
  el.style.position = ""
  el.style.left = ""
  el.style.right = ""
  el.style.top = ""
  el.style.zIndex = ""
  el.style.pointerEvents = ""
}
function duration(name: string, fallback: number) {
  const value = parseFloat(getComputedStyle(document.documentElement).getPropertyValue(name))
  return Number.isFinite(value) ? value : fallback
}

export function showView(el: HTMLElement) {
  const generation = bump(el)
  const wasHidden = el.hidden
  const style = getComputedStyle(el),
    from = {
      opacity: wasHidden ? "0" : style.opacity,
      transform: wasHidden ? "translateY(16px)" : style.transform,
    }
  for (const anim of el.getAnimations()) anim.cancel()
  clearPosition(el)
  el.hidden = false
  el.inert = false
  el.removeAttribute("aria-hidden")
  if (prefersReduced()) return
  const anim = el.animate([from, { opacity: 1, transform: "none" }], {
    duration: duration("--motion-enter", 320),
    easing: EASE,
    fill: "both",
  })
  anim.onfinish = () => {
    if (generations.get(el) === generation) anim.cancel()
  }
}

export function hideView(el: HTMLElement) {
  if (el.hidden) return
  const generation = bump(el)
  const style = getComputedStyle(el),
    from = { opacity: style.opacity, transform: style.transform }
  el.inert = true
  el.setAttribute("aria-hidden", "true")
  for (const anim of el.getAnimations()) anim.cancel()
  if (prefersReduced()) {
    el.hidden = true
    clearPosition(el)
    return
  }
  el.style.position = "absolute"
  el.style.left = "0"
  el.style.right = "0"
  el.style.top = "0"
  el.style.zIndex = "2"
  el.style.pointerEvents = "none"
  const anim = el.animate([from, { opacity: 0, transform: from.transform }], {
    duration: duration("--motion-exit", 200),
    easing: EASE,
    fill: "forwards",
  })
  anim.onfinish = () => {
    if (generations.get(el) !== generation) return
    el.hidden = true
    clearPosition(el)
    anim.cancel()
  }
}

export function slideTitle(el: HTMLElement, title: string) {
  if (el.dataset.value === title) return
  const previous = el.dataset.value
  el.dataset.value = title
  const incoming = document.createElement("span")
  incoming.className = "title-now"
  incoming.textContent = title
  el.replaceChildren(incoming)
  if (!previous || prefersReduced()) return
  const ghost = document.createElement("span")
  ghost.className = "title-ghost"
  ghost.setAttribute("aria-hidden", "true")
  ghost.textContent = previous
  el.append(ghost)
  const leave = ghost.animate(
    [
      { opacity: 1, transform: "none", filter: "blur(0)" },
      { opacity: 0, transform: "translateY(-0.55em)", filter: "blur(2px)" },
    ],
    { duration: 220, easing: EASE, fill: "forwards" },
  )
  leave.onfinish = () => ghost.remove()
  incoming.animate(
    [
      { opacity: 0, transform: "translateY(0.6em)" },
      { opacity: 1, transform: "none" },
    ],
    { duration: 320, delay: 60, easing: EASE, fill: "backwards" },
  )
}

const EASE_MOVE = "cubic-bezier(0.77, 0, 0.175, 1)"

export function carryStill(start: DOMRect, to: HTMLElement) {
  if (prefersReduced() || start.width < 8) return
  const end = to.getBoundingClientRect()
  if (end.width < 8 || end.height < 8) return
  const clone = to.cloneNode(true) as HTMLElement
  clone.removeAttribute("id")
  clone.querySelectorAll("[id]").forEach((node) => node.removeAttribute("id"))
  clone.inert = true
  clone.setAttribute("aria-hidden", "true")
  clone.style.position = "fixed"
  clone.style.left = `${start.left}px`
  clone.style.top = `${start.top}px`
  clone.style.width = `${start.width}px`
  clone.style.height = `${start.height}px`
  clone.style.margin = "0"
  clone.style.zIndex = "30"
  clone.style.pointerEvents = "none"
  clone.style.overflow = "hidden"
  clone.style.borderRadius = "10px"
  clone.style.transformOrigin = "top left"
  document.body.appendChild(clone)
  to.style.opacity = "0"
  const dx = end.left - start.left
  const dy = end.top - start.top
  const sx = end.width / start.width
  const sy = end.height / start.height
  const anim = clone.animate([{ transform: "none" }, { transform: `translate(${dx}px, ${dy}px) scale(${sx}, ${sy})` }], {
    duration: 280,
    easing: EASE_MOVE,
    fill: "forwards",
  })
  const finish = () => {
    clone.remove()
    to.style.opacity = ""
  }
  anim.onfinish = finish
  anim.oncancel = finish
}

export function flyStage(stage: HTMLElement, origin: HTMLElement | null, closing: boolean): Animation | null {
  if (prefersReduced() || !origin || !origin.isConnected) return null
  const current = getComputedStyle(stage).transform
  for (const animation of stage.getAnimations()) animation.cancel()
  const first = origin.getBoundingClientRect()
  const last = stage.getBoundingClientRect()
  if (!last.width || !last.height || !first.width) return null
  const dx = first.left - last.left
  const dy = first.top - last.top
  const sx = first.width / last.width
  const sy = first.height / last.height
  const from = `translate(${dx}px, ${dy}px) scale(${sx}, ${sy})`
  return stage.animate(closing ? [{ transform: current }, { transform: from }] : [{ transform: from }, { transform: "none" }], {
    duration: closing ? 220 : 280,
    easing: "cubic-bezier(0.32, 0.72, 0, 1)",
    fill: "forwards",
  })
}

