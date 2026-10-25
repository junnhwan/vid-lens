import { lectures, sameVideo, type Lecture } from "./lectures"

export function LectureShelf({ link, onPick }: { link: string; onPick: (item: Lecture) => void }) {
  return (
    <section className="lecture-shelf" aria-labelledby="lecture-shelf-title">
      <header className="lecture-head">
        <div>
          <p className="eyebrow">
            <span /> THE WATCHLIST
          </p>
          <h2 id="lecture-shelf-title">从这四条视频试起</h2>
        </div>
        <p>一场访谈、一段技术评论、两堂智能体课程。<br />选一条，链接和关注点会填到上面的表单。</p>
      </header>
      <ol className="lecture-list">
        {lectures.map((item, index) => {
          const picked = sameVideo(link, item.url)
          return (
            <li key={item.id}>
              <button type="button" className="lecture-row" aria-label={`选择视频：${item.title}`} aria-pressed={picked} onClick={() => onPick(item)}>
                <span className="lecture-cover">
                  <img src={item.cover} alt={item.coverAlt} width="1280" height="720" loading="lazy" decoding="async" />
                  <span className="lecture-duration" aria-label={`视频时长 ${item.duration}`}>{item.duration}</span>
                </span>
                <span className="lecture-copy">
                  <span className="lecture-topline">
                    <span className="lecture-kind">{item.kind}</span>
                    <span className="lecture-index" aria-hidden="true">{String(index + 1).padStart(2, "0")}</span>
                  </span>
                  <strong>{item.title}</strong>
                  <span className="lecture-channel">{item.channel}</span>
                  <span className="lecture-hint">{item.hint}</span>
                  <span className="lecture-go">
                    {picked ? "已填入表单" : "选这条视频"}
                    <span className="nudge nudge-forward" aria-hidden="true">{picked ? " ✓" : " →"}</span>
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
