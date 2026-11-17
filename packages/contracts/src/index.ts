import { Ajv2020, type AnySchema, type ValidateFunction } from "ajv/dist/2020.js";

export interface Violation {
  /** JSON Pointer into the validated document; "" is the document root. */
  instancePath: string;
  message: string;
}

export type ValidationResult = { valid: true } | { valid: false; violations: Violation[] };

export interface ContractRegistry {
  validate(schemaId: string, document: unknown): ValidationResult;
}

/**
 * Compiles a set of Draft 2020-12 schemas that may reference each other by $id.
 * No reference is fetched over the network.
 */
export function createRegistry(schemas: readonly AnySchema[]): ContractRegistry {
  const ajv = new Ajv2020({ allErrors: true, strict: true });
  const ids: string[] = [];
  for (const schema of schemas) {
    const id = schemaId(schema);
    ajv.addSchema(schema, id);
    ids.push(id);
  }

  const validators = new Map<string, ValidateFunction<unknown>>();
  for (const id of ids) {
    const validator = ajv.getSchema<unknown>(id);
    if (validator === undefined) {
      throw new Error(`schema ${id} did not compile`);
    }
    validators.set(id, validator);
  }

  return {
    validate(id, document) {
      const validator = validators.get(id);
      if (validator === undefined) {
        throw new Error(`unknown schema ${id}`);
      }
      if (validator(document)) {
        return { valid: true };
      }
      return {
        valid: false,
        // Ajv adds a summary error for a failed if/then branch beside the
        // errors that caused it. Drop it so only the causes remain, matching
        // the Go adapter.
        violations: (validator.errors ?? [])
          .filter((error) => error.keyword !== "if")
          .map((error) => ({
            instancePath: error.instancePath,
            message: error.message ?? error.keyword,
          })),
      };
    },
  };
}

function schemaId(schema: AnySchema): string {
  if (typeof schema === "object" && typeof schema.$id === "string" && schema.$id !== "") {
    return schema.$id;
  }
  throw new Error("schema has no $id");
}
