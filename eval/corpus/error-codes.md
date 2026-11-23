# Error Codes

Every error response from the Larkspur API has a machine-readable code in the form `LS-NNNN`, an HTTP status, and a human-readable message. The first digit of the code groups errors by cause: 1 for authentication, 3 for validation, 4 for limits and quotas, 5 for service-side problems.

## Authentication

| Code | HTTP | Meaning | What to do |
|---|---|---|---|
| LS-1001 | 401 | The API key is missing or malformed. | Send the key in the Authorization header as `Bearer <key>`. |
| LS-1002 | 401 | The API key was revoked or has expired. | Create a new key in the console. |
| LS-1007 | 403 | The key is valid but lacks the scope for this action. | Add the scope to the key or use a different key. |

## Validation

| Code | HTTP | Meaning | What to do |
|---|---|---|---|
| LS-3004 | 400 | The bucket name is invalid. Names must be 3 to 63 lowercase letters, digits, or hyphens. | Choose a valid name. |
| LS-3012 | 409 | The bucket name is already taken in this region. | Pick another name; bucket names are unique per region. |
| LS-3020 | 413 | The object is larger than 5 TB, the maximum object size. | Split the object, or use multipart upload for anything above 100 MB. |

## Limits and quotas

| Code | HTTP | Meaning | What to do |
|---|---|---|---|
| LS-4130 | 403 | The Starter storage limit has been reached. | Delete data or upgrade. |
| LS-4290 | 429 | The rate limit was exceeded. | Wait for the number of seconds in `Retry-After`. |
| LS-4291 | 429 | The client was temporarily blocked for ignoring `Retry-After`. | Wait up to one hour, then retry with backoff. |

## Service

| Code | HTTP | Meaning | What to do |
|---|---|---|---|
| LS-5031 | 503 | The region is temporarily unavailable. | Retry with backoff, or fail over to a replica region. |
| LS-5040 | 504 | An upstream storage node timed out. | Retry the request; it is safe for idempotent operations. |

## Request IDs

Each response includes an `X-Larkspur-Request-Id` header. Include it when you contact support; it lets the team find the request in the logs, which are kept for 400 days.
