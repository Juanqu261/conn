import { type ChargeAttempt, shouldRetryCharge } from "../payments/retry.js";

/** Decide whether checkout should submit the charge again after a transport failure. */
export function nextCheckoutStep(attempt: ChargeAttempt): "retry" | "fail" {
  return shouldRetryCharge(attempt) ? "retry" : "fail";
}
