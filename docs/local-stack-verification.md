# Local stack verification

Issue #4. Written 2026-10-04.

Issue #4 asks the least interesting question in the project: does `make up` +
`make seed` actually produce a working stack, given that every later issue assumes
it does. Answered by doing it, from an empty database.

## Result

| Check | Result |
|---|---|
| `go mod tidy` | **no change** — `go.sum` was already committed and complete |
| `make up` | mongodb container **healthy** |
| `GET /health` | **200** `{"status":"ok"}` |
| `make seed` from an empty database | **111 ayahs, 4 reciters, 8 placeholder recitations** |
| `make seed-check` | **PASS** — 111 byte-identical to quran.com, 0 bad hashes |
| `GET /surah/12` | **200**, all 111 ayahs rendered, reciter switcher present |
| Re-running `make seed` | `modified 0, upserted 0` — idempotent |

One real defect was found and fixed: **`make seed` did not work at all.**

## The defect: `make seed` pointed at a deleted file

```
go run ../scripts/seed_quran.go     # no such file
```

`scripts/seed_quran.go` was the 2-placeholder-ayah scaffold. Issue #2 replaced it
with `backend/cmd/seed_quran`, which loads the verified Uthmani corpus and verifies
it against three cross-check sources. The Makefile kept calling the old path, so the
target failed on the first line — silently enough that nothing in the repo noticed,
because the corpus had been loaded once by hand before the rename.

That is #4's acceptance criterion failing outright: a fresh clone plus `make up`
plus `make seed` needs manual fixes. It now calls the real loader.

## Two things the issue got wrong about itself

**"confirm all 3 containers report healthy/running."** `docker-compose.yml` defines
one service. Backend and web are deliberately not containerized — the Go API and the
Next.js dev server are run from source by `make dev` and reload on edit, which is
the point of the local loop. One container is the correct number.

**"Run `make seed` and confirm the placeholder ayahs load."** There are no
placeholder ayahs to load any more. The corpus is the verified Uthmani text of Surah
Yusuf. What is still placeholder is the recitation audio (issue #14): 8 rows, two
per reciter, covering ayahs 1–2 only, which is why the reader shows audio controls
on the first two verses and nowhere else.

## A papercut worth knowing about

`.env` lives at the repository root. Every backend command calls
`godotenv.Load()`, which reads `./.env` and nothing else — and the commands are all
documented as `cd backend && go run ./cmd/...`. So `.env` loading has never fired
from `backend/`; it only works if you run from the root or export the variables
yourself.

Rather than change the loader in eight commands, `make seed` now sources `.env`
itself before invoking them. That is also why a developer pointing `MONGO_URI` at
Atlas gets Atlas: the old target hardcoded `mongodb://tadbor:...@localhost:27017`,
which would have quietly overridden `.env`. `cmd/seed_quran` and
`scripts/seed_recitation.go` also gained the `godotenv.Load()` call every other
command has, so running them from the repository root works too.

## What was *not* broken

I reported the surah reader as rendering blank, and that was wrong. It renders all
111 ayahs. The text is Uthmani, so a search for the bare string `يوسف` misses it —
the corpus spells it `يوسُفُ`. The check that works is on the unvocalised prefix:

```
$ curl -s localhost:3000/surah/12 | grep -c 'ٱلْكِتَـٰبِ'
1
```

`web/next.config.js` already defaults `NEXT_PUBLIC_API_URL` to
`http://localhost:8080`, so the reader needs no environment variable of its own.
`make dev` sets none and needs none.

Worth recording because the failure mode is real even though this instance was not
it: `fetchList` in `web/app/api-client/index.ts` turns any unparseable or failed
response into an empty list, so a reader with a broken API URL renders an empty page
at HTTP 200. That is a deliberate choice — a dropped row beats an `any` that
type-checks — but it means a 200 from `/surah/12` is not evidence that the reader
works. The evidence is ayah text in the response.

## Reproducing

```bash
make up-d          # MongoDB
make seed          # 111 ayahs + reciters (idempotent)
make seed-check    # re-verify the corpus, write nothing
make dev           # MongoDB + API :8080 + web :3000

curl -s localhost:8080/health
curl -s localhost:3000/surah/12 | grep -c 'ٱلْكِتَـٰبِ'
```

`make seed-check` is the one to reach for when a verse looks wrong. Reseeding
re-fetches from quran.com; it does not tell you the stored text is right.

## Open items

- **Recitation audio is still placeholder** — 8 rows, ayahs 1–2,
  `https://example.com/placeholder/...`. Issue #14. The reader renders audio controls
  that will 404.
- **Explanations are absent.** `/surahs/12/explanations?style=simplified_ar` returns
  `null`, so the reader shows Quran text with no commentary under it. That is
  correct for now — generation is issue #10 — but it means the product's actual
  feature is not visible in the local stack yet.
- **Deploying changes the shape of this.** Render's free web service has no
  persistent disk, so Day 5 (issue #15) needs a hosted `MONGO_URI` (Atlas M0) and a
  real build of the Next.js app rather than a dev server. `web/next.config.js`'s
  localhost fallback is a local-loop convenience and has no meaning in production,
  where the URL must come from the environment.