# Implementation Plan: Refund audit

## Project Structure

### Documentation (this feature)

```text
specs/003-refund-audit/
├── plan.md              # This file
└── tasks.md             # Not created yet
```

### Source Code (repository root)

<!--
  ACTION REQUIRED: Replace the placeholder tree below.
-->

```text
# [REMOVE IF UNUSED] Option 1: Single project (DEFAULT)
src/
├── payments/
│   ├── retry.ts         # existing file
│   └── refunds.ts       # new file in an existing directory
└── orders/
    ├── checkout.ts
    └── [same as payments above]

tests/
└── refunds.test.ts      # its directory does not exist

ios/ or android/
```
