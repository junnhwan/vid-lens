import assert from 'node:assert/strict'

import {
  formatSessionLabel,
  formatSessionRelativeTime,
} from '../src/chatSessionDisplayPolicy.js'

assert.equal(formatSessionLabel({ title: '  讲了什么  ', id: 3 }), '讲了什么')
assert.equal(formatSessionLabel({ title: '', id: 9 }), '会话 #9')
assert.equal(formatSessionLabel({ title: null }, { index: 2 }), '对话 3')

const now = new Date('2026-07-14T12:00:00Z')
assert.equal(formatSessionRelativeTime(new Date('2026-07-14T11:59:30Z'), now), '刚刚')
assert.equal(formatSessionRelativeTime(new Date('2026-07-14T11:40:00Z'), now), '20 分钟前')
assert.equal(formatSessionRelativeTime(new Date('2026-07-14T09:00:00Z'), now), '3 小时前')
assert.equal(formatSessionRelativeTime(new Date('2026-07-12T12:00:00Z'), now), '2 天前')
assert.equal(formatSessionRelativeTime(new Date('2026-06-01T00:00:00Z'), now), '2026-06-01')
assert.equal(formatSessionRelativeTime(null, now), '')

console.log('chatSessionDisplayPolicy tests passed')
