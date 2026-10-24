# Ingestion Notes - Issue #31 (Surah 12, Ibn Kathir)

## Status
- Source verified: `ibn-kathir-ar` marked verified (issue #30)
- Chunks present in MongoDB: 454 chunks, all with `source_verified: true`
- Coverage: 111/111 ayahs covered, 0 gaps
- Embeddings: All 454 chunks have BAAI/bge-m3 vectors (1024 dims), hash verified

## Commands Used
```bash
# Check coverage
set -a && source .env && set +a
cd backend && go run ./cmd/check_coverage -source ibn-kathir-ar -surah 12

# Check embeddings
cd backend && go run ./cmd/embed_chunks -check
```

## Verification
- `make seed-check` and coverage checks pass
- Retrieval can serve evidence for Surah 12 ayahs from verified source
