# Service Level Agreement

This agreement describes the availability Larkspur Cloud commits to and the credits you receive when we miss it. It applies to paid plans only; Starter has no availability commitment.

## Uptime commitments

Availability is measured per region, per calendar month, as the percentage of minutes in which the object storage API answered at least 99% of valid requests without a server error.

### Team

This plan commits to 99.9% monthly availability. That allows about 43 minutes of downtime in a 30-day month before credits apply.

### Business

This plan commits to 99.95% monthly availability, which is roughly 22 minutes of downtime in a 30-day month. It also covers the managed queue service, which Team does not.

### Enterprise

This plan commits to 99.99% monthly availability, measured across the customer's dedicated capacity. Multi-region deployments are measured on the combined availability of all their regions.

## Service credits

If monthly availability falls below the commitment, you receive a credit on the next invoice. Credits are a percentage of that month's plan fee for the affected region.

| Monthly availability | Credit |
|---|---|
| Below the commitment but at least 99.0% | 10% |
| Below 99.0% but at least 95.0% | 25% |
| Below 95.0% | 50% |

Credits are capped at 50% of the monthly plan fee and cannot be exchanged for cash.

## Claiming a credit

To claim a credit, open a support ticket within 30 days of the end of the affected month and include the region and the time ranges you observed errors. We confirm or decline the claim within 10 business days.

## Exclusions

The commitment does not cover downtime caused by scheduled maintenance announced at least 72 hours in advance, by the customer exceeding documented rate limits, by suspension for non-payment, or by events outside our reasonable control. Scheduled maintenance windows are limited to four hours per month per region.
