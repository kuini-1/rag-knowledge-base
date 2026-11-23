# Incident Report: ap-bangkok-1 Object Storage Outage

## Summary

On August 14, 2025, the object storage API in ap-bangkok-1 returned errors for 47 minutes, from 02:13 to 03:00 UTC. During that time, 92% of requests to the region failed with error LS-5031. No data was lost, and no other region was affected.

## Impact

Customers with buckets in ap-bangkok-1 could not read or write objects. Customers who had enabled cross-region replication to ap-singapore-1 could read the replicated copies there. Monthly availability for the region fell to 99.89%, below the Business and Enterprise commitments, so affected customers on those plans received service credits automatically.

## Root cause

An internal load balancer in front of the storage nodes used a TLS certificate that expired at 02:13 UTC. The certificate had been issued manually when the region launched, before the automated renewal system covered internal endpoints. When it expired, the API gateways could no longer connect to the storage nodes and returned errors for every request.

## Detection and response

Synthetic monitoring alerted the on-call engineer at 02:15 UTC, two minutes after the first errors. The engineer identified the expired certificate at 02:38 UTC. A new certificate was deployed at 02:52 UTC, and error rates returned to normal by 03:00 UTC as connections were re-established.

## What we are changing

1. All internal certificates are now issued and renewed by the same automated system as public certificates. This was completed for every region on September 5, 2025.
2. Certificate expiry is now monitored, with alerts 30 days and 7 days before expiry.
3. Region launch checklists now include an audit of manually issued certificates.

We apologize for the disruption. Questions about this incident can be sent to support with the reference INC-2025-0814.
