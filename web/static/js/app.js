// Mobile nav toggle. The nav itself stays plain markup (a <nav> full of
// links) at every width; only visibility is different below the
// max-width:620px breakpoint, where CSS hides it behind this hamburger
// button instead of letting it overflow the header (confirmed: at
// 375px wide the uncollapsed nav overflowed the viewport by 100px+,
// pushing "Log out" off-screen with no way to reach it at all).
(function () {
  const toggle = document.querySelector("[data-nav-toggle]");
  const nav = document.querySelector("[data-nav]");
  if (!toggle || !nav) return;

  function setOpen(open) {
    nav.classList.toggle("open", open);
    toggle.setAttribute("aria-expanded", open ? "true" : "false");
  }

  toggle.addEventListener("click", () => setOpen(!nav.classList.contains("open")));
  document.addEventListener("click", (e) => {
    // toggle.contains(e.target), not e.target !== toggle: the button's
    // visible bars are child <span> elements, so a real click on the
    // hamburger icon has e.target set to one of those spans, not the
    // <button> itself. Comparing target directly against toggle missed
    // that, so a click on the icon opened the nav via the listener
    // above and then immediately closed it again here in the same
    // bubbling click event -- the menu never appeared to open at all.
    if (nav.classList.contains("open") && !nav.contains(e.target) && !toggle.contains(e.target)) {
      setOpen(false);
    }
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") setOpen(false);
  });
})();

// Node page tabs: a purely client-side grouping of already-independent
// form sections (each [data-tab-panel] wraps one or more complete,
// unmodified forms) into tabs, so switching tabs can never affect what
// a form submits. The active tab is remembered per node in
// localStorage, since every form on this page is a traditional POST +
// full-page redirect, not AJAX -- without this, saving anything on a
// tab other than the first would silently bounce the operator back to
// tab one. Progressive enhancement: every panel is plain visible markup
// with no hiding CSS of its own, so with JS disabled (or before this
// runs) the page is exactly the old single long page, nothing missing.
(function () {
  const panels = document.querySelectorAll("[data-tab-panel]");
  const tabsBar = document.querySelector(".tabs[data-node-number]");
  const buttons = document.querySelectorAll("[data-tab-target]");
  if (!panels.length || !buttons.length) return;

  const storageKey = "hamvoip-node-tab-" + (tabsBar ? tabsBar.getAttribute("data-node-number") : "");

  function activate(tab) {
    let matched = false;
    panels.forEach((p) => {
      const isMatch = p.getAttribute("data-tab-panel") === tab;
      p.hidden = !isMatch;
      if (isMatch) matched = true;
    });
    if (!matched) {
      // Unknown/stale stored tab id (e.g. from an older version of this
      // page) -- fall back to the first tab rather than hiding
      // everything.
      panels.forEach((p, i) => (p.hidden = i !== 0));
      tab = panels[0].getAttribute("data-tab-panel");
    }
    buttons.forEach((b) => b.classList.toggle("active", b.getAttribute("data-tab-target") === tab));
    try {
      localStorage.setItem(storageKey, tab);
    } catch (e) {
      // Private browsing / storage disabled -- tab switching still
      // works for this page view, it just won't be remembered.
    }
  }

  buttons.forEach((b) => {
    b.addEventListener("click", () => activate(b.getAttribute("data-tab-target")));
  });

  let initial = null;
  try {
    initial = localStorage.getItem(storageKey);
  } catch (e) {}
  activate(initial || buttons[0].getAttribute("data-tab-target"));
})();

// Confirmation modal, replacing native confirm(). confirmModal(message,
// opts) returns a Promise<boolean> resolving true if the operator
// confirmed. opts.danger styles the confirm button like a destructive
// action instead of the default accent color. Built lazily (once, on
// first use) and reused for every subsequent call, rather than one
// element per confirm site, since only one confirmation is ever open at
// a time. The message is set via textContent, never innerHTML, so it
// can't be misread as allowing markup injection even though every
// current caller's text is server-rendered, not user input.
const confirmModal = (function () {
  let backdrop, messageEl, cancelBtn, okBtn, resolveFn;

  function build() {
    backdrop = document.createElement("div");
    backdrop.className = "modal-backdrop";
    backdrop.hidden = true;

    const card = document.createElement("div");
    card.className = "modal-card";
    card.setAttribute("role", "alertdialog");
    card.setAttribute("aria-modal", "true");

    messageEl = document.createElement("p");
    card.appendChild(messageEl);

    const actions = document.createElement("div");
    actions.className = "modal-actions";
    cancelBtn = document.createElement("button");
    cancelBtn.type = "button";
    cancelBtn.className = "btn";
    cancelBtn.textContent = "Cancel";
    okBtn = document.createElement("button");
    okBtn.type = "button";
    actions.appendChild(cancelBtn);
    actions.appendChild(okBtn);
    card.appendChild(actions);
    backdrop.appendChild(card);
    document.body.appendChild(backdrop);

    backdrop.addEventListener("click", (e) => {
      if (e.target === backdrop) settle(false);
    });
    cancelBtn.addEventListener("click", () => settle(false));
    okBtn.addEventListener("click", () => settle(true));
    document.addEventListener("keydown", (e) => {
      if (backdrop.hidden) return;
      if (e.key === "Escape") settle(false);
      else if (e.key === "Enter") settle(true);
    });
  }

  function settle(result) {
    if (!resolveFn) return;
    backdrop.hidden = true;
    const resolve = resolveFn;
    resolveFn = null;
    resolve(result);
  }

  return function confirmModal(message, opts) {
    if (!backdrop) build();
    opts = opts || {};
    messageEl.textContent = message;
    okBtn.className = "btn " + (opts.danger ? "danger" : "primary");
    okBtn.textContent = opts.okLabel || (opts.danger ? "Delete" : "Confirm");
    backdrop.hidden = false;
    okBtn.focus();
    return new Promise((resolve) => {
      resolveFn = resolve;
    });
  };
})();

// Generic confirm-before-submit for any form or submit button carrying
// data-confirm, replacing inline onsubmit/onclick="return confirm(...)"
// so every confirmation looks like the rest of the app rather than the
// browser's own dialog chrome. data-confirm-danger marks the action as
// destructive, styling the modal's confirm button to match.
//
// A form is re-submitted via requestSubmit() after confirmation, which
// re-fires the "submit" event — approvedForms tracks which submission
// was already confirmed so it's let through exactly once instead of
// looping back into this same handler. A button instead calls
// button.form.requestSubmit(button), which respects that button's own
// formaction/form attributes (e.g. a delete button pointing at a
// different form, or overriding the enclosing form's action) — the
// same thing a real click on it would have done.
(function () {
  const approvedForms = new WeakSet();

  document.querySelectorAll("form[data-confirm]").forEach((form) => {
    if (form.hasAttribute("data-ajax-link")) return; // handled inline where it's submitted, see below
    form.addEventListener("submit", (e) => {
      if (approvedForms.has(form)) {
        approvedForms.delete(form);
        return;
      }
      e.preventDefault();
      confirmModal(form.getAttribute("data-confirm"), {
        danger: form.hasAttribute("data-confirm-danger"),
      }).then((ok) => {
        if (!ok) return;
        approvedForms.add(form);
        form.requestSubmit();
      });
    });
  });

  document.querySelectorAll("button[data-confirm]").forEach((btn) => {
    btn.addEventListener("click", (e) => {
      e.preventDefault();
      confirmModal(btn.getAttribute("data-confirm"), {
        danger: btn.hasAttribute("data-confirm-danger"),
      }).then((ok) => {
        if (ok && btn.form) btn.form.requestSubmit(btn);
      });
    });
  });
})();

// Polls /api/status and updates the dashboard status pill + stat grid.
// Plain short-polling rather than SSE/WebSocket: for a handful of scalar
// values refreshed every few seconds this is simpler and just as
// "realtime" as it needs to be. A push-based stream (WebSocket) is worth
// adding later for high-frequency data like live PTT/COS or log tailing.
(function () {
  const pill = document.querySelector("[data-status-pill]");
  const uptimeEl = document.querySelector("[data-uptime]");
  const hostnameEl = document.querySelector("[data-hostname]");
  if (!pill) return;

  async function poll() {
    try {
      const res = await fetch("/api/status", { credentials: "same-origin" });
      if (!res.ok) throw new Error("status " + res.status);
      const s = await res.json();

      pill.classList.toggle("up", s.asterisk_running);
      pill.classList.toggle("down", !s.asterisk_running);
      pill.querySelector(".label").textContent = s.asterisk_running
        ? "Asterisk running"
        : "Asterisk stopped";

      if (uptimeEl) uptimeEl.textContent = s.uptime || "—";
      if (hostnameEl) hostnameEl.textContent = s.hostname || "—";
    } catch (e) {
      pill.classList.remove("up");
      pill.classList.add("down");
      pill.querySelector(".label").textContent = "Status unavailable";
    }
  }

  poll();
  setInterval(poll, 4000);
})();

// Live node data: subscribes to a per-node Server-Sent Events stream and
// updates whichever pieces are present on the current page — Home's
// on-air pill and connected-node chips (with "talking" markers), and/or
// Stats's "Signal on input" cell and connection-history tables — as
// state changes, no page reload. Each element is looked up with
// querySelector and simply skipped if the page doesn't have it, so the
// same script serves both pages without knowing which one it's on.
// Progressive enhancement: every card is already rendered server-side,
// so if EventSource is unavailable or a proxy blocks the stream, the
// static snapshot simply stays put. DOM is built with
// textContent/createElement so callsign/description text from the node
// directory can never inject markup.
(function () {
  const cards = document.querySelectorAll("[data-live-node]");
  if (!cards.length || typeof EventSource === "undefined") return;

  function renderPill(container, receiving) {
    const pill = document.createElement("span");
    pill.className = "status-pill " + (receiving ? "down" : "up");
    const dot = document.createElement("span");
    dot.className = "dot";
    pill.appendChild(dot);
    pill.appendChild(
      document.createTextNode(
        receiving ? "On the air — signal on input" : "Idle — no signal on input"
      )
    );
    container.replaceChildren(pill);
  }

  function renderConnected(container, connected) {
    if (!connected || !connected.length) {
      const hint = document.createElement("div");
      hint.className = "hint";
      hint.textContent = "Nothing connected.";
      container.replaceChildren(hint);
      return;
    }
    const frag = document.createDocumentFragment();
    connected.forEach((n) => {
      const chip = document.createElement("span");
      chip.className = "node-chip" + (n.keyed ? " keyed" : "");
      if (n.detail) chip.title = n.detail;

      const tag = document.createElement("span");
      tag.className = "tag";
      tag.textContent = n.number;
      chip.appendChild(tag);

      if (n.callsign) {
        const call = document.createElement("span");
        call.className = "node-call";
        call.textContent = n.callsign;
        chip.appendChild(call);
      }
      if (n.keyed) {
        const badge = document.createElement("span");
        badge.className = "talking-badge";
        badge.textContent = "talking";
        chip.appendChild(badge);
      }
      frag.appendChild(chip);
      frag.appendChild(document.createTextNode(" "));
    });
    container.replaceChildren(frag);
  }

  cards.forEach((card) => {
    const node = card.getAttribute("data-live-node");
    const pillBox = card.querySelector("[data-live-pill]");
    const connBox = card.querySelector("[data-live-connected]");
    const signalCell = card.querySelector("[data-live-signal]");
    const indicator = card.querySelector("[data-live-indicator]");

    // Scoped by node number, not just presence: with more than one node
    // configured, an unscoped querySelector would find whichever history
    // box happens to be first in the document and every node's stream
    // would write into it instead of its own.
    const historyBox = document.querySelector(
      '[data-live-history="' + CSS.escape(node) + '"]'
    );

    const es = new EventSource("/nodes/" + encodeURIComponent(node) + "/live", {
      withCredentials: true,
    });

    // "live" event: the right-now card (pill, connected chips, signal cell).
    es.addEventListener("live", (ev) => {
      let s;
      try {
        s = JSON.parse(ev.data);
      } catch (e) {
        return;
      }
      if (indicator) indicator.classList.add("on");
      if (pillBox) renderPill(pillBox, s.receiving);
      if (connBox) renderConnected(connBox, s.connected);
      if (signalCell && s.signalOnInput) signalCell.textContent = s.signalOnInput;
    });

    // "history" event: the whole history card, re-rendered server-side and
    // swapped in, so the connection-history tables update without a reload
    // and without duplicating their markup here. The payload is a
    // JSON-encoded HTML string produced by the same template as page load.
    es.addEventListener("history", (ev) => {
      if (!historyBox) return;
      let html;
      try {
        html = JSON.parse(ev.data);
      } catch (e) {
        return;
      }
      if (typeof html === "string" && html.length) historyBox.innerHTML = html;
    });

    es.onerror = () => {
      // EventSource retries on its own; just dim the live indicator while
      // the connection is down so the card doesn't look falsely live.
      if (indicator) indicator.classList.remove("on");
    };
  });
})();

// Link/unlink without a full-page reload: submit the quick-connect form
// in the background and show the result inline. The live stream above
// then reflects the connection appearing or dropping on its own. Falls
// back to a normal form POST if fetch is unavailable.
(function () {
  const forms = document.querySelectorAll("form[data-ajax-link]");
  if (!forms.length || typeof fetch === "undefined") return;

  forms.forEach((form) => {
    form.addEventListener("submit", async (e) => {
      e.preventDefault();

      const confirmMsg = form.getAttribute("data-confirm");
      if (confirmMsg && !(await confirmModal(confirmMsg, { danger: form.hasAttribute("data-confirm-danger") }))) return;

      const result = form.querySelector("[data-link-result]");
      const params = new URLSearchParams(new FormData(form));
      // FormData omits the submit button; carry which one was clicked.
      if (e.submitter && e.submitter.name) {
        params.set(e.submitter.name, e.submitter.value);
      }

      const buttons = form.querySelectorAll("button");
      buttons.forEach((b) => (b.disabled = true));

      function show(ok, msg) {
        if (!result) return;
        result.hidden = false;
        result.textContent = msg;
        result.classList.toggle("ok", ok);
        result.classList.toggle("error", !ok);
      }

      // getAttribute, not form.action: the submit buttons are named
      // "action", which DOM-clobbers the form's .action URL property into
      // returning the button collection instead of the endpoint.
      const endpoint = form.getAttribute("action");

      try {
        const res = await fetch(endpoint, {
          method: "POST",
          body: params,
          headers: { Accept: "application/json" },
          credentials: "same-origin",
        });
        const j = await res.json();
        show(!!j.ok, j.message || (j.ok ? "Done" : "Failed"));
      } catch (err) {
        show(false, "Request failed — check the connection and try again.");
      } finally {
        buttons.forEach((b) => (b.disabled = false));
      }
    });
  });
})();

// Node page "radio hardware" toggle: shows either the existing-device
// picker or the create-a-new-device sub-form depending on which radio
// button is selected, so both stay in the same form (one POST covers
// node + optional new device) without cluttering the page with an
// always-visible device-creation form most edits don't need.
(function () {
  const radios = document.querySelectorAll("[data-radio-mode]");
  if (!radios.length) return;
  const sections = {
    existing: document.querySelector('[data-radio-mode-section="existing"]'),
    new: document.querySelector('[data-radio-mode-section="new"]'),
  };
  function apply() {
    const checked = document.querySelector("[data-radio-mode]:checked");
    const mode = checked ? checked.value : "existing";
    for (const key in sections) {
      if (sections[key]) sections[key].style.display = key === mode ? "" : "none";
    }
  }
  radios.forEach((r) => r.addEventListener("change", apply));
  apply();
})();

// Generalized version of the toggle above for a page with more than one
// independent radio group (e.g. the WX courtesy tone form's separate
// Normal/WX "tone" vs "sound file" choices) -- each radio's own name
// attribute is the group key, so any number of groups can share this one
// block instead of duplicating the IIFE per group. A section is shown
// when data-type-toggle-section="<radio name>:<radio value>" matches the
// checked radio in that name's group.
(function () {
  const radios = document.querySelectorAll("[data-type-toggle]");
  if (!radios.length) return;
  const groups = new Set();
  radios.forEach((r) => groups.add(r.name));
  function apply() {
    groups.forEach((name) => {
      const checked = document.querySelector(`[data-type-toggle][name="${name}"]:checked`);
      const value = checked ? checked.value : "";
      document.querySelectorAll(`[data-type-toggle-section^="${name}:"]`).forEach((section) => {
        const sectionValue = section.getAttribute("data-type-toggle-section").split(":")[1];
        section.style.display = sectionValue === value ? "" : "none";
      });
    });
  }
  radios.forEach((r) => r.addEventListener("change", apply));
  apply();
})();

// Connections page "quick action" buttons: fills the DTMF sequence
// field with <prefix><target node>, so the operator can review it
// before sending rather than the click sending anything directly.
(function () {
  const target = document.querySelector("[data-target-node]");
  const digitsField = document.querySelector("[data-dtmf-field]");
  if (!digitsField) return;
  document.querySelectorAll("[data-fill-digits]").forEach((btn) => {
    btn.addEventListener("click", () => {
      const prefix = btn.getAttribute("data-fill-digits");
      const node = target ? target.value.trim() : "";
      digitsField.value = prefix + node;
      digitsField.focus();
    });
  });
})();

// "Play" buttons next to each Custom sound files row. One shared Audio
// element for the whole page (not one per row) so starting a second
// clip always stops whichever one was already playing, and clicking the
// same row's button again toggles it off rather than restarting it.
(function () {
  const buttons = document.querySelectorAll("[data-play-sound]");
  if (!buttons.length) return;
  const audio = new Audio();
  let activeBtn = null;

  function stop() {
    audio.pause();
    audio.currentTime = 0;
    if (activeBtn) activeBtn.textContent = "Play";
    activeBtn = null;
  }
  audio.addEventListener("ended", stop);

  buttons.forEach((btn) => {
    btn.addEventListener("click", () => {
      const wasActive = activeBtn === btn;
      stop();
      if (wasActive) return; // this click was the toggle-off
      audio.src = btn.getAttribute("data-play-sound");
      audio.play();
      btn.textContent = "Stop";
      activeBtn = btn;
    });
  });
})();

// "Preview" button on the "Create from text" card: synthesizes speech
// for whatever voice/text is currently filled in and plays it
// immediately, via fetch rather than a normal form submission — a full
// page reload just to hear a few seconds of audio (and losing whatever
// else was mid-edit on the page) would be a bad way to let someone try
// a few wordings/voices before committing to "Generate & save".
(function () {
  const btn = document.querySelector("[data-tts-preview]");
  if (!btn) return;
  const voiceField = document.getElementById("tts_voice");
  const engineField = document.getElementById("tts_engine");
  const textField = document.getElementById("tts_text");
  const status = document.querySelector("[data-tts-preview-status]");
  const audio = new Audio();

  btn.addEventListener("click", async () => {
    const text = textField.value.trim();
    if (!text) {
      textField.focus();
      return;
    }
    audio.pause();
    btn.disabled = true;
    const originalLabel = btn.textContent;
    btn.textContent = "Generating…";
    if (status) status.textContent = "";
    try {
      const body = new URLSearchParams({
        tts_voice: voiceField.value,
        tts_text: text,
        tts_engine: engineField ? engineField.value : "",
      });
      const resp = await fetch(btn.getAttribute("data-tts-preview"), { method: "POST", body });
      if (!resp.ok) {
        if (status) status.textContent = await resp.text();
        return;
      }
      const blob = await resp.blob();
      audio.src = URL.createObjectURL(blob);
      audio.play();
    } catch (err) {
      if (status) status.textContent = "Couldn't reach the server to generate a preview.";
    } finally {
      btn.disabled = false;
      btn.textContent = originalLabel;
    }
  });
})();
