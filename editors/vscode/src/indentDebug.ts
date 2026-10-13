export type IndentAction = "none" | "indent" | "indentOutdent" | "outdent";

export interface OnEnterRule {
  beforeText?: string;
  afterText?: string;
  action?: {
    indentAction?: IndentAction | string;
  };
}

export interface LanguageIndentConfiguration {
  indentationRules?: {
    increaseIndentPattern?: string;
    decreaseIndentPattern?: string;
    indentNextLinePattern?: string;
  };
  onEnterRules?: OnEnterRule[];
}

export interface MatchedOnEnterRule {
  index: number;
  indentAction: string;
}

export interface IndentationEvaluation {
  increaseIndent: boolean;
  decreaseIndent: boolean;
  indentNextLine: boolean;
  matchedOnEnter: MatchedOnEnterRule[];
}

export function evaluateIndentationRules(
  config: LanguageIndentConfiguration,
  beforeText: string,
  afterText: string
): IndentationEvaluation {
  const increaseIndentPattern = config.indentationRules?.increaseIndentPattern;
  const decreaseIndentPattern = config.indentationRules?.decreaseIndentPattern;
  const indentNextLinePattern = config.indentationRules?.indentNextLinePattern;

  const result: IndentationEvaluation = {
    increaseIndent: matchesPattern(increaseIndentPattern, beforeText),
    decreaseIndent: matchesPattern(decreaseIndentPattern, beforeText),
    indentNextLine: matchesPattern(indentNextLinePattern, beforeText),
    matchedOnEnter: []
  };

  const onEnterRules = config.onEnterRules ?? [];
  for (let index = 0; index < onEnterRules.length; index += 1) {
    const rule = onEnterRules[index];
    if (!matchesPattern(rule.beforeText, beforeText)) {
      continue;
    }
    if (rule.afterText !== undefined && !matchesPattern(rule.afterText, afterText)) {
      continue;
    }
    const indentAction = String(rule.action?.indentAction ?? "none");
    result.matchedOnEnter.push({ index, indentAction });
  }

  return result;
}

function matchesPattern(pattern: string | undefined, text: string): boolean {
  if (pattern === undefined || pattern === "") {
    return false;
  }
  try {
    return new RegExp(pattern).test(text);
  } catch {
    return false;
  }
}
