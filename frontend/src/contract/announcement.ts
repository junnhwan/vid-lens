export interface PublishedAnnouncement {
  version: string
  title: string
  body: string
}

export interface AnnouncementSettings extends PublishedAnnouncement {
  revision: number
  enabled: boolean
}

export interface AnnouncementResponse {
  announcement: PublishedAnnouncement | null
}
