export interface ManagedClient {
  start(): Promise<void>;
  stop(): Promise<void>;
}

export interface ManagedClientStart {
  startupMessage: string;
  client: ManagedClient;
}

export interface ClientManagerOutput {
  appendLine(message: string): void;
}

export interface LanguageClientManagerOptions {
  createClient: () => ManagedClientStart;
  output: ClientManagerOutput;
  onStartError: (error: unknown) => Promise<void>;
}

export class LanguageClientManager {
  private client: ManagedClient | undefined;
  private startPromise: Promise<void> | undefined;

  private readonly createClient: () => ManagedClientStart;
  private readonly output: ClientManagerOutput;
  private readonly onStartError: (error: unknown) => Promise<void>;

  constructor(options: LanguageClientManagerOptions) {
    this.createClient = options.createClient;
    this.output = options.output;
    this.onStartError = options.onStartError;
  }

  async start(): Promise<void> {
    if (this.startPromise !== undefined) {
      await this.startPromise;
      return;
    }

    const started = this.startInternal();
    this.startPromise = started.finally(() => {
      this.startPromise = undefined;
    });
    await this.startPromise;
  }

  async stop(): Promise<void> {
    if (this.client === undefined) {
      return;
    }

    const runningClient = this.client;
    this.client = undefined;
    await runningClient.stop();
  }

  async restart(): Promise<void> {
    await this.stop();
    await this.start();
  }

  private async startInternal(): Promise<void> {
    const started = this.createClient();
    this.output.appendLine(started.startupMessage);
    this.client = started.client;

    try {
      await started.client.start();
      this.output.appendLine("[doggo] language server started.");
    } catch (error) {
      if (this.client === started.client) {
        this.client = undefined;
      }
      this.output.appendLine(`[doggo] failed to start language server: ${String(error)}`);
      await this.onStartError(error);
    }
  }
}
