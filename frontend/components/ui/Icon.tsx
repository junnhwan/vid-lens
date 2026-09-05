/* 图标集:Tabler Icons (MIT),路径与 docs/prototype/index.html 的 <symbol> 逐字一致。
   IconSprite 在根布局渲染一次隐藏 sprite,页面内用 <Icon name="..." /> 引用。 */

const PATHS = {
  home: (
    <>
      <path d="M5 12l-2 0l9 -9l9 9l-2 0" />
      <path d="M5 12v7a2 2 0 0 0 2 2h10a2 2 0 0 0 2 -2v-7" />
      <path d="M9 21v-6a2 2 0 0 1 2 -2h2a2 2 0 0 1 2 2v6" />
    </>
  ),
  video: (
    <>
      <path d="M15 10l4.55 -2.27a1 1 0 0 1 1.45 .9v6.74a1 1 0 0 1 -1.45 .9l-4.55 -2.29" />
      <rect x="3" y="6" width="12" height="12" rx="2" />
    </>
  ),
  folder: <path d="M5 4h4l3 5h7a2 2 0 0 1 2 2v8a2 2 0 0 1 -2 2h-14a2 2 0 0 1 -2 -2v-13a2 2 0 0 1 2 -2z" />,
  settings: (
    <>
      <path d="M4 10a2 2 0 1 0 4 0a2 2 0 0 0 -4 0" />
      <path d="M6 4v4" />
      <path d="M6 12v8" />
      <path d="M10 16a2 2 0 1 0 4 0a2 2 0 0 0 -4 0" />
      <path d="M12 4v10" />
      <path d="M12 18v2" />
      <path d="M16 7a2 2 0 1 0 4 0a2 2 0 0 0 -4 0" />
      <path d="M18 4v1" />
      <path d="M18 9v11" />
    </>
  ),
  send: (
    <>
      <path d="M10 14l11 -11" />
      <path d="M21 3l-6.5 18a.55 .55 0 0 1 -1 0l-3.5 -7l-7 -3.5a.55 .55 0 0 1 0 -1l18 -6.5" />
    </>
  ),
  play: <path d="M7 5v14l12 -7z" fill="currentColor" stroke="none" />,
  pause: (
    <>
      <rect x="6" y="5" width="4" height="14" rx="1" fill="currentColor" stroke="none" />
      <rect x="14" y="5" width="4" height="14" rx="1" fill="currentColor" stroke="none" />
    </>
  ),
  clock: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 3" />
    </>
  ),
  bolt: <path d="M13 3l0 7l6 0l-8 11l0 -7l-6 0l8 -11" />,
  search: (
    <>
      <circle cx="10" cy="10" r="7" />
      <path d="M21 21l-6 -6" />
    </>
  ),
  upload: (
    <>
      <path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2 -2v-2" />
      <path d="M7 9l5 -5l5 5" />
      <path d="M12 4v12" />
    </>
  ),
  link: (
    <>
      <path d="M9 15l6 -6" />
      <path d="M11 6l.5 -.5a3.5 3.5 0 0 1 5 5l-.5 .5" />
      <path d="M13 18l-.5 .5a3.5 3.5 0 0 1 -5 -5l.5 -.5" />
    </>
  ),
  x: (
    <>
      <path d="M18 6l-12 12" />
      <path d="M6 6l12 12" />
    </>
  ),
  check: <path d="M5 12l5 5l10 -11" />,
  alert: (
    <>
      <path d="M12 9v4" />
      <path d="M12 15h.01" />
      <path d="M10.24 3.957l-8.422 14.06a1.989 1.989 0 0 0 1.7 2.983h16.845a1.989 1.989 0 0 0 1.7 -2.983l-8.423 -14.06a1.989 1.989 0 0 0 -3.4 0z" />
    </>
  ),
  'chev-r': <path d="M9 6l6 6l-6 6" />,
  'chev-l': <path d="M15 6l-6 6l6 6" />,
  plus: (
    <>
      <path d="M12 5v14" />
      <path d="M5 12h14" />
    </>
  ),
  file: (
    <>
      <path d="M14 3v4a1 1 0 0 0 1 1h4" />
      <path d="M17 21h-10a2 2 0 0 1 -2 -2v-14a2 2 0 0 1 2 -2h7l5 5v11a2 2 0 0 1 -2 2z" />
      <path d="M9 13l6 0" />
      <path d="M9 17l6 0" />
    </>
  ),
  eye: (
    <>
      <path d="M10 12a2 2 0 1 0 4 0a2 2 0 0 0 -4 0" />
      <path d="M21 12c-2.4 4-5.4 6-9 6c-3.6 0-6.6-2-9-6c2.4-4 5.4-6 9-6c3.6 0 6.6 2 9 6" />
    </>
  ),
  refresh: (
    <>
      <path d="M20 11a8.1 8.1 0 0 0 -15.5 -2" />
      <path d="M4 5v4h4" />
      <path d="M4 13a8.1 8.1 0 0 0 15.5 2" />
      <path d="M20 19v-4h-4" />
    </>
  ),
  bulb: (
    <>
      <path d="M9 18h6" />
      <path d="M10 22h4" />
      <path d="M12 2a7 7 0 0 1 4 12.7c-.6.5-1 1.4-1 2.3h-6c0-.9-.4-1.8-1-2.3a7 7 0 0 1 4-12.7z" />
    </>
  ),
  shield: <path d="M12 3l7 3v5c0 5-3 8.5-7 10-4-1.5-7-5-7-10V6z" />,
  'shield-check': (
    <>
      <path d="M12 3l7 3v5c0 5-3 8.5-7 10-4-1.5-7-5-7-10V6z" />
      <path d="M9.5 12l1.8 1.8l3.5 -3.8" />
    </>
  ),
  activity: <path d="M22 12h-4l-3 8l-6 -16l-3 8h-4" />,
  layers: (
    <>
      <path d="M12 3l9 4.5l-9 4.5l-9 -4.5l9 -4.5" />
      <path d="M3 12l9 4.5l9 -4.5" />
      <path d="M3 16.5l9 4.5l9 -4.5" />
    </>
  ),
  sort: (
    <>
      <path d="M3 9l4 -4l4 4" />
      <path d="M7 5v14" />
      <path d="M21 15l-4 4l-4 -4" />
      <path d="M17 19V5" />
    </>
  ),
  photo: (
    <>
      <rect x="3" y="5" width="18" height="14" rx="2" />
      <circle cx="9" cy="10" r="1.6" />
      <path d="M7 19l5.5 -6l3 3.2l2 -2.2l3.5 5" />
    </>
  ),
  scan: (
    <>
      <path d="M3 7V5a2 2 0 0 1 2 -2h2" />
      <path d="M17 3h2a2 2 0 0 1 2 2v2" />
      <path d="M21 17v2a2 2 0 0 1 -2 2h-2" />
      <path d="M7 21H5a2 2 0 0 1 -2 -2v-2" />
      <path d="M8 8h8" />
      <path d="M8 12h8" />
      <path d="M8 16h5" />
    </>
  ),
  target: (
    <>
      <circle cx="12" cy="12" r="9" />
      <circle cx="12" cy="12" r="5" />
      <circle cx="12" cy="12" r="1" />
    </>
  ),
  filter: <path d="M4 4h16l-6 8v6l-4 -2v-4l-6 -8" />,
  trash: (
    <>
      <path d="M4 7h16" />
      <path d="M10 11v6" />
      <path d="M14 11v6" />
      <path d="M5 7l1 12a2 2 0 0 0 2 2h8a2 2 0 0 0 2 -2l1 -12" />
      <path d="M9 7V4h6v3" />
    </>
  ),
  message: (
    <>
      <path d="M8 9h8" />
      <path d="M8 13h5" />
      <path d="M12 21a9 9 0 1 0 -9 -9c0 1.6.4 3 1.2 4.3l-1.2 4.7l4.8 -1.2a9 9 0 0 0 4.2 1.2z" />
    </>
  ),
  download: (
    <>
      <path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2 -2v-2" />
      <path d="M7 11l5 5l5 -5" />
      <path d="M12 4v12" />
    </>
  ),
  cpu: (
    <>
      <rect x="5" y="5" width="14" height="14" rx="2" />
      <rect x="9" y="9" width="6" height="6" rx="1" />
      <path d="M9 2v3" />
      <path d="M15 2v3" />
      <path d="M9 19v3" />
      <path d="M15 19v3" />
      <path d="M2 9h3" />
      <path d="M2 15h3" />
      <path d="M19 9h3" />
      <path d="M19 15h3" />
    </>
  ),
  'arrow-r': (
    <>
      <path d="M5 12h14" />
      <path d="M13 6l6 6l-6 6" />
    </>
  ),
  info: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 8h.01" />
      <path d="M11.5 12h.5v4h1" />
    </>
  ),
  pencil: (
    <>
      <path d="M4 20h4l10.5 -10.5a2.1 2.1 0 0 0 -3 -3l-10.5 10.5v4z" />
      <path d="M13.5 6.5l3 3" />
    </>
  ),
  dots: (
    <>
      <path d="M5 12h.01" />
      <path d="M12 12h.01" />
      <path d="M19 12h.01" />
    </>
  ),
  'zoom-scan': (
    <>
      <path d="M3 7V5a2 2 0 0 1 2 -2h2" />
      <path d="M17 3h2a2 2 0 0 1 2 2v2" />
      <path d="M21 17v2a2 2 0 0 1 -2 2h-2" />
      <path d="M7 21H5a2 2 0 0 1 -2 -2v-2" />
      <circle cx="11" cy="11" r="4" />
      <path d="M16.5 16.5l-1.8 -1.8" />
    </>
  ),
  list: (
    <>
      <path d="M9 6h11" />
      <path d="M9 12h11" />
      <path d="M9 18h11" />
      <path d="M5 6h.01" />
      <path d="M5 12h.01" />
      <path d="M5 18h.01" />
    </>
  ),
  wand: (
    <>
      <path d="M6 21l15 -15" />
      <path d="M15 4l1.5 1.5" />
      <path d="M9 4l.75 .75" />
      <path d="M4.5 8.5l.75 .75" />
      <path d="M7 3l.5 .5" />
    </>
  ),
} as const

export type IconName = keyof typeof PATHS

export function IconSprite() {
  return (
    <svg xmlns="http://www.w3.org/2000/svg" style={{ display: 'none' }} aria-hidden="true">
      {Object.entries(PATHS).map(([name, node]) => (
        <symbol key={name} id={`i-${name}`} viewBox="0 0 24 24">
          {node}
        </symbol>
      ))}
    </svg>
  )
}

export function Icon({ name, size, className }: { name: IconName; size?: 'sm' | 'lg'; className?: string }) {
  const cls = ['ic', size ? `ic-${size}` : '', className || ''].filter(Boolean).join(' ')
  return (
    <svg className={cls} aria-hidden="true">
      <use href={`#i-${name}`} />
    </svg>
  )
}
