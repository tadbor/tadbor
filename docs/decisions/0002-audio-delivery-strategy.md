# ADR 0002: Audio Delivery Strategy

- Status: Accepted
- Date: 2026-10-24
- Deciders: Maintainers

## Context
Surah 12 has 444 recitation files across 4 reciters (EveryAyah-sourced). Hotlinking rights are unresolved per `docs/recitation-audio-sources.md`. Delivery model affects backend, web, and mobile.

## Options Considered
1. **Stream from source (hotlink)** - Direct URLs from source; no storage cost but licensing unclear, less control.
2. **Stream from our own storage/CDN** - We host files; need rights cleared + storage/CDN cost.
3. **Temporary in-session cache** - Browser/app cache during session; still needs source or hosted files.
4. **Opt-in per-surah offline download** - User explicitly downloads; largest complexity (storage, eviction, deletion).

## Decision

**Per-platform recommendation:**

- **Web:** Stream from source (option 1) as interim if rights allow or with explicit attribution/decision pending, but **do not** auto-download all-Quran. Prefer option 2 only after rights cleared and hosting set up. Implement graceful fallbacks on failure.
- **Mobile:** Stream from source initially with same restrictions; opt-in per-surah download (option 4) only as a separate feature behind clear UX, storage limits, eviction, and deletion controls. Never auto-download entire Quran.
- **Backend:** Serve recitation metadata/URLs via existing API; support proxying/range requests only if we self-host (option 2). No bulk download endpoints.

**Constraints:**
- **All-Quran audio is never downloaded automatically** on any platform.
- **Behavior on failure/interruption/navigation:** Show unavailable/retry states; don't block reading; stop playback on navigation per UX rules.
- **Licensing:** Hotlinking rights unresolved - track per reciter in `docs/recitation-audio-sources.md`. If unresolvable, do not publish those reciters or host without clearance.
- **Attribution:** Display reciter attribution where shown.

## Consequences
- Interim approach minimizes cost while rights are clarified.
- Clear guardrails prevent accidental bulk downloads.
- If moving to self-hosting/offline, requires separate scoped issue with limits/eviction/deletion.

## References
- `docs/recitation-audio-sources.md`
- `backend/internal/recitation/*`
- `web/app/surah/[id]/page.tsx`
