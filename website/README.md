# Go AI SDK Docs Site

This is the [Docusaurus 3](https://docusaurus.io/) site that renders the Markdown/MDX docs in
[`../docs`](../docs) at **https://digitallysavvy.github.io/go-ai/**.

## Local development

```bash
npm ci
npm start
```

This starts a local dev server at `http://localhost:3000/go-ai/` with hot reload. Most edits
(including content in `../docs`) show up live without a server restart.

## Build

```bash
npm run build
```

Produces a static site in `build/`. This runs with `onBrokenLinks: 'throw'`, so any broken
internal link or missing doc will fail the build — the same check that runs in CI on every
push and pull request (see `.github/workflows/docs.yml`).

To sanity-check the production build locally:

```bash
npm run build
npm run serve
```

## Content

- Docs content lives in [`../docs`](../docs), not under `website/`. Sidebar ordering comes
  from the numeric folder/file prefixes (`00-introduction`, `02-foundations`, ...); labels are
  auto-generated with the numbers stripped.
- Internal/maintainer-only docs (`docs/_templates`, `docs/scripts`, style guides, etc.) are
  excluded from the published site via the `exclude` list in `docusaurus.config.ts`, not
  deleted from the repo.
- The landing page lives at `src/pages/index.tsx`; global styling is in `src/css/custom.css`.
- Brand assets (favicon, navbar mark, social card) live in `static/img/`.

## Deployment

Deployment is fully automated via GitHub Actions (`.github/workflows/docs.yml`):

- Every push to `main` builds and deploys to GitHub Pages.
- Every pull request runs a build-only check (no deploy) so broken docs are caught in review.
- The workflow can also be run manually via `workflow_dispatch`.

### One-time repo setting

For the deploy step to work, a repo maintainer must set, once:

**Settings → Pages → Source: GitHub Actions**

(Not "Deploy from a branch" — the workflow publishes directly via the Pages API.)
