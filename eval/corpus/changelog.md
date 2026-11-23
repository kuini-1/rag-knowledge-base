# API Changelog

Changes to the Larkspur API, newest first.

## v2.6 (2026-09-15)

- Added server-side copy between buckets in different regions.
- Object listing now returns up to 5,000 keys per page, up from 1,000.

## v2.5 (2026-04-02)

- New `If-Match` support on deletes to prevent removing an object that changed.
- The `/v2/usage` endpoint now reports egress per destination region.

## v2.4 (2025-11-20)

- Raised the Business plan rate limit from 4,000 to 6,000 requests per minute.
- Added error LS-4291 for clients that keep retrying without honoring `Retry-After`.

## v2.3 (2025-06-10)

- Raised the Team plan rate limit from 600 to 1,200 requests per minute.
- Presigned URLs can now be valid for up to 7 days, up from 24 hours.

## v2.0 (2024-10-01)

- Launched the v2 API with JSON error bodies that include `LS-` error codes.
- The v1 API is deprecated. It will stop accepting requests on 2026-12-31.

## Deprecation policy

A deprecated API version keeps working for at least 12 months after the deprecation notice. We email organization Owners 90 days and again 30 days before a version is shut down.
