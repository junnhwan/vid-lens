interface CitationView {
 id: string; content: string; anchorQuote?: string; startMS?: number; endMS?: number
 timeRangeStatus?: string; claimTexts?: string[]; claimEndRunes?: number[]; quoteTruncated?: boolean
}

export function withClaimCitations(content: string, cites: CitationView[]): string {
 const runes=Array.from(content)
 const ends=new Map<number, Set<string>>()
 for (const c of cites) {
  for (const end of c.claimEndRunes || []) {
   if (!Number.isInteger(end) || end<0 || end>runes.length || !/^C[1-9]\d*$/.test(c.id)) continue
   const ids=ends.get(end)||new Set<string>(); ids.add(c.id); ends.set(end,ids)
  }
 }
 let result=''
 for (let i=0;i<=runes.length;i++) {
  result+=Array.from(ends.get(i)||[],id=>`[${id}]`).join('')
  if (i<runes.length) result+=runes[i]
 }
 return result
}

export function citationCopyText(title: string, cite: CitationView, clock: (ms?: number)=>string): string {
 const timed=(cite.timeRangeStatus==='exact'||cite.timeRangeStatus==='coarse') && Number.isFinite(cite.startMS) && Number.isFinite(cite.endMS) && cite.endMS!>cite.startMS!
 const time=timed ? `${clock(cite.startMS)}${cite.timeRangeStatus==='coarse'?'（粗粒度）':''}` : '时间未知'
 return `[${title} ${time}] ${cite.anchorQuote||cite.content}${cite.quoteTruncated?'…（原文省略）':''}`
}

export function highlightQuote(context: string, quote: string): {text:string; highlight:boolean}[] {
 const pos=context.indexOf(quote)
 if (!quote || pos<0) return [{text:context,highlight:false}]
 return [{text:context.slice(0,pos),highlight:false},{text:quote,highlight:true},{text:context.slice(pos+quote.length),highlight:false}]
}
