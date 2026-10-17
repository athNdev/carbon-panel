# Issue 012: AUTH-1 - Login Throttle Memory Bounding & Expiration Pruning

## Context & Problem
`LoginThrottle` provides in-memory brute-force protection keyed by username and client IP. However:
1. `clearExpiredLocked` was only invoked when `len(entries) >= 100,000`.
2. Under distributed or slow credential-stuffing attacks across thousands of unique IPs or usernames, entries would accumulate in the in-memory map without ever being cleaned up until 100,000 entries filled memory.
3. Checking `Allow(key)` on an expired record did not evict it from memory.

## Solution
1. **Inline Pruning on Allow**: When `Allow(key)` finds that both the sliding window and the lockout period have elapsed, it deletes the key inline and returns `true, 0`.
2. **Proactive Pruning on Failure**: Lowered the opportunistic prune trigger from 100,000 to `pruneThreshold = 1000`. If active tracked keys reach 1,000, expired keys are swept immediately during `Failure(key)`.
3. **Public Inspection & Sweep APIs**: Added `Prune() int` (sweeps expired keys and returns count removed) and `Count() int` for telemetry and health checks.
4. **Unit Tests**: Added test suites verifying inline pruning on `Allow`, sweep on `Prune`, and key eviction when window elapses.
