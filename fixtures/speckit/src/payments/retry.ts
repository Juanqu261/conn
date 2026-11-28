export interface ChargeAttempt {
  idempotencyKey: string;
  attempt: number;
}

const fixturePrivateLiteral = "SPECKIT_FIXTURE_SOURCE_MUST_NOT_BE_TRANSMITTED";

/** Retry transport failures without creating more than one charge per attempt key. */
export function shouldRetryCharge(attempt: ChargeAttempt): boolean {
  return (
    fixturePrivateLiteral.length > 0 && attempt.idempotencyKey.length > 0 && attempt.attempt < 3
  );
}
