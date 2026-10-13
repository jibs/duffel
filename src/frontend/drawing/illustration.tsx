import React, { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  createTLStore,
  Editor,
  getSnapshot,
  loadSnapshot,
  Tldraw,
  TLEditorSnapshot,
  TLStore,
} from "tldraw";
import "tldraw/tldraw.css";
import "./illustration.css";
import {
  IllustrationResponse,
  readIllustration,
  readIllustrationConfig,
  writeIllustration,
} from "../ts/api.js";
import { IllustrationSaveQueue } from "../ts/illustration-save.js";

type LoadState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "ready"; record: IllustrationResponse; store: TLStore; licenseKey: string };

const params = new URLSearchParams(window.location.search);
const notePath = params.get("path") || "";
const preview = params.get("mode") === "preview";

function IllustrationApp(): React.JSX.Element {
  const [loadState, setLoadState] = useState<LoadState>({ status: "loading" });
  const [saveStatus, setSaveStatus] = useState("Loading…");
  const [initializing, setInitializing] = useState(true);
  const revision = useRef("none");
  const queue = useRef<IllustrationSaveQueue | null>(null);
  const initialization = useRef<Promise<void> | null>(null);
  const timer = useRef<number | undefined>(undefined);
  const conflict = useRef(false);
  const editorRef = useRef<Editor | null>(null);
  const savedDocument = useRef("");

  useEffect(() => {
    let active = true;
    void Promise.all([readIllustration(notePath), readIllustrationConfig()])
      .then(([record, config]) => {
        if (!active) return;
        const store = createTLStore();
        if (record.document?.snapshot) {
          loadSnapshot(store, record.document.snapshot as TLEditorSnapshot);
        }
        revision.current = record.revision || "none";
        setLoadState({ status: "ready", record, store, licenseKey: config.licenseKey });
      })
      .catch((error: Error) => {
        if (active) setLoadState({ status: "error", message: error.message });
      });
    const flush = async (): Promise<void> => {
      if (preview) return;
      if (!initialization.current || !queue.current) throw new Error("Editor is still loading");
      await initialization.current.catch(() => undefined);
      window.clearTimeout(timer.current);
      const editor = editorRef.current;
      editor?.complete();
      editor?.updateInstanceState({ isReadonly: true });
      // Store listeners may be delivered on the next animation frame. Inspect
      // the document itself so an immediate close also includes that last edit.
      if (editor && JSON.stringify(getSnapshot(editor.store).document) !== savedDocument.current) {
        queue.current.markDirty();
      }
      try {
        await queue.current.flush();
      } catch (error) {
        editorRef.current?.updateInstanceState({ isReadonly: preview });
        throw error;
      }
    };
    window.duffelIllustration = { flush };
    const beforeUnload = (event: BeforeUnloadEvent): void => {
      if (!preview && (queue.current?.dirty || (editorRef.current &&
        JSON.stringify(getSnapshot(editorRef.current.store).document) !== savedDocument.current))) {
        event.preventDefault();
        event.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", beforeUnload);
    return () => {
      active = false;
      delete window.duffelIllustration;
      window.removeEventListener("beforeunload", beforeUnload);
    };
  }, []);

  function handleMount(editor: Editor, record: IllustrationResponse): () => void {
    let active = true;
    let initialized = false;
    editorRef.current = editor;
    savedDocument.current = JSON.stringify(getSnapshot(editor.store).document);
    queue.current = new IllustrationSaveQueue(async () => {
      if (preview) throw new Error("Previews are read-only");
      if (conflict.current) throw new Error("Changed elsewhere. Keep this draft open and copy it before reloading.");
      setSaveStatus("Saving…");
      try {
        // Only document state is shared; viewport/selection belongs to this session.
        const document = getSnapshot(editor.store).document;
        const fingerprint = JSON.stringify(document);
        const result = await writeIllustration(notePath, {
          version: 1,
          snapshot: { document },
        }, revision.current);
        revision.current = result.revision || "none";
        savedDocument.current = fingerprint;
        if (active) setSaveStatus("Saved");
      } catch (error) {
        conflict.current = (error as Error).message.includes("changed since");
        if (active) setSaveStatus(`Save failed: ${(error as Error).message}`);
        throw error;
      }
    });
    initialization.current = (async () => {
      if (!record.document?.snapshot && record.document?.mermaid) {
        setSaveStatus("Building Mermaid diagram…");
        const { createMermaidDiagram } = await import("@tldraw/mermaid");
        if (!active) return;
        await createMermaidDiagram(editor, record.document.mermaid);
        if (!active) return;
        if (!preview) {
          queue.current!.markDirty();
          await queue.current!.flush();
        }
      }
      if (!active) return;
      editor.selectNone();
      editor.zoomToFit({ immediate: true });
      editor.updateInstanceState({ isReadonly: preview });
      setSaveStatus(record.exists ? "Saved" : "Start drawing");
      initialized = true;
      setInitializing(false);
    })();
    // Wait for conversion before collecting the draft; failed saves remain dirty.
    void initialization.current.catch((error: Error) => {
      if (active) {
        setSaveStatus(`Diagram error: ${error.message}`);
        editor.updateInstanceState({ isReadonly: preview });
        initialized = true;
        setInitializing(false);
      }
    });

    const stop = preview ? () => undefined : editor.store.listen(() => {
      if (!initialized) return;
      queue.current!.markDirty();
      setSaveStatus("Unsaved changes");
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => {
        void queue.current!.flush().catch(() => undefined);
      }, 650);
    }, { source: "user", scope: "document" });
    const escape = (event: KeyboardEvent): void => {
      if (event.key === "Escape" && window.parent !== window) {
        window.parent.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
      }
    };
    window.addEventListener("keydown", escape);
    return () => {
      active = false;
      window.clearTimeout(timer.current);
      stop();
      editorRef.current = null;
      window.removeEventListener("keydown", escape);
    };
  }

  if (!notePath) return <div className="drawing-message">Missing note path.</div>;
  if (loadState.status === "loading") return <div className="drawing-message">Loading illustration…</div>;
  if (loadState.status === "error") {
    return <div className="drawing-message drawing-error">{loadState.message}</div>;
  }
  return (
    <div className={`drawing-root ${preview ? "drawing-preview" : ""} ${initializing ? "drawing-loading" : ""}`}>
      <Tldraw
        store={loadState.store}
        hideUi={preview || initializing}
        licenseKey={loadState.licenseKey || undefined}
        onMount={(editor) => handleMount(editor, loadState.record)}
      />
      {!preview && <div className="drawing-save-status" role="status">{saveStatus}</div>}
      {preview && saveStatus.startsWith("Diagram error:") &&
        <div className="drawing-save-status drawing-error" role="alert">{saveStatus}</div>}
    </div>
  );
}

const root = document.getElementById("drawing-root");
if (!root) throw new Error("Missing drawing root");
createRoot(root).render(<IllustrationApp />);
