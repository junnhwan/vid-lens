import { App } from "../app/App"
import { Docs } from "./Docs"
import { Example } from "./Example"
import { AnnouncementArchive } from "./AnnouncementArchive"
import { Manage } from "./Manage"
import { SharedRecord } from "./SharedRecord"
import { VisitorAnnouncement } from "../ui/Announcement"
export function SiteApp() {
  const path = location.pathname
  if (path === "/manage" || path.startsWith("/manage/")) return <Manage />
  if (path === "/announcements" || path === "/announcements/") return <AnnouncementArchive />
  const page = path === "/s" || path.startsWith("/s/") ? <SharedRecord />
    : path === "/docs" || path.startsWith("/docs/") ? <Docs />
    : path === "/example" || path === "/example/" ? <Example /> : <App />
  return <>{page}<VisitorAnnouncement /></>
}
