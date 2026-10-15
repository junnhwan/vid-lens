import { useRef } from "react"
import { EASE, prefersReduced } from "./motion"

const initial = 86

function formatCount(value: number) {
  return value.toLocaleString("zh-Hans")
}

export function LikeButton() {
  const buttonRef = useRef<HTMLButtonElement>(null)
  const count = useRef(initial)
  const taps = useRef(0)
  const announce = useRef(0)
  const bloom = useRef(0)

  function roll(value: number) {
    const el = buttonRef.current?.querySelector<HTMLElement>(".like-count")
    if (!el) return
    const next = document.createElement("span")
    next.className = "like-num is-fresh"
    next.textContent = formatCount(value)
    const previous = [...el.querySelectorAll<HTMLElement>(".like-num")]
    el.append(next)
    for (const node of previous) {
      node.classList.remove("is-fresh")
      if (prefersReduced()) {
        node.remove()
        continue
      }
      node.animate(
        [{ transform: "translateY(0)", opacity: 1 }, { transform: "translateY(-120%)", opacity: 0 }],
        { duration: 180, easing: EASE, fill: "forwards" },
      ).onfinish = () => node.remove()
    }
    if (prefersReduced()) return
    next.animate(
      [{ transform: "translateY(70%)", opacity: 0.2 }, { transform: "translateY(0)", opacity: 1 }],
      { duration: 180, easing: EASE },
    )
  }

  function stamp(n: number) {
    const button = buttonRef.current
    if (!button || prefersReduced()) return
    const seal = button.querySelector<HTMLElement>(".like-seal")
    const prints = button.querySelector<HTMLElement>(".like-prints")
    if (!seal || !prints) return
    const tilt = ((n % 5) - 2) * 11
    const angle = ((n * 52 - 90) * Math.PI) / 180
    const blooming = n % 5 === 0
    seal.getAnimations().forEach((anim) => anim.cancel())
    seal.animate(
      [
        { transform: "translateY(0) rotate(0deg) scale(1)" },
        { transform: `translateY(3px) rotate(${tilt}deg) scale(0.88)`, offset: 0.28 },
        { transform: `translateY(0) rotate(${tilt * 0.35}deg) scale(1.05)`, offset: 0.62 },
        { transform: "translateY(0) rotate(0deg) scale(1)" },
      ],
      { duration: 420, easing: EASE },
    )
    const thumb = button.querySelector(".like-thumb")
    if (!thumb) return
    const ghost = document.createElement("span")
    ghost.className = "like-ghost"
    ghost.append(thumb.cloneNode(true))
    const dist = 26 + (n % 4) * 8
    const dx = Math.cos(angle) * dist
    const dy = Math.sin(angle) * dist
    prints.append(ghost)
    while (prints.childElementCount > 6) prints.firstElementChild?.remove()
    ghost.animate(
      [
        { opacity: 0.9, transform: "translate(-50%, -50%) rotate(0deg) scale(1)" },
        { opacity: 0, transform: `translate(calc(-50% + ${dx}px), calc(-50% + ${dy}px)) rotate(${tilt + (n % 2 ? 16 : -12)}deg) scale(0.7)` },
      ],
      { duration: 780, easing: EASE },
    ).onfinish = () => ghost.remove()

    const dent = document.createElement("span")
    dent.className = "like-dent"
    seal.append(dent)
    dent.animate(
      [
        { opacity: 0.85, transform: `rotate(${tilt}deg) scale(1, 0.55)` },
        { opacity: 0, transform: `rotate(${tilt}deg) scale(2.6, 1.4)` },
      ],
      { duration: 520, easing: EASE },
    ).onfinish = () => dent.remove()

    const spray = document.createElement("span")
    spray.className = "like-spray"
    const drops = blooming ? 9 : 6
    for (let i = 0; i < drops; i += 1) {
      const blot = document.createElement("i")
      const spread = angle + (i - (drops - 1) / 2) * 0.42
      const reach = 18 + (i % 3) * 9 + (blooming ? 10 : 0)
      blot.style.setProperty("--dx", `${Math.cos(spread) * reach}px`)
      blot.style.setProperty("--dy", `${Math.sin(spread) * reach}px`)
      blot.style.setProperty("--spin", `${((i * 37 + n * 13) % 70) - 35}deg`)
      if (i % 3 === 0) blot.dataset.warm = "true"
      if (blooming && i === drops - 1) blot.dataset.hot = "true"
      spray.append(blot)
    }
    seal.append(spray)
    while (seal.querySelectorAll(".like-spray").length > 3) seal.querySelector(".like-spray")?.remove()
    spray.addEventListener("animationend", (event) => {
      if (event.target === spray.lastElementChild) spray.remove()
    })
    const plus = document.createElement("span")
    plus.className = "like-plus"
    plus.textContent = "+1"
    plus.style.setProperty("--drift", `${Math.round(Math.cos(angle) * 12)}px`)
    seal.append(plus)
    while (seal.querySelectorAll(".like-plus").length > 3) seal.querySelector(".like-plus")?.remove()
    plus.animate(
      [
        { opacity: 0, transform: "translate(-50%, 6px)" },
        { opacity: 1, offset: 0.22, transform: "translate(calc(-50% + var(--drift)), -6px)" },
        { opacity: 0, transform: "translate(calc(-50% + var(--drift)), -30px)" },
      ],
      { duration: 680, easing: EASE },
    ).onfinish = () => plus.remove()
    if (blooming) {
      button.dataset.bloom = "true"
      window.clearTimeout(bloom.current)
      bloom.current = window.setTimeout(() => {
        delete button.dataset.bloom
      }, 520)
    }
  }

  function addLike() {
    taps.current += 1
    count.current += 1
    const next = count.current
    const button = buttonRef.current
    if (!button) return
    button.dataset.inked = "true"
    button.setAttribute("aria-label", `觉得有用，再点一次。目前 ${formatCount(next)} 个赞`)
    roll(next)
    stamp(taps.current)
    window.clearTimeout(announce.current)
    announce.current = window.setTimeout(() => {
      const live = document.getElementById("live")
      if (live) live.textContent = `已加上赞，现在 ${formatCount(next)} 个`
    }, 280)
  }

  return (
    <button type="button" className="like" id="like" ref={buttonRef} aria-label={`觉得有用，可以多次点赞。目前 ${formatCount(initial)} 个赞`} onClick={addLike}>
      <span className="like-well" aria-hidden="true">
        <span className="like-prints" />
        <span className="like-seal">
          <svg className="like-thumb" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M8.2 10.4 11 4.2c.4-.8 1.5-.9 2.1-.2.4.5.5 1.2.3 1.8L12.4 9h5.1c1.2 0 2.1 1.1 1.9 2.3l-1.1 6.2A2.2 2.2 0 0 1 16.1 19H8.2V10.4z" />
            <path d="M3.6 10.2h3.2c.4 0 .7.3.7.7v7.4c0 .4-.3.7-.7.7H3.6c-.6 0-1.1-.5-1.1-1.1v-6.6c0-.6.5-1.1 1.1-1.1z" />
          </svg>
        </span>
      </span>
      <span className="like-label" id="like-label">觉得有用</span>
      <span className="like-count" id="like-count"><span className="like-num">{formatCount(initial)}</span></span>
    </button>
  )
}
