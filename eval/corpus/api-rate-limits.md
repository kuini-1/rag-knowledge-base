# API Rate Limits

Rate limits protect shared capacity. They are applied per organization, per region, and they count every API request, including requests that fail.

## Limits by plan

| Plan | Requests per minute | Burst | Concurrent uploads |
|---|---|---|---|
| Starter | 120 | 20 | 2 |
| Team | 1,200 | 200 | 16 |
| Business | 6,000 | 1,000 | 64 |
| Enterprise | Negotiated | Negotiated | Negotiated |

The burst allowance lets a client exceed the per-minute rate for a few seconds. It refills at the plan's per-minute rate.

## Response headers

Every API response carries three headers so clients can pace themselves:

- `X-Larkspur-RateLimit-Limit`: the per-minute limit for your plan.
- `X-Larkspur-RateLimit-Remaining`: requests left in the current window.
- `X-Larkspur-RateLimit-Reset`: seconds until the window resets.

## When you hit the limit

Requests over the limit fail with HTTP 429 and error code LS-4290. The response includes a `Retry-After` header with the number of seconds to wait. Clients should back off exponentially and add jitter; the official SDKs do this automatically and retry up to five times.

Repeatedly ignoring `Retry-After` for more than ten minutes can trigger a temporary block of up to one hour, reported as error LS-4291.

## Raising your limits

Business customers can request a higher limit from the console under Settings, then Quotas. Requests are reviewed within two business days. Team customers must upgrade to Business to raise limits.

## Exempt endpoints

Health checks against `/v2/status` and token refreshes against `/v2/auth/refresh` do not count toward the limit.
