import { lectures, sameVideo, type Lecture } from "./lectures"

export function LectureShelf({ link, onPick }: { link: string; onPick: (item: Lecture) => void }) {
  return (
    <section className="lecture-shelf" aria-labelledby="lecture-shelf-title">
      <header className="lecture-head">
        <div>
          <p className="eyebrow">
            <span /> TRY A LECTURE
          </p>
          <h2 id="lecture-shelf-title">从这些讲课视频试起</h2>
        </div>
        <p>半小时以内，画面里有架构图、结构图、动画或代码。点一条，链接和关注点会填进上面的表单。</p>
      </header>
      <ol className="lecture-list">
        {lectures.map((item, index) => {
          const picked = sameVideo(link, item.url)
          return (
            <li key={item.id}>
              <button type="button" className="lecture-row" aria-pressed={picked} onClick={() => onPick(item)}>
                <span className="lecture-index">{String(index + 1).padStart(2, "0")}</span>
                <span className="lecture-kind">{item.kind}</span>
                <span className="lecture-copy">
                  <strong>{item.title}</strong>
                  <span>
                    {item.channel} · {item.duration} · {item.hint}
                  </span>
                </span>
                <span className="lecture-go">
                  {picked ? "已填入" : "试这篇"}
                  <span className="nudge nudge-forward" aria-hidden="true">
                    {picked ? "" : " →"}
                  </span>
                </span>
              </button>
            </li>
          )
        })}
      </ol>
    </section>
  )
}
