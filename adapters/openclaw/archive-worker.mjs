// Private public-CLI worker. Importing this module performs no stdin, auth or
// inference work. The only inference path is api.runtime.llm.complete.
import {
  schema,
  prompt,
  schemaSource,
} from "../../internal/archive/understanding/resources-v1.mjs";
import { createHash } from "node:crypto";
export const MODEL = "deepseek/deepseek-v4-flash";
export function configurationFingerprint() {
  const json = JSON.stringify({
    Model: MODEL,
    Prompt: prompt,
    Schema: schemaSource,
    Route:
      "deepseek/direct-provider/aliases:deepseek-v4-flash,deepseek-flash/zero-tools/reasoning:low/temperature:0/evidence-only:1",
    MaxTokens: 12000,
  }).replace(
    /[<>&\u2028\u2029]/g,
    (c) => "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"),
  );
  return createHash("sha256").update(json).digest("hex");
}

const fail = () => {
  throw new Error("worker_invalid");
};
export function strictParse(text, limit = 65536, allowNull = false) {
  if (typeof text !== "string" || Buffer.byteLength(text) > limit) fail();
  let i = 0;
  const ws = () => {
    while (/[\x20\t\r\n]/.test(text[i] ?? "x")) i++;
  };
  const str = () => {
    const start = i++;
    while (i < text.length) {
      const c = text[i++];
      if (c === '"') {
        try {
          const parsed = JSON.parse(text.slice(start, i));
          if (!parsed.isWellFormed()) fail();
          return parsed;
        } catch {
          fail();
        }
      }
      if (c === "\\") i++;
    }
    fail();
  };
  const value = (depth) => {
    if (depth > 32) fail();
    ws();
    const c = text[i];
    if (c === '"') return str();
    if (c === "{") {
      i++;
      ws();
      const keys = new Set();
      const o = {};
      if (text[i] === "}") {
        i++;
        return o;
      }
      while (true) {
        ws();
        if (text[i] !== '"') fail();
        const k = str();
        if (keys.has(k)) fail();
        keys.add(k);
        ws();
        if (text[i++] !== ":") fail();
        Object.defineProperty(o, k, {
          value: value(depth + 1),
          enumerable: true,
          writable: true,
          configurable: true,
        });
        ws();
        const end = text[i++];
        if (end === "}") return o;
        if (end !== ",") fail();
      }
    }
    if (c === "[") {
      i++;
      ws();
      const a = [];
      if (text[i] === "]") {
        i++;
        return a;
      }
      while (true) {
        a.push(value(depth + 1));
        ws();
        const end = text[i++];
        if (end === "]") return a;
        if (end !== ",") fail();
      }
    }
    const m =
      /^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/.exec(
        text.slice(i),
      );
    if (!m) fail();
    i += m[0].length;
    const v = JSON.parse(m[0]);
    if (
      (v === null && !allowNull) ||
      (typeof v === "number" && !Number.isFinite(v))
    )
      fail();
    return v;
  };
  const out = value(0);
  ws();
  if (i !== text.length) fail();
  return out;
}
export function validateSchema(v, s) {
  const invalid = (code) => {
    const error = new Error(code);
    error.code = code;
    throw error;
  };
  if (s.type === "object") {
    if (v === null || typeof v !== "object" || Array.isArray(v))
      invalid("worker_schema_type");
    for (const k of s.required ?? [])
      if (!Object.hasOwn(v, k)) invalid("worker_schema_required");
    for (const k of Object.keys(v)) {
      if (!s.properties[k]) {
        const known = {
          requires_review: "worker_schema_review_flag",
          confidence: "worker_schema_confidence",
          unit: "worker_schema_unit_field",
          currency: "worker_schema_currency_field",
          value: "worker_schema_value_field",
          evidence: "worker_schema_evidence_field",
        };
        invalid(known[k] ?? "worker_schema_unknown");
      }
      validateSchema(v[k], s.properties[k]);
    }
  } else if (s.type === "array") {
    if (!Array.isArray(v)) invalid("worker_schema_type");
    if (v.length > (s.maxItems ?? 128)) invalid("worker_schema_limit");
    for (const x of v) validateSchema(x, s.items);
  } else if (s.type === "string") {
    if (typeof v !== "string") invalid("worker_schema_type");
    if ([...v].length > (s.maxLength ?? 8192)) invalid("worker_schema_limit");
  } else if (s.type === "integer") {
    if (!Number.isSafeInteger(v) || v < (s.minimum ?? 0))
      invalid("worker_schema_type");
  } else if (s.type === "boolean") {
    if (typeof v !== "boolean") invalid("worker_schema_type");
  } else invalid("worker_schema_type");
  if (s.enum && !s.enum.includes(v)) invalid("worker_schema_enum");
}
export function validateRoute(r) {
  if (
    r?.provider !== "deepseek" ||
    r.model !== "deepseek-v4-flash" ||
    !["deepseek-flash", "deepseek-v4-flash"].includes(r.responseModel) ||
    r.stopReason !== "stop" ||
    r.execution?.mode !== "direct-provider" ||
    r.execution.owner?.kind !== "provider" ||
    r.execution.owner?.id !== "deepseek"
  )
    fail();
}
const exactKeys = (o, keys) => {
  if (
    !o ||
    typeof o !== "object" ||
    Array.isArray(o) ||
    Object.keys(o).length !== keys.length ||
    keys.some((k) => !Object.hasOwn(o, k))
  )
    fail();
};
export function validateRequest(r) {
  exactKeys(r, [
    "protocol_version",
    "invocation_id",
    "input_digest",
    "schema_version",
    "extraction_version",
    "chunk_version",
    "parse_digest",
    "index",
    "owned",
    "context",
  ]);
  if (
    r.protocol_version !== 1 ||
    r.schema_version !== "archive-extraction/1" ||
    r.extraction_version !== "archive-understand/1" ||
    r.chunk_version !== "archive-chunk/1" ||
    typeof r.invocation_id !== "string" ||
    r.invocation_id.length > 160 ||
    !r.invocation_id ||
    !Number.isSafeInteger(r.index) ||
    r.index < 0 ||
    r.index >= 256 ||
    !/^[a-f0-9]{64}$/.test(r.parse_digest) ||
    !/^[a-f0-9]{64}$/.test(r.input_digest)
  )
    fail();
  const forDigest = { ...r, input_digest: "" };
  const canonical = JSON.stringify(forDigest).replace(
    /[<>&\u2028\u2029]/g,
    (c) => "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"),
  );
  if (createHash("sha256").update(canonical).digest("hex") !== r.input_digest)
    fail();
  if (
    !Array.isArray(r.owned) ||
    !Array.isArray(r.context) ||
    !r.owned.length ||
    r.owned.length + r.context.length > 128 ||
    Buffer.byteLength(JSON.stringify([...r.owned, ...r.context])) > 24576
  )
    fail();
  const seen = new Set();
  for (const v of [...r.owned, ...r.context]) {
    exactKeys(v, [
      "quote_hints",
      "view_id",
      "parent_segment_id",
      "alternative_id",
      "component_id",
      "component_kind",
      "origin",
      "start",
      "end",
      "text",
      "parse_digest",
      "parser_version",
      "requires_review",
      "visual_state",
      "structure",
      "derived",
    ]);
    for (const k of [
      "view_id",
      "parent_segment_id",
      "alternative_id",
      "component_id",
      "origin",
      "visual_state",
    ])
      if (typeof v[k] !== "string" || v[k].length > 512) fail();
    if (!Array.isArray(v.quote_hints) || v.quote_hints.length > 128) fail();
    for (const h of v.quote_hints) {
      exactKeys(h, ["start", "end", "text"]);
      if (
        !Number.isSafeInteger(h.start) ||
        !Number.isSafeInteger(h.end) ||
        h.start < v.start ||
        h.end > v.end ||
        h.end < h.start ||
        [...v.text].slice(h.start - v.start, h.end - v.start).join("") !==
          h.text
      )
        fail();
    }
    if (seen.has(v.view_id)) fail();
    seen.add(v.view_id);
    if (
      v.parse_digest !== r.parse_digest ||
      !/^(?:archive-parse\/1|archive-native\/1(?:\/[a-f0-9]{64})?)$/.test(
        v.parser_version,
      ) ||
      ![
        "body",
        "cell_text",
        "numeric_raw",
        "historical_cache",
        "formula_text",
        "format_code",
        "derived_display",
      ].includes(v.component_kind) ||
      typeof v.requires_review !== "boolean" ||
      typeof v.text !== "string" ||
      !Number.isSafeInteger(v.start) ||
      v.start < 0 ||
      !Number.isSafeInteger(v.end) ||
      v.end - v.start !== [...v.text].length
    )
      fail();
    exactKeys(v.structure, [
      "locator",
      "cell_state",
      "cell_type",
      "value_present",
      "cache_present",
      "row_hidden",
      "column_hidden",
      "column_hidden_unresolved",
      "merge_range",
      "merge_anchor",
      "docx_merge",
    ]);
    for (const k of [
      "value_present",
      "cache_present",
      "row_hidden",
      "column_hidden",
      "column_hidden_unresolved",
    ])
      if (typeof v.structure[k] !== "boolean") fail();
    for (const k of ["cell_state", "cell_type", "merge_range", "merge_anchor"])
      if (typeof v.structure[k] !== "string") fail();
    if (v.derived !== null) {
      exactKeys(v.derived, [
        "ref",
        "original_component_id",
        "format_component_id",
        "parse_digest",
        "parser_version",
        "value",
      ]);
      if (
        v.component_kind !== "derived_display" ||
        v.derived.parse_digest !== r.parse_digest ||
        v.derived.parser_version !== v.parser_version ||
        v.derived.value !== v.text
      )
        fail();
    }
  }
}
async function readInput(signal) {
  const chunks = [];
  let size = 0;
  const abort = () => process.stdin.destroy(new Error("worker_aborted"));
  signal.addEventListener("abort", abort, { once: true });
  try {
    for await (const b of process.stdin) {
      size += b.length;
      if (size > 32768) fail();
      chunks.push(b);
    }
    return new TextDecoder("utf-8", { fatal: true }).decode(
      Buffer.concat(chunks),
    );
  } finally {
    signal.removeEventListener("abort", abort);
  }
}
export async function runArchiveWorker(api) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 110000);
  const abort = () => controller.abort();
  process.once("SIGTERM", abort);
  let emitted = false;
  let stage = "worker_input";
  let outputDone = Promise.resolve();
  const write = (obj) => {
    if (emitted) return outputDone;
    let text = JSON.stringify(obj);
    if (Buffer.byteLength(text) > 65535)
      text = '{"error_code":"worker_output_limit"}';
    emitted = true;
    outputDone = new Promise((resolve) => {
      process.stdout.write(text + "\n", () => resolve());
    });
    return outputDone;
  };
  const fault = () => {
    controller.abort();
    write({ error_code: "worker_failed" });
  };
  process.on("uncaughtException", fault);
  process.on("unhandledRejection", fault);
  try {
    const request = strictParse(
      await readInput(controller.signal),
      32768,
      true,
    );
    validateRequest(request);
    const schemaText = JSON.stringify(schema);
    const completionRequest = {
      model: MODEL,
      systemPrompt: prompt + "\nJSON schema:\n" + schemaText,
      messages: [
        {
          role: "user",
          content: JSON.stringify({
            owned: request.owned,
            context: request.context,
          }),
        },
      ],
      purpose: "atrium.archive-understand.v1",
      maxTokens: 12000,
      reasoning: "low",
      temperature: 0,
    };
    if (Buffer.byteLength(JSON.stringify(completionRequest)) > 32768) fail();
    stage = "worker_completion";
    const r = await api.runtime.llm.complete({
      ...completionRequest,
      signal: controller.signal,
    });
    if (controller.signal.aborted) fail();
    stage = "worker_route";
    validateRoute(r);
    stage = "worker_schema";
    const result = strictParse(r.text, 60000);
    validateSchema(result, schema);
    const usage = {};
    for (const [source, target] of Object.entries({
      inputTokens: "input_tokens",
      outputTokens: "output_tokens",
      totalTokens: "total_tokens",
      cacheReadTokens: "cache_read_tokens",
      cacheWriteTokens: "cache_write_tokens",
      costUsd: "cost_usd",
    })) {
      const value = r.usage?.[source];
      if (value !== undefined) {
        if (!Number.isFinite(value) || value < 0 || value > 1e12) fail();
        usage[target] = value;
      }
    }
    write({
      ...(Object.keys(usage).length ? { usage } : {}),
      configuration_fingerprint: configurationFingerprint(),
      protocol_version: request.protocol_version,
      invocation_id: request.invocation_id,
      input_digest: request.input_digest,
      schema_version: request.schema_version,
      extraction_version: request.extraction_version,
      chunk_version: request.chunk_version,
      provider: r.provider,
      requested_model: MODEL,
      model: r.model,
      response_model: r.responseModel,
      stop_reason: r.stopReason,
      execution: {
        mode: r.execution.mode,
        owner: { kind: r.execution.owner.kind, id: r.execution.owner.id },
      },
      result,
    });
  } catch (error) {
    const code =
      stage === "worker_schema" &&
      [
        "worker_schema_type",
        "worker_schema_required",
        "worker_schema_unknown",
        "worker_schema_review_flag",
        "worker_schema_confidence",
        "worker_schema_unit_field",
        "worker_schema_currency_field",
        "worker_schema_value_field",
        "worker_schema_evidence_field",
        "worker_schema_limit",
        "worker_schema_enum",
      ].includes(error?.code)
        ? error.code
        : stage;
    write({ error_code: code });
  } finally {
    await outputDone;
    clearTimeout(timer);
    process.removeListener("SIGTERM", abort);
    process.removeListener("uncaughtException", fault);
    process.removeListener("unhandledRejection", fault);
  }
}
