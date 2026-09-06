export function BrandMark({ size = 30 }: { size?: number }) {
  return (
    <svg
      className="brand-mark"
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="none"
      aria-hidden="true"
    >
      <rect width="32" height="32" rx="9" fill="var(--bg-3)" stroke="var(--acc-line)" />
      {/* 镜头光圈:点顶六边形 + 内圈 + 叶片缝 */}
      <path
        d="M16 5.6 L24.9 10.8 V21.2 L16 26.4 L7.1 21.2 V10.8 Z"
        stroke="var(--acc)"
        strokeWidth="1.55"
        strokeLinejoin="round"
      />
      <path
        d="M16 9.2 L21.7 12.5 V19.5 L16 22.8 L10.3 19.5 V12.5 Z"
        stroke="var(--acc)"
        strokeWidth="1.15"
        strokeLinejoin="round"
        opacity="0.55"
      />
      <line x1="16" y1="5.6" x2="16" y2="9.2" stroke="var(--acc)" strokeWidth="1.1" opacity="0.45" />
      <line x1="24.9" y1="10.8" x2="21.7" y2="12.5" stroke="var(--acc)" strokeWidth="1.1" opacity="0.45" />
      <line x1="24.9" y1="21.2" x2="21.7" y2="19.5" stroke="var(--acc)" strokeWidth="1.1" opacity="0.45" />
      <line x1="16" y1="26.4" x2="16" y2="22.8" stroke="var(--acc)" strokeWidth="1.1" opacity="0.45" />
      <line x1="7.1" y1="21.2" x2="10.3" y2="19.5" stroke="var(--acc)" strokeWidth="1.1" opacity="0.45" />
      <line x1="7.1" y1="10.8" x2="10.3" y2="12.5" stroke="var(--acc)" strokeWidth="1.1" opacity="0.45" />
      <circle cx="16" cy="16" r="3.05" fill="var(--acc)" />
      <circle cx="14.6" cy="14.5" r="1" fill="#fff6d8" />
    </svg>
  )
}
