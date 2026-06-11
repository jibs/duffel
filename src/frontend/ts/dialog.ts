export async function showAlert(message: string): Promise<void> {
  return new Promise((resolve) => {
    const overlay = makeOverlay();
    const modal = makeModal();

    const msg = document.createElement("p");
    msg.className = "dialog-message";
    msg.textContent = message;

    const btns = document.createElement("div");
    btns.className = "modal-buttons";

    const ok = document.createElement("button");
    ok.className = "primary";
    ok.textContent = "OK";
    const dismiss = () => { overlay.remove(); resolve(); };
    ok.addEventListener("click", dismiss);
    overlay.addEventListener("keydown", (e) => {
      if (e.key === "Escape" || e.key === "Enter") dismiss();
    });

    btns.appendChild(ok);
    modal.appendChild(msg);
    modal.appendChild(btns);
    overlay.appendChild(modal);
    document.body.appendChild(overlay);
    ok.focus();
  });
}

export interface ConfirmOptions {
  confirmLabel?: string;
  danger?: boolean;
}

export async function showConfirm(message: string, opts: ConfirmOptions = {}): Promise<boolean> {
  return new Promise((resolve) => {
    const overlay = makeOverlay();
    const modal = makeModal();

    const msg = document.createElement("p");
    msg.className = "dialog-message";
    msg.textContent = message;

    const btns = document.createElement("div");
    btns.className = "modal-buttons";

    const cancel = document.createElement("button");
    cancel.textContent = "Cancel";
    cancel.addEventListener("click", () => { overlay.remove(); resolve(false); });

    const ok = document.createElement("button");
    ok.className = opts.danger ? "danger" : "primary";
    ok.textContent = opts.confirmLabel ?? (opts.danger ? "Delete" : "Confirm");
    ok.addEventListener("click", () => { overlay.remove(); resolve(true); });

    btns.appendChild(cancel);
    btns.appendChild(ok);
    modal.appendChild(msg);
    modal.appendChild(btns);
    overlay.appendChild(modal);
    document.body.appendChild(overlay);

    // For destructive actions, default focus to Cancel so Enter doesn't accidentally confirm
    (opts.danger ? cancel : ok).focus();

    overlay.addEventListener("keydown", (e) => {
      if (e.key === "Escape") { overlay.remove(); resolve(false); }
    });
    overlay.addEventListener("click", (e) => {
      if (e.target === overlay) { overlay.remove(); resolve(false); }
    });
  });
}

export async function showPrompt(
  message: string,
  opts: { placeholder?: string; defaultValue?: string; submitLabel?: string } = {},
): Promise<string | null> {
  return new Promise((resolve) => {
    const overlay = makeOverlay();
    const modal = makeModal();

    const msg = document.createElement("p");
    msg.className = "dialog-message";
    msg.textContent = message;

    const input = document.createElement("input");
    input.type = "text";
    input.placeholder = opts.placeholder ?? "";
    input.value = opts.defaultValue ?? "";

    const btns = document.createElement("div");
    btns.className = "modal-buttons";

    const cancel = document.createElement("button");
    cancel.textContent = "Cancel";
    cancel.addEventListener("click", () => { overlay.remove(); resolve(null); });

    const ok = document.createElement("button");
    ok.className = "primary";
    ok.textContent = opts.submitLabel ?? "Create";

    const submit = () => {
      const val = input.value.trim();
      if (!val) { input.focus(); return; }
      overlay.remove();
      resolve(val);
    };

    ok.addEventListener("click", submit);
    input.addEventListener("keydown", (e) => {
      if (e.key === "Enter") { e.preventDefault(); submit(); }
      if (e.key === "Escape") { overlay.remove(); resolve(null); }
    });
    overlay.addEventListener("click", (e) => {
      if (e.target === overlay) { overlay.remove(); resolve(null); }
    });

    btns.appendChild(cancel);
    btns.appendChild(ok);
    modal.appendChild(msg);
    modal.appendChild(input);
    modal.appendChild(btns);
    overlay.appendChild(modal);
    document.body.appendChild(overlay);

    requestAnimationFrame(() => input.focus());
  });
}

function makeOverlay(): HTMLDivElement {
  const el = document.createElement("div");
  el.className = "modal-overlay";
  return el;
}

function makeModal(): HTMLDivElement {
  const el = document.createElement("div");
  el.className = "modal dialog-modal";
  return el;
}
