import { Ajv2020, type AnySchema, type ValidateFunction } from "ajv/dist/2020.js";

export function compileSchema(schema: AnySchema): ValidateFunction<unknown> {
  const validator = new Ajv2020({
    allErrors: true,
    strict: true,
  });

  return validator.compile<unknown>(schema);
}
