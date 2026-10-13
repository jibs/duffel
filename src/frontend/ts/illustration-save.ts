declare global {
  interface Window {
    duffelIllustration?: { flush: () => Promise<void> };
  }
}

// One writer drains all changes, including edits made while a request is in flight.
// Failed writes stay dirty; callers can retry without pretending the draft was saved.
export class IllustrationSaveQueue {
  private changed = 0;
  private saved = 0;
  private running: Promise<void> | null = null;

  constructor(private readonly write: () => Promise<void>) {}

  markDirty(): void { this.changed++; }
  get dirty(): boolean { return this.changed !== this.saved; }

  flush(): Promise<void> {
    if (this.running) return this.running;
    this.running = this.drain().finally(() => { this.running = null; });
    return this.running;
  }

  private async drain(): Promise<void> {
    while (this.dirty) {
      const writing = this.changed;
      await this.write();
      this.saved = writing;
    }
  }
}
