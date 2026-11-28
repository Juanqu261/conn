# Implementation Plan: Checkout retries

**Branch**: `001-checkout-retries` | **Date**: 2026-11-20 | **Spec**: [spec.md](spec.md)

## Summary

Reuse one idempotency key across retries. tasks.md exists, so Conn ignores the structure block below.

## Project Structure

### Source Code (repository root)

```text
src/
└── billing/
    └── ignored-because-tasks-exist.ts
```
