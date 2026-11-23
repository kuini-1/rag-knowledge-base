# Data Retention and Deletion Policy

## Purpose

This policy explains how long Larkspur Cloud keeps customer data, logs, and backups, and what happens when you delete data or close your account.

## Deleted objects and buckets

When you delete an object from a bucket with versioning turned off, the object is removed immediately and cannot be recovered. With versioning on, deleting an object adds a delete marker and the previous versions remain until a lifecycle rule removes them.

Deleted buckets are kept in a soft-deleted state for 30 days. During that time, an Owner can restore the bucket from the console. After 30 days, the bucket and all of its contents are permanently erased.

## Account closure

When an organization is closed, its data is retained for 60 days so that the organization can be reopened by contacting support. After 60 days, all buckets, volumes, and snapshots are permanently deleted, and the deletion is confirmed by email to the former Owners.

## Logs

Access logs that customers enable on a bucket are kept for as long as the customer chooses. Larkspur's own request logs, which are used for support and security investigations, are retained for 400 days and then deleted.

## Backups of deleted data

Internal backups used for disaster recovery are kept for 35 days. Data deleted by a customer can therefore remain in internal backups for up to 35 days after deletion, but it is never restored from those backups into a customer account.

## Legal holds

If we receive a valid legal request to preserve data, deletion of the affected data is suspended until the hold is lifted. We notify the customer of a legal hold unless the law forbids it.

## Requests

To request deletion of personal data under GDPR or the Thai Personal Data Protection Act, email privacy@larkspur.example. We respond within 30 days.
