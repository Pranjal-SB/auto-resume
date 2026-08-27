# auto-resume

My resume, regenerated weekly by GitHub Actions. `resume.yaml` holds the
prose; star counts and language spread come from the GitHub API at build
time; a LaTeX template renders it and CI commits the PDF back.

Latest build: [`resume.pdf`](resume.pdf)

## Credit

Based on [Rahuletto/auto-resume](https://github.com/Rahuletto/auto-resume) by
[Rahul Marban](https://github.com/Rahuletto), MIT licensed. The idea, the
LaTeX template, `resume.cls`, and the Actions workflow shape are his. The
`LICENSE` in this repo is his original notice, unchanged.

What I changed: moved the resume content out of hardcoded Go maps and a
LinkedIn scraper into `resume.yaml`, made the LinkedIn fetch optional, and
added per-project URL overrides so private repos link somewhere that resolves.

## How it works

```
resume.yaml  ──┐
               ├──> go run . ──> resume.tex ──> pdflatex ──> resume.pdf
GitHub API   ──┘
```

- `resume.yaml` is the source of truth. Bullets, dates, contact details.
- The GitHub GraphQL API supplies live star counts, repo URLs and the
  language list.
- LinkedIn is optional and only fills sections `resume.yaml` left empty.
  Without `LINKEDIN_API_KEY` it is skipped entirely.

## Setup

```bash
go mod tidy
cp .env.example .env   # add TOKEN
go run .
```

`TOKEN` is a GitHub PAT. It needs `repo` scope only if you list a private
repo in `resume.yaml`; a classic token with no scopes works for public repos.

Set `LOCAL=true` to cache API responses to `github_data.json` and reuse them,
which keeps you off the API while iterating on the template.

Compiling the PDF locally needs a LaTeX distribution:

```bash
pdflatex -interaction=nonstopmode -halt-on-error resume.tex
```

CI installs TinyTeX for this, so a local LaTeX install is optional.

## Editing

Everything lives in `resume.yaml`. Two things worth knowing:

**Private repos need an explicit `url`.** A PAT with `repo` scope sees your
private repos, so they render fine for you and 404 for everyone else. Point
`url` at a live site instead:

```yaml
- name: ExamDB
  repo: examdb
  url: https://examdb.org
```

**Wrap metrics in quotes to bold them.** `"194" exams` renders as **194**
exams.

Text is escaped for LaTeX automatically, and non-ASCII punctuation is folded
to ASCII, because `resume.cls` loads `article` with no `inputenc`.

## Deploy

Three secrets, all optional except the first:

| Secret | Needed for |
|---|---|
| `TOKEN` | GitHub API. Required. |
| `LINKEDIN_API_KEY` | RapidAPI LinkedIn scraper. Skipped if unset. |

The workflow runs on push, Sundays at midnight UTC, and manual dispatch.

## Tests

```bash
go test ./...
```

## License

MIT, per the upstream project. See [`LICENSE`](LICENSE).
