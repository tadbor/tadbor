# Batch generation: what the pipeline does, and what it caught

## The job

`make generate` runs `backend/cmd/generate_explanations -live`. For each of the
111 ayahs of Surah Yusuf it retrieves evidence, calls the LLM with
`task: "simplify"`, validates the draft, and writes a pending `explanations`
document for a human to review.

```
go run ./cmd/generate_explanations              # preview, no provider calls
go run ./cmd/generate_explanations -dry-run     # resolve evidence, skip the LLM
go run ./cmd/generate_explanations -live
go run ./cmd/generate_explanations -live -limit 8
go run ./cmd/generate_explanations -check       # gate: 111 pending or exit 1
```

## It cannot write anything yet, and that is the guardrail

Both retrieval paths filter `source_verified: true`. The committed Ibn Kathir
text is still `unverified` (see `docs/reviews/ibn-kathir-ar-surah-12.md`), so
every ayah returns `ErrInsufficientEvidence` and the job writes **zero**
explanations and 111 audit records saying so. Verified directly: against a
database holding only the verified Quran corpus and no approved Tafsir, a run
produced 0 explanations and 5 `insufficient_evidence` records for the first 5
ayahs.

That is Part 1 §12 working. The empty result is the rule, not a bug to route
around, and nothing in the job can be persuaded to write anyway.

## The free tier allows 5 requests a minute

Measured from what the provider actually returns, not from a quota page:

```
429 Too Many Requests
  Quota exceeded for metric: generativelanguage.googleapis.com/
  generate_content_free_tier_requests, limit: 5
```

The first version of this job defaulted to `-pace 2s`, which asks for 30 requests
a minute. It lost 3 of its first 5 ayahs to 429 and 503, and no amount of client
retrying fixes a quota you are exceeding on every call. The default is now 13s —
5 a minute with headroom — which puts a full run at about 24 minutes. That is
the real cost of the free tier and it belongs in the open rather than behind a
fast default.

After any provider failure the next ayah waits 45s regardless of `-pace`, so one
throttling event does not turn into a run where every ayah fails.

## "simplify" was not simplifying

Found by generating for real and reading the output. Asked to simplify, Gemini
returned a shortened paraphrase of Ibn Kathir in the same register — scholarly
Arabic, narration chains intact, attributed to the scholar by name:

> نزل القرآن الكريم باللغة العربية لأن لغة العرب هي أفحص اللغات وأوضحها... إذ نزل
> بأشرف اللغات، وهو أشرف الكتب، ونزل على أشرف الرسل، بواسطة أشرف الملائكة، في
> أشرف بقاع الأرض

Every validation check passed on that draft. It was fluent, faithful, grounded,
correctly cited, within the length bound, and in the right script — and still
useless to a beginner. No automated check in §19 can catch this; it is a question
about audience, and it is the reviewer's question, so the fix belongs in the
prompt.

`taskBrief` now states what each task means. For `simplify`, aimed at someone
who knows the alphabet and nothing about tafsir: plain modern Arabic, a few short
sentences, no narration chains, no technical asbab, and **do not narrate your
source** — never "Ibn Kathir says" or "it is reported that". Same evidence, same
verse:

> تبين الآية أن الله يقدم في القرآن أحسن القصص وأصدقها للنبي عن طريق الوحي.
> وجاء هذا البيان حين طلب الصحابة سماع القصص والأحاديث، فأرشدهم الله إلى أن ما
> نزل في القرآن هو الأكمل، وأنه كتاب كافٍ يغني عما سواه لوضوحه وكماله

The brief is per-task rather than global because the tasks fail in opposite
directions — `simplify` needs its register lowered, `translate` needs it
preserved — and one global instruction would average them into something that
helps neither.

## Prompt version is derived, not maintained

`generation.PromptVersion()` hashes the system prompt plus every task brief. So
editing any of them changes the version automatically, and a review record can
never claim a prompt version that no longer describes the prompt. The edit above
moved it `sys-41c57d85e01b` → `sys-800a3cca6810` with no manual step — which is
exactly how it was caught on the probe run, where a stale draft from the old
prompt was visible in the review queue next to new ones.

`ModelVersion()` reads the model out of `LLM_API_URL` rather than hardcoding it,
for the same reason: a review record naming a model this pipeline did not use is
worse than one naming none.

## Re-running is safe, and will not overwrite a human

A 24-minute run against a rate limit will get interrupted, so re-running has to
be safe. Three cases, and the difference between them matters:

| Existing explanation | Action |
| --- | --- |
| Written by this run and prompt | skip, counted as `already_current` |
| `review_status` is not `pending` | skip, counted as `decided_skipped` |
| Pending, older run or older prompt | regenerate |

The middle case is the reason this is not just an upsert. Regenerating over an
approved explanation would delete the text a reviewer approved and silently
replace the citation history behind it, so a decided draft is never touched.

Observed on the probe: after regenerating two ayahs under the new prompt, a
re-run reported `already_current: 2` and made **one** provider call instead of
three.

## Validation, and what §19 still does not cover

`generation.Validate` covers what is decidable from the request and the draft:
non-empty content, length, `requires_review`, citation coverage, script. Its
three §19 database-dependent checks live here, because this job has the handles:

- **Source validation** — every cited `source_ref` resolves to a chunk whose
  source is `verified`.
- **Verse mapping** — every cited chunk actually covers the ayah being
  explained. This catches a generation-layer mix-up where ayah 12's draft is
  grounded in ayah 40's commentary.
- **Quran text integrity** — the verse under discussion is read back from the
  verified corpus and must be present and non-empty, so a truncated or missing
  row cannot be the subject of an explanation.

Grounding is checked **before** the provider is called, so an ayah that cannot be
grounded costs no request.

**Not checked, stated plainly because §19 reads as if it might be:** no attempt is
made to verify Arabic quoted inside the model's own output against the corpus.
That needs quote extraction, and a wrong extraction would reject drafts for the
wrong reason. A misquotation is caught by the reviewer's "no added or removed
meaning" check in §20 — a human judgement about meaning, not a string match.

## Failures are recorded, not skipped

Three outcomes per ayah, treated differently on purpose:

- `insufficient_evidence` — no approved commentary yet. Not an error: the honest
  record is "we looked and there is nothing", kept distinct so a coverage gap is
  visible rather than inferred from a missing row.
- `provider_error` — the provider refused or failed. Not retried beyond the
  client's own budget, since it may be the outage that stops all 111.
- `validation_failed` — the draft failed a check. Stored **with the validation
  error and the raw output**, because a rejected draft is evidence about the
  prompt and discarding the text loses the only copy of why the model misbehaved.

Every attempt writes a `generation_runs` record — model, prompt version,
retrieved chunk ids, the evidence text, the raw structured output, and the
outcome — before any explanation is written. `run_id` is deterministic
(`s012a037`), so a re-run replaces its own audit record instead of accumulating
one per attempt.

`generation_runs` is also where a rejected draft is readable:

```
db.generation_runs.find({ayah: 37, outcome: "validation_failed"})
```

## Verified

Against a throwaway database (`tadbor-probe-gen`) built by ingesting the
manifest with the source temporarily marked verified **in the probe only**. The
real `tadbor` database was never written to: 0 `tafsir_chunks`, 0
`explanations`, 0 `generation_runs`, source still `unverified` after every run.
Both probe databases were dropped afterwards.

- Retrieval resolves evidence for 111/111 ayahs in the probe
  (`check_coverage`: 111/111 searchable, 454 chunks, no mapping drift).
- Explanations written, then regenerated under the new prompt, with
  `source_version_snapshot`, `prompt_version`, `model_version` and
  `generation_run_id` on each document.
- The full chain reads back through the reviewer dashboard: 3 pending items,
  each with its verified Uthmani verse, resolved evidence chunks, no
  `missing_refs`, and correct provenance.
- `-check` exits non-zero at 3 of 111 — the gate fails when coverage is short.
- 429 and 503 handled: recorded as `provider_error`, no partial document written,
  run exits non-zero.

## Not done

- **No Egyptian Arabic or English.** This job runs one style; #13 covers the
  others.
- **No `level` beyond `beginner`.** The flag exists and is written to the
  document, but nothing generates `tadabbur` or `advanced` yet.
- **One model, one prompt at a time.** No A/B comparison of prompts, so
  "this prompt is better" rests on reading output, not on measurement.