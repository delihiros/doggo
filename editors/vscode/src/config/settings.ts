export type TraceSetting = "off" | "messages" | "verbose";

export interface DoggoSettings {
  lspCommand: string;
  lspArgs: string[];
  lspEnv: Record<string, string>;
  traceServer: TraceSetting;
  indentTrace: boolean;
}

export interface DoggoSettingsInput {
  lspCommand?: unknown;
  lspArgs?: unknown;
  lspEnv?: unknown;
  traceServer?: unknown;
  indentTrace?: unknown;
}

export interface DoggoWorkspaceConfiguration {
  get<T>(section: string, defaultValue: T): T;
}

const VALID_TRACE_SETTINGS: ReadonlySet<TraceSetting> = new Set(["off", "messages", "verbose"]);

export function readDoggoSettings(config: DoggoWorkspaceConfiguration): DoggoSettings {
  return parseDoggoSettings({
    lspCommand: config.get("lsp.command", ""),
    lspArgs: config.get("lsp.args", []),
    lspEnv: config.get("lsp.env", {}),
    traceServer: config.get("trace.server", "off"),
    indentTrace: config.get("debug.indentTrace", false)
  });
}

export function parseDoggoSettings(raw: DoggoSettingsInput): DoggoSettings {
  const traceServer = normalizeTraceSetting(raw.traceServer);
  return {
    lspCommand: normalizeString(raw.lspCommand),
    lspArgs: normalizeArgs(raw.lspArgs),
    lspEnv: normalizeEnv(raw.lspEnv),
    traceServer,
    indentTrace: typeof raw.indentTrace === "boolean" ? raw.indentTrace : false
  };
}

function normalizeString(value: unknown): string {
  if (typeof value !== "string") {
    return "";
  }
  return value.trim();
}

function normalizeArgs(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return [];
  }
  const out: string[] = [];
  for (const item of value) {
    if (typeof item !== "string") {
      continue;
    }
    const trimmed = item.trim();
    if (trimmed === "") {
      continue;
    }
    out.push(trimmed);
  }
  return out;
}

function normalizeEnv(value: unknown): Record<string, string> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    return {};
  }
  const out: Record<string, string> = {};
  for (const [key, raw] of Object.entries(value)) {
    const normalizedKey = key.trim();
    if (normalizedKey === "") {
      continue;
    }
    out[normalizedKey] = String(raw);
  }
  return out;
}

function normalizeTraceSetting(value: unknown): TraceSetting {
  if (typeof value !== "string") {
    return "off";
  }
  if (VALID_TRACE_SETTINGS.has(value as TraceSetting)) {
    return value as TraceSetting;
  }
  return "off";
}
