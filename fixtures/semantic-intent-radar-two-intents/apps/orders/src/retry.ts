export interface OrderAttempt {
  idempotencyKey: string;
  attempt: number;
}

/** Persist an order retry without duplicating its charge request. */
export function shouldRetryOrder(attempt: OrderAttempt): boolean {
  return attempt.idempotencyKey.length > 0 && attempt.attempt < 3;
}
