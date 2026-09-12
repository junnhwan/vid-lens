import test from 'node:test'
import assert from 'node:assert/strict'
import { replayLink } from './knowledge.ts'

test('replay keeps video identity and only uses reliable nonnegative timestamps',()=>{
  assert.equal(replayLink(12,90500,'exact'),'/video/12?t=90500')
  assert.equal(replayLink(13,0,'coarse'),'/video/13?t=0')
  assert.equal(replayLink(12,90500,'unknown'),'/video/12')
  assert.equal(replayLink(12,-1,'exact'),'/video/12')
  assert.equal(replayLink(12,NaN,'exact'),'/video/12')
})
