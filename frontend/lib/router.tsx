import { forwardRef, useMemo, type AnchorHTMLAttributes } from 'react'
import { Link as RouterLink, useLocation, useNavigate } from 'react-router'

type LinkProps = Omit<AnchorHTMLAttributes<HTMLAnchorElement>, 'href'> & { href: string }

const Link = forwardRef<HTMLAnchorElement, LinkProps>(function Link({ href, ...props }, ref) {
  return <RouterLink ref={ref} to={href} {...props} />
})

export default Link

export function usePathname() {
  return useLocation().pathname
}

export function useRouter() {
  const navigate = useNavigate()
  return useMemo(() => ({
    push: (path: string) => navigate(path),
    replace: (path: string) => navigate(path, { replace: true }),
    back: () => navigate(-1),
  }), [navigate])
}
