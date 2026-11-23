# Backups and Recovery

Larkspur Cloud takes automatic snapshots of block volumes and of bucket metadata. Object data itself is protected by replication across availability zones, and by versioning if you enable it.

## Snapshot schedule

Snapshots run every 24 hours. Business and Enterprise customers can add an hourly schedule for any volume.

## Retention

How long snapshots are kept depends on the plan:

- Team: 7 days
- Business: 30 days
- Enterprise: 90 days, or longer by contract

Starter volumes are not snapshotted.

## Point-in-time recovery

On Business and Enterprise, the managed database add-on supports point-in-time recovery to any second within the retention period. On Team, the database can only be restored to a daily snapshot.

## Restoring

Restores are started from the console or with `lark volume restore`. A restored volume is created next to the original; nothing is overwritten. Our recovery time objective is 30 minutes for volumes up to 1 TB, and the recovery point objective is 24 hours on the daily schedule or one hour on the hourly schedule.

## Testing restores

We recommend restoring a recent snapshot to a scratch volume at least once a quarter. Restored volumes are billed like any other volume, so delete them after the test.

## What is not backed up

Deleted buckets cannot be recovered by support once the 30-day soft-delete period ends. Objects deleted from a bucket without versioning cannot be recovered at all.
