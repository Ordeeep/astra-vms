// Only preparations overlap serially. Established streams and initial mosaic
// connections do not occupy this queue.
export function createLiveTransitionQueue() {
  let busy = false;
  const waiting: (() => void)[] = [];
  return {
    acquire(signal: AbortSignal): Promise<() => void> {
      return new Promise((resolve, reject) => {
        let released = false;
        const cancel = () => {
          const index = waiting.indexOf(start);
          if (index >= 0) waiting.splice(index, 1);
          reject(new DOMException("Encerrado", "AbortError"));
        };
        const start = () => {
          signal.removeEventListener("abort", cancel);
          if (signal.aborted) { cancel(); return; }
          busy = true;
          resolve(() => {
            if (released) return;
            released = true;
            busy = false;
            waiting.shift()?.();
          });
        };
        if (signal.aborted) { cancel(); return; }
        signal.addEventListener("abort", cancel, { once: true });
        if (busy) waiting.push(start); else start();
      });
    },
  };
}

export type LiveTransitionQueue = ReturnType<typeof createLiveTransitionQueue>;
