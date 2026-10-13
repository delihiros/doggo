export interface ReplSelection {
  start: number;
  end: number;
  text: string;
}

export type ReplPreparationFailureReason = "not-doggo" | "no-selection";

export type ReplPreparationResult =
  | {
      ok: true;
      texts: string[];
    }
  | {
      ok: false;
      reason: ReplPreparationFailureReason;
    };

export function prepareSelectionsForRepl(
  languageId: string,
  selections: readonly ReplSelection[]
): ReplPreparationResult {
  if (languageId !== "doggo") {
    return { ok: false, reason: "not-doggo" };
  }

  const texts = selections
    .filter((selection) => selection.text !== "")
    .sort((a, b) => {
      if (a.start !== b.start) {
        return a.start - b.start;
      }
      return a.end - b.end;
    })
    .map((selection) => selection.text);

  if (texts.length === 0) {
    return { ok: false, reason: "no-selection" };
  }

  return { ok: true, texts };
}
