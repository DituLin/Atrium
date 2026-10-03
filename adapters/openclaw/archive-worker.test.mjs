import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import {
  strictParse,
  validateSchema,
  validateRoute,
  MODEL,
} from "./archive-worker.mjs";
test("strict model JSON rejects ambiguity, depth, null and resource growth", () => {
  for (const value of [
    '{"a":1,"a":2}',
    "null",
    "[1] trailing",
    "[".repeat(40) + "]".repeat(40),
    '"' + "a".repeat(65536) + '"',
  ])
    assert.throws(() => strictParse(value));
  assert.deepEqual(strictParse('{"a":[1,true,"x"]}'), { a: [1, true, "x"] });
});
test("schema is strict and requires classification coverage", async () => {
  const schema = JSON.parse(
    await readFile(
      new URL(
        "../../internal/archive/understanding/extraction-v1.schema.json",
        import.meta.url,
      ),
      "utf8",
    ),
  );
  const result = {
    domain: "health",
    document_type: "new_subtype",
    subjects: [],
    fields: [],
    events: [],
    coverage: [],
    issues: [],
  };
  assert.doesNotThrow(() => validateSchema(result, schema));
  for (const bad of [
    { ...result, global_entity_id: "x" },
    { ...result, fields: null },
    { ...result, domain: 123 },
  ])
    assert.throws(() => validateSchema(bad, schema));
});
test("only the verified direct provider route and terminal stop are accepted", () => {
  const r = {
    provider: "deepseek",
    model: "deepseek-v4-flash",
    responseModel: "deepseek-flash",
    stopReason: "stop",
    execution: {
      mode: "direct-provider",
      owner: { kind: "provider", id: "deepseek" },
    },
  };
  assert.doesNotThrow(() => validateRoute(r));
  assert.equal(MODEL, "deepseek/deepseek-v4-flash");
  for (const stopReason of ["length", "toolUse", "error", "aborted", undefined])
    assert.throws(() => validateRoute({ ...r, stopReason }));
  assert.throws(() =>
    validateRoute({ ...r, execution: { mode: "isolated-agent-runtime" } }),
  );
});
test("packaged prompt and schema equal versioned source resources", async () => {
  const { schema, prompt, schemaSource } =
    await import("../../internal/archive/understanding/resources-v1.mjs");
  assert.equal(
    schemaSource,
    await readFile(
      new URL(
        "../../internal/archive/understanding/extraction-v1.schema.json",
        import.meta.url,
      ),
      "utf8",
    ),
  );
  assert.deepEqual(
    schema,
    JSON.parse(
      await readFile(
        new URL(
          "../../internal/archive/understanding/extraction-v1.schema.json",
          import.meta.url,
        ),
        "utf8",
      ),
    ),
  );
  assert.equal(
    prompt,
    await readFile(
      new URL(
        "../../internal/archive/understanding/prompt-v1.txt",
        import.meta.url,
      ),
      "utf8",
    ),
  );
});
