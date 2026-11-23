# Security Overview

This page summarizes how Larkspur Cloud protects customer data. Detailed reports are available to customers under NDA from the trust portal.

## Encryption

All data is encrypted at rest with AES-256. Each bucket has its own data key, and data keys are wrapped by a regional master key held in a hardware security module. Master keys are rotated every 90 days; old keys are kept only to decrypt data written before the rotation.

Data in transit is protected with TLS 1.3. TLS 1.2 is still accepted for older clients until June 30, 2026, after which only TLS 1.3 will be accepted. Plain HTTP requests are rejected rather than redirected.

Business and Enterprise customers can bring their own key. With customer-managed keys, revoking the key makes the data unreadable within five minutes.

## Identity and access

Organizations can require multi-factor authentication for every member. Multi-factor authentication is always required for members with the Owner or Admin role, and it cannot be turned off for them.

Single sign-on with SAML 2.0 is available on Business and Enterprise. SCIM provisioning, which creates and removes members automatically from your identity provider, is available on Enterprise only.

API keys can be limited to specific buckets, actions, and source IP ranges. Keys created after January 2025 expire after one year by default.

## Audit logs

Every console action and every API call that changes configuration is written to the audit log. Audit logs are retained for 400 days and can be streamed to an external SIEM over syslog or HTTPS.

## Compliance

Larkspur Cloud holds a SOC 2 Type II report, renewed every year, and ISO/IEC 27001 certification. The platform is not currently certified for PCI DSS, so it should not be used to store full payment card numbers.

## Reporting a vulnerability

Report security issues to security@larkspur.example. We acknowledge reports within two business days and run a bug bounty that pays up to $10,000 for critical findings.
