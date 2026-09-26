# Addendum — MongoDB Pivot

The documents in this folder (Parts 1–2, build-deploy-guide) were written
against **PostgreSQL + pgvector**. The actual repo uses **MongoDB** instead,
per a later decision. The core architecture (source registry, verse mapping,
retrieval guardrails, generation contract, human review gate) is unchanged —
only the storage layer differs. Read the docs with this substitution in mind:

| Docs say | Repo actually uses | Why it still works |
|---|---|---|
| Postgres tables (`ayahs`, `sources`, `tafsir_chunks`, ...) | MongoDB collections with the same names/fields | Same schema shape, document store instead of relational — no foreign keys, so relationships (e.g. `chunk_ayah_mappings`) are denormalized onto the chunk document instead of a join table |
| `pgvector` similarity search | Brute-force cosine similarity computed in Go (`internal/retrieval/service.go`) | Self-hosted MongoDB has no built-in ANN vector index (that's a MongoDB Atlas-only feature); at one-surah scale, brute force over a few hundred chunks is fast enough. Revisit if the corpus grows a lot — see Part 2 §31's scaling triggers |
| Supabase/Neon free tier | Local Dockerized MongoDB (this repo) or MongoDB Atlas free tier for a hosted option | Either works; the code doesn't care which, only `MONGO_URI` changes |

Everything else in Parts 1–2 (source tiers, human review workflow, generation
contract, hallucination prevention layers, ADRs) applies unchanged.
