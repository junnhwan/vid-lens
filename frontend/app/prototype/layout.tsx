import './prototype.css'
import { ProtoPageNav } from '@/components/prototype/c/Shell'

export default function PrototypeLayout({ children }: { children: React.ReactNode }) {
  return (
    <>
      {children}
      <div className="proto-grain" aria-hidden />
      <ProtoPageNav />
    </>
  )
}
