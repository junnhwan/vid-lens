# Product UI

The production workspace retains the existing routes and adds `/artifacts`,
`/artifacts/[id]`, and `/tasks`. The API authority is
`../docs/architecture/artifact-api-contract.md`.

## Where to change the design

- `styles/tokens.css`: shared colors, typefaces, spacing, paper, evidence panel,
  map palette, and theme overrides. Change these first for visual adjustments.
- `styles/product.css`: shell, page headings, dashboard hero and metrics.
- `styles/artifacts.css`: note paper, editor, map, evidence and task layouts.
- `components/shell/ShellFrame.tsx`: shared navigation and responsive frame.
- `components/product/`: common page headings and dashboard hero.
- `components/artifacts/`: reusable workspace, creation dialog, evidence panel,
  task status, cards and version-aware editing.

Notes and the mind map render the same structured body. Markmap is loaded on
demand; labels are escaped and no model-provided code or assets are executed.
Large trees initially collapse chapters; the text outline exposes all nodes.

## Data and writes

`lib/artifacts/schema.ts` validates wire responses with Zod. `api.ts` uses the
existing authenticated transport; production has no fixture fallback.
TanStack Query is scoped to the signed-in owner. Active runs use GET polling;
the existing conversation stream is unchanged.

Generation uses a stable idempotency key for retries of the same submission.
Editing uses the head version captured with the draft. A 409 preserves that
draft and offers comparison and download. Historical versions are read-only.
Unknown evidence timestamps never produce a seek; outdated snapshots retain
their text but disable seeking into a changed source.

## Development preview

Run `npm run dev`, then open `/dev/product?view=notes`. Other views are `home`,
`artifacts`, and `tasks`. The state selector exposes errors, conflicts, read-only
access, source deletion, unknown timestamps and a 200-node map.

`dev/productFixtures.ts` contains explicit synthetic examples. Saving in this
preview uses tab-scoped session storage only. Preview components are shared with
production, but the sample data is never a production API response or fallback.
The page guard and middleware reject this route outside development with 404.

## Checks

```powershell
npm test
npm run typecheck
npm run build
npm run test:stream
node --experimental-strip-types scripts/check-artifact-contract.mjs <local-response-file.json>
```

The last command validates private response specimens emitted by the backend
handler integration test. It does not substitute for authenticated browser
acceptance against a running API, worker, model provider and video media.
