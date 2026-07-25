import React, { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { createMermaidDiagram } from "@tldraw/mermaid";
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

type LoadState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | {
    status: "ready";
    record: IllustrationResponse;
    store: TLStore;
    licenseKey: string;
  };

const params = new URLSearchParams(window.location.search);
const notePath = params.get("path") || "";
const preview = params.get("mode") === "preview";

function IllustrationApp(): React.JSX.Element {
  const [loadState, setLoadState] = useState<LoadState>({ status: "loading" });
  const [saveStatus, setSaveStatus] = useState("Loading…");
  const revision = useRef("");
  const saving = useRef(false);
  const saveQueued = useRef(false);
  const editorRef = useRef<Editor | null>(null);

  useEffect(() => {
    let active = true;
    void Promise.all([readIllustration(notePath), readIllustrationConfig()])
      .then(([record, config]) => {
        if (!active) return;
        const store = createTLStore();
        if (record.document?.snapshot) {
          loadSnapshot(store, record.document.snapshot as TLEditorSnapshot);
        }
        revision.current = record.revision || "";
        setLoadState({ status: "ready", record, store, licenseKey: config.licenseKey });
      })
      .catch((error: Error) => {
        if (active) setLoadState({ status: "error", message: error.message });
      });
    return () => {
      active = false;
    };
  }, []);

  async function persist(): Promise<void> {
    const editor = editorRef.current;
    if (!editor) return;
    if (saving.current) {
      saveQueued.current = true;
      return;
    }
    saving.current = true;
    setSaveStatus("Saving…");
    try {
      const result = await writeIllustration(notePath, {
        version: 1,
        snapshot: getSnapshot(editor.store),
      }, revision.current);
      revision.current = result.revision || "";
      setSaveStatus("Saved");
    } catch (error) {
      setSaveStatus((error as Error).message.includes("changed since")
        ? "Changed elsewhere — close and reopen"
        : `Save failed: ${(error as Error).message}`);
    } finally {
      saving.current = false;
      if (saveQueued.current) {
        saveQueued.current = false;
        void persist();
      }
    }
  }

  async function handleMount(editor: Editor, record: IllustrationResponse): Promise<() => void> {
    editorRef.current = editor;

    if (!record.document?.snapshot && record.document?.mermaid) {
      setSaveStatus("Building Mermaid diagram…");
      try {
        await createMermaidDiagram(editor, record.document.mermaid);
        editor.selectNone();
        editor.zoomToFit({ immediate: true });
        await persist();
      } catch (error) {
        setSaveStatus(`Mermaid error: ${(error as Error).message}`);
      }
    } else {
      editor.zoomToFit({ immediate: true });
      setSaveStatus(record.exists ? "Saved" : "Start drawing");
    }

    if (preview) {
      editor.selectNone();
      editor.updateInstanceState({ isReadonly: true });
    }

    let timer: number | undefined;
    const stop = preview
      ? () => undefined
      : editor.store.listen(() => {
        window.clearTimeout(timer);
        timer = window.setTimeout(() => void persist(), 650);
      }, { source: "user", scope: "document" });

    return () => {
      window.clearTimeout(timer);
      stop();
      editorRef.current = null;
    };
  }

  if (!notePath) {
    return <div className="drawing-message">Missing note path.</div>;
  }
  if (loadState.status === "loading") {
    return <div className="drawing-message">Loading illustration…</div>;
  }
  if (loadState.status === "error") {
    return <div className="drawing-message drawing-error">{loadState.message}</div>;
  }

  return (
    <div className={`drawing-root ${preview ? "drawing-preview" : ""}`}>
      <Tldraw
        store={loadState.store}
        hideUi={preview}
        licenseKey={loadState.licenseKey || undefined}
        onMount={(editor) => {
          void handleMount(editor, loadState.record);
        }}
      />
      {!preview && <div className="drawing-save-status" role="status">{saveStatus}</div>}
    </div>
  );
}

const root = document.getElementById("drawing-root");
if (!root) throw new Error("Missing drawing root");
createRoot(root).render(<IllustrationApp />);
