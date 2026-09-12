import assert from 'node:assert/strict'
import test from 'node:test'
import { budgetProgress } from './budgetNotice.ts'
import { progressTrace } from '../components/chat/traceTypes.ts'

test('budget completion updates the existing progress row and explains output units', () => {
 const before=progressTrace([], {id:'budget',kind:'budget',label:'正在收尾',status:'done'})
 const notice=budgetProgress('budget_finalized',{dimension:'output_tokens',used:100,estimated_next:50,reserve:20,limit:150,usage_source:'estimated'})!
 const after=progressTrace(before,notice)
 assert.equal(after.length,1)
 assert.match(after[0].detail!,/输出 Token/)
 assert.match(after[0].detail!,/估算/)
 assert.doesNotMatch(after[0].detail!,/budget_finalized|estimated/)
})
