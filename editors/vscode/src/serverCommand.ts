export type CommandSource = "config" | "path";

export interface ResolveDoggoCommandInput {
  subcommand: string;
  configuredCommand?: string;
  configuredArgs?: string[];
  configuredEnv?: Record<string, string>;
}

export interface ResolvedServerCommand {
  command: string;
  args: string[];
  env: NodeJS.ProcessEnv;
  source: CommandSource;
}

export interface ResolveServerCommandInput extends Omit<ResolveDoggoCommandInput, "subcommand"> {}

export function resolveDoggoCommand(input: ResolveDoggoCommandInput): ResolvedServerCommand {
  const subcommand = normalizeString(input.subcommand);
  const configuredCommand = normalizeString(input.configuredCommand);
  const configuredArgs = normalizeArgs(input.configuredArgs);
  const configuredEnv = normalizeEnv(input.configuredEnv);

  const configured = configuredCommand !== "";

  return {
    command: configured ? configuredCommand : "doggo",
    args: configured
      ? [subcommand, ...configuredArgs]
      : ["exec", "--", subcommand, ...configuredArgs],
    env: {
      ...process.env,
      ...configuredEnv
    },
    source: configured ? "config" : "path"
  };
}

export function resolveServerCommand(input: ResolveServerCommandInput): ResolvedServerCommand {
  return resolveDoggoCommand({
    subcommand: "lsp",
    ...input
  });
}

function normalizeString(value: string | undefined): string {
  return (value ?? "").trim();
}

function normalizeArgs(value: string[] | undefined): string[] {
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

function normalizeEnv(value: Record<string, string> | undefined): Record<string, string> {
  if (value === undefined || value === null) {
    return {};
  }
  const out: Record<string, string> = {};
  for (const [key, raw] of Object.entries(value)) {
    const k = key.trim();
    if (k === "") {
      continue;
    }
    out[k] = String(raw);
  }
  return out;
}
