export const DOGGO_COMMAND_IDS = [
  "doggo.restartLanguageServer",
  "doggo.showLanguageServerOutput",
  "doggo.runSelectionInRepl",
  "doggo.diagnoseIndentation"
] as const;

export interface DisposableLike {
  dispose(): void;
}

export interface CommandRegistry {
  registerCommand(
    id: string,
    callback: (...args: unknown[]) => unknown
  ): DisposableLike;
}

export interface SubscriptionSink {
  push(disposable: DisposableLike): number;
}

export interface DoggoCommandHandlers {
  restartLanguageServer: () => Promise<void>;
  showLanguageServerOutput: () => void;
  runSelectionInRepl: () => Promise<void>;
  diagnoseIndentation: () => Promise<void>;
}

export function registerDoggoCommands(
  registry: CommandRegistry,
  subscriptions: SubscriptionSink,
  handlers: DoggoCommandHandlers
): void {
  subscriptions.push(
    registry.registerCommand("doggo.restartLanguageServer", async () => {
      await handlers.restartLanguageServer();
    })
  );

  subscriptions.push(
    registry.registerCommand("doggo.showLanguageServerOutput", () => {
      handlers.showLanguageServerOutput();
    })
  );

  subscriptions.push(
    registry.registerCommand("doggo.runSelectionInRepl", async () => {
      await handlers.runSelectionInRepl();
    })
  );

  subscriptions.push(
    registry.registerCommand("doggo.diagnoseIndentation", async () => {
      await handlers.diagnoseIndentation();
    })
  );
}
