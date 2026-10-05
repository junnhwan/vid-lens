import test from 'node:test'
import assert from 'node:assert/strict'
import { citationCopyText, highlightQuote, withClaimCitations } from './citationText.ts'

test('claim links preserve Unicode offsets and multiple sources',()=>{
 assert.equal(withClaimCitations('😀结论。', [{id:'C1',content:'原文',claimEndRunes:[3]},{id:'C2',content:'原文2',claimEndRunes:[3]}]),'😀结论[C1][C2]。')
 assert.equal(withClaimCitations('结论。',[{id:'C9',content:'原文',claimEndRunes:[99]}]),'结论。')
})
test('unknown and coarse time copy stays truthful',()=>{
 assert.equal(citationCopyText('视频B',{id:'C1',content:'条件原文',startMS:0,endMS:0,timeRangeStatus:'unknown'},()=> '00:00'),'[视频B 时间未知] 条件原文')
 assert.equal(citationCopyText('视频B',{id:'C1',content:'条件原文',startMS:0,endMS:1000,timeRangeStatus:'coarse'},()=> '00:00'),'[视频B 00:00（粗粒度）] 条件原文')
})
test('highlight is a verbatim source slice',()=>{
 assert.deepEqual(highlightQuote('前文。不能重试。后文。','不能重试。'),[{text:'前文。',highlight:false},{text:'不能重试。',highlight:true},{text:'后文。',highlight:false}])
})
