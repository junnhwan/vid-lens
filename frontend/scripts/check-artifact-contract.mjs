// Validate actual backend handler responses without copying private data into source.
// node --experimental-strip-types scripts/check-artifact-contract.mjs <response-file.json>
import { readFile } from 'node:fs/promises'
import assert from 'node:assert/strict'
import { z } from 'zod'
import {
  detailSchema, runSchema, sourceSchema, taskPageSchema,
  versionSchema, versionSummarySchema, editRunSchema, editOperationSchema,
} from '../lib/artifacts/schema.ts'

const file = process.argv[2]
if (!file) throw new Error('Provide a local backend response JSON file.')
const responses = JSON.parse((await readFile(file, 'utf8')).replace(/^\uFEFF/, ''))
const cases = {
  artifact: detailSchema,
  created_artifact: detailSchema,
  run: runSchema,
  edit_run: editRunSchema,
  edit_operation: editOperationSchema,
  source: sourceSchema,
  tasks: taskPageSchema,
  version: versionSchema,
  versions: z.object({ list: z.array(versionSummarySchema) }),
}
for (const [name, schema] of Object.entries(cases)) {
  assert.ok([200, 202].includes(responses[name]?.code), `${name}: success envelope`)
  schema.parse(responses[name].data)
  console.log(`PASS: ${name}`)
}
assert.equal(responses.version_conflict.code, 409)
assert.equal(responses.version_conflict.data.error_code, 'version_conflict')
console.log('PASS: version_conflict')
