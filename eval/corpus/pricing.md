# Larkspur Cloud Pricing

Larkspur Cloud bills monthly in arrears. Every plan includes object storage, block volumes, and the managed queue service. Prices below are in US dollars and exclude local taxes.

## Plans at a glance

| Plan | Monthly price | Included storage | Included egress | Team seats | Support tier |
|---|---|---|---|---|---|
| Starter | $0 | 25 GB | 50 GB | 1 | Community |
| Team | $49 | 500 GB | 1 TB | 10 | Standard |
| Business | $399 | 5 TB | 10 TB | 50 | Priority |
| Enterprise | Custom | Custom | Custom | Unlimited | Premier |

The Starter plan is meant for evaluation and hobby projects. Workloads on Starter are placed on shared capacity and may be paused after 30 days without any API activity.

## Overage rates

When you use more than your plan includes, the extra usage is billed at the end of the month.

- Additional storage costs $0.018 per GB-month on Team and $0.015 per GB-month on Business.
- Additional egress costs $0.04 per GB on Team and $0.03 per GB on Business. Egress between two Larkspur regions is billed at half the internet egress rate.
- Additional team seats cost $9 per seat per month on Team. Business includes 50 seats; each seat beyond that costs $7 per month.

Starter accounts cannot buy overage. Once a Starter account reaches its storage limit, new writes are rejected with error LS-4130 until data is deleted or the account is upgraded.

## Annual billing

Paying annually upfront gives a 17% discount on the Team and Business plan fees. The discount applies to the plan fee only, not to overage. Annual plans renew automatically unless cancelled at least 14 days before the renewal date.

## Enterprise

Enterprise contracts start at 12 months and are priced per deployment. They add dedicated capacity, a named technical account manager, custom data residency, and the 99.99% uptime commitment described in the SLA. Contact sales to request a quote; a typical quote takes three business days.

## Legacy plans

Customers who signed up before March 1, 2025 may still be on the legacy Pro plan, which cost $79 per month and included 1 TB of storage. The Pro plan is closed to new customers. Legacy Pro customers keep their price until they change plans, after which the current plan prices apply.

## Free trial

New organizations get a 21-day trial of the Business plan with no credit card required. At the end of the trial, the organization moves to Starter unless a payment method has been added.
