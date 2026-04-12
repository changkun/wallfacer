---
title: Billing Idempotency
status: drafted
depends_on:
  - specs/cloud/cloud-infrastructure.md
  - specs/shared/authentication.md
affects:
  - internal/billing/
  - internal/cloud/
effort: small
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Billing Idempotency

## Problem

When wallfacer cloud charges a user for task usage via Stripe, network failures can cause retries. Without idempotency keys, a retry creates a second charge — the user is double-billed. For a trust-focused product where cost visibility is the central story, double-charging is catastrophic.

Stripe natively supports idempotency keys for exactly this case. We must use them on every charge operation.

## Scope

All Stripe API calls that modify state (create charge, create invoice, create subscription, etc.) use a deterministic idempotency key. Read operations do not need keys.

## Design

### Key construction

For each chargeable event, construct an idempotency key that is deterministic and unique per event:

```
idempotency_key = sha256("charge:" + user_id + ":" + task_id + ":" + turn_id)
```

For subscription-level operations (monthly invoice, plan change), use the relevant unique IDs:
```
idempotency_key = sha256("invoice:" + user_id + ":" + billing_period_start)
```

Store the key alongside the event in `internal/billing/` so retries can reconstruct it identically.

### Retry policy

- First attempt: generate + persist idempotency key, call Stripe
- Network failure: retry up to 5 times with exponential backoff (1s, 2s, 4s, 8s, 16s)
- Stripe response: if Stripe returns the same key's prior result, treat as success (Stripe deduped)
- Persistent failure after 5 retries: mark billing event as `failed`, alert via internal metrics, user sees "Payment pending" banner

### Testing

- Unit: key generation is deterministic for same inputs
- Integration: simulate network failure mid-charge, verify retry succeeds without duplicate charge
- Stripe test mode: run full flow including "card declined" to verify error handling

## Implementation

- `internal/billing/idempotency.go` — key generation
- `internal/billing/stripe.go` — Stripe client wrapper with retry
- `internal/billing/billing_test.go` — tests

## Success

- Zero double-charge incidents in production
- CI test proves retry-on-failure does not double-charge
- Runbook entry for "what to do if billing event is stuck in failed state"
