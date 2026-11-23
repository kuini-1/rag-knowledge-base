# Regions

Larkspur Cloud runs in seven regions. Each region is fully independent: data written in a region stays there unless you set up replication.

| Region code | Location | Launched | Availability zones |
|---|---|---|---|
| us-oregon-2 | Oregon, USA | 2021 | 3 |
| us-virginia-1 | Virginia, USA | 2020 | 3 |
| eu-frankfurt-1 | Frankfurt, Germany | 2021 | 3 |
| eu-dublin-1 | Dublin, Ireland | 2023 | 2 |
| ap-singapore-1 | Singapore | 2022 | 3 |
| ap-bangkok-1 | Bangkok, Thailand | 2025 | 2 |
| ap-tokyo-1 | Tokyo, Japan | 2024 | 3 |

## Choosing a region

Pick the region closest to your users. Inside Southeast Asia, ap-singapore-1 and ap-bangkok-1 are about 30 milliseconds apart, so either works for most applications in the area.

The us-oregon-2 region replaced us-oregon-1, which was retired in 2023. Buckets that lived in us-oregon-1 were migrated automatically and kept their names.

## Feature availability

Most features are available in every region. The exceptions are:

- The managed queue service is not yet available in ap-bangkok-1.
- Customer-managed keys are available in every region except eu-dublin-1, where support is planned.
- GPU-backed image processing runs only in us-oregon-2 and ap-tokyo-1.

## Replication

Cross-region replication copies new objects to a second region within 15 minutes for 99% of objects. Replication must be enabled per bucket, and the destination region is billed for the stored copy. Replication between the two EU regions keeps data inside the European Union.
