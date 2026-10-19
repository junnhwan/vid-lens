export interface PublicNotice {
  id?: string
  version: string
  title: string
  body: string
  published_at?: string
}

export interface AnnouncementResponse {
  announcement: PublicNotice | null
  timeline?: PublicNotice[]
}

export interface AnnouncementEntry {
  id: string
  title: string
  body: string
  status: "current" | "published" | "withdrawn" | "draft"
  version: string
  published_at: string | null
}

export interface AnnouncementList {
  revision: number
  items: AnnouncementEntry[]
}
