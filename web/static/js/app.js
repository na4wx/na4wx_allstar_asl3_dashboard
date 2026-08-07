// Mobile nav toggle. The nav itself stays plain markup (a <nav> full of
// links) at every width; only visibility is different below the
// max-width:620px breakpoint, where CSS hides it behind this hamburger
// button instead of letting it overflow the header (confirmed: at
// 375px wide the uncollapsed nav overflowed the viewport by 100px+,
// pushing "Log out" off-screen with no way to reach it at all).
//
// Lives in <header>, outside <main>, so it's never touched by a content
// swap (see AppSocket below) -- no re-init needed, ever.
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

// Confirmation modal, replacing native confirm(). confirmModal(message,
// opts) returns a Promise<boolean> resolving true if the operator
// confirmed. opts.danger styles the confirm button like a destructive
// action instead of the default accent color. Built lazily (once, on
// first use) into document.body -- outside <main>, so it survives every
// content swap untouched, same as the nav toggle above. The message is
// set via textContent, never innerHTML, so it can't be misread as
// allowing markup injection even though every current caller's text is
// server-rendered, not user input.
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
// Delegated on document rather than bound per-element, so it keeps
// working on every form/button AppSocket swaps into <main> later without
// needing to be re-run -- one binding for the life of the page, matching
// AppSocket's own delegated link/submit interception below.
//
// A form is re-submitted via requestSubmit() after confirmation, which
// re-fires the "submit" event; approvedForms tracks which submission was
// already confirmed so it's let through exactly once instead of looping
// back into this same handler. AppSocket's own submit interception checks
// e.defaultPrevented before acting, so it correctly skips the first,
// modal-triggering submit here and only intercepts the approved
// resubmission (a fresh, non-prevented event). A button instead calls
// button.form.requestSubmit(button), which respects that button's own
// formaction/form attributes (e.g. a delete button pointing at a
// different form) -- the same thing a real click on it would have done.
(function () {
  const approvedForms = new WeakSet();

  document.addEventListener("submit", (e) => {
    const form = e.target;
    if (!(form instanceof HTMLFormElement) || !form.hasAttribute("data-confirm")) return;
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

  document.addEventListener("click", (e) => {
    const btn = e.target.closest("button[data-confirm]");
    if (!btn) return;
    e.preventDefault();
    confirmModal(btn.getAttribute("data-confirm"), {
      danger: btn.hasAttribute("data-confirm-danger"),
    }).then((ok) => {
      if (ok && btn.form) btn.form.requestSubmit(btn);
    });
  });
})();

// AppSocket: one WebSocket connection per tab that replaces both full
// browser navigation and the old per-node SSE live stream -- see
// internal/server/ws.go's own doc comment for the server half and why
// replaying a request over the socket and swapping the rendered result
// into <main> is equivalent to a real navigation.
//
// Every form keeps its real action and every link keeps its real href.
// Interception below is skipped whenever the socket isn't open (not yet
// connected, or mid-reconnect), which is what makes "the app still works
// with JS/WS unavailable" true by construction rather than a separately
// maintained fallback path.
const AppSocket = (function () {
  let socket = null;
  let everConnected = false;
  let intentionalClose = false;
  let reconnectDelay = 1000;
  let reconnectTimer = null;
  let reconnectAttempts = 0;
  let pendingPush = true;
  let subscribedNodes = new Set();

  // A WebSocket's own JS API can't tell a permanent failure (the
  // session expired -- GET /ws now bounces to /login instead of
  // upgrading, e.g. after the whole process restarted on a reboot, not
  // just Asterisk) apart from a transient one (the server is still
  // coming back up), so this can't just retry forever hoping it
  // eventually works. After this many failed attempts (~1 minute of
  // capped backoff), fall back to a real reload -- if the session is
  // gone that lands on a real login page instead of an infinite
  // "Reconnecting…"; if the server was just slow to come back, the
  // reload finds it and everything resumes normally either way.
  const maxReconnectAttempts = 8;

  const indicator = document.querySelector("[data-ws-status]");

  function setIndicator(state) {
    if (!indicator) return;
    indicator.hidden = state === "connected";
    if (state === "reconnecting") indicator.textContent = "Reconnecting…";
    else if (state === "connecting") indicator.textContent = "Connecting…";
  }

  function isOpen() {
    return !!socket && socket.readyState === WebSocket.OPEN;
  }

  function send(msg) {
    if (!isOpen()) return false;
    socket.send(JSON.stringify(msg));
    return true;
  }

  function connect() {
    setIndicator(everConnected ? "reconnecting" : "connecting");
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    socket = new WebSocket(proto + "//" + location.host + "/ws");

    socket.addEventListener("open", () => {
      reconnectDelay = 1000;
      reconnectAttempts = 0;
      setIndicator("connected");
      const reconnected = everConnected;
      everConnected = true;
      syncLiveSubscriptions(true);
      if (reconnected) {
        // The connection dropped and came back (an Asterisk restart or
        // reboot triggered from the System page, most likely) --
        // refresh whatever's currently on screen instead of leaving it
        // stale with no indication anything changed.
        navigateTo(location.pathname + location.search, { push: false });
      }
    });

    socket.addEventListener("close", () => {
      document.querySelectorAll("[data-live-indicator]").forEach((el) => el.classList.remove("on"));
      if (intentionalClose) return;
      reconnectAttempts++;
      if (reconnectAttempts > maxReconnectAttempts) {
        location.reload();
        return;
      }
      setIndicator("reconnecting");
      reconnectTimer = setTimeout(connect, reconnectDelay);
      reconnectDelay = Math.min(reconnectDelay * 2, 10000);
    });

    socket.addEventListener("error", () => {
      socket.close();
    });

    socket.addEventListener("message", (ev) => {
      let msg;
      try {
        msg = JSON.parse(ev.data);
      } catch (e) {
        return;
      }
      if (msg.type === "page") {
        applyPage(msg.url, msg.html, pendingPush);
      } else if (msg.type === "live" || msg.type === "history") {
        onLive(msg.type, msg.node, msg.data);
      }
    });
  }

  window.addEventListener("beforeunload", () => {
    intentionalClose = true;
    if (reconnectTimer) clearTimeout(reconnectTimer);
    if (socket) socket.close();
  });

  // ---- page navigation/submit replay ----

  function navigateTo(url, opts) {
    opts = opts || {};
    if (!isOpen()) {
      location.href = url;
      return;
    }
    pendingPush = opts.push !== false;
    send({ type: "nav", url: url });
  }

  function submitForm(method, url, body, opts) {
    opts = opts || {};
    if (!isOpen()) return false;
    pendingPush = opts.push !== false;
    send({ type: "submit", method: method, url: url, body: body });
    return true;
  }

  function applyPage(url, html, push) {
    const doc = new DOMParser().parseFromString(html, "text/html");
    const newMain = doc.querySelector("main");
    const liveMain = document.querySelector("main");
    if (!newMain || !liveMain) {
      showRawFallback(html);
      return;
    }
    // A missing url (see ws.go's replay's own doc comment) means the
    // server has no independently GET-able page to report -- e.g. a
    // handler that rendered in place without redirecting. Treat exactly
    // like "same URL as now": keep the address bar and scroll position
    // untouched, just swap the content.
    const currentPath = location.pathname + location.search;
    const sameUrl = !url || url === currentPath;
    const scrollY = sameUrl ? window.scrollY : 0;
    liveMain.innerHTML = newMain.innerHTML;
    if (url && !sameUrl && push) {
      history.pushState({}, "", url);
    }
    window.scrollTo(0, scrollY);
    document.dispatchEvent(new CustomEvent("app:content-swapped"));
  }

  // A bare error response (a handler that never reached s.render(), e.g.
  // a plain http.Error/http.NotFound) has no <main> for applyPage to
  // find -- shown as a flash-style banner instead of attempting a
  // partial swap that would find nothing.
  function showRawFallback(text) {
    const liveMain = document.querySelector("main");
    if (!liveMain) return;
    const pre = document.createElement("pre");
    pre.className = "flash error";
    pre.textContent = typeof text === "string" && text.trim() ? text : "Something went wrong loading that page.";
    liveMain.replaceChildren(pre);
  }

  window.addEventListener("popstate", () => {
    navigateTo(location.pathname + location.search, { push: false });
  });

  // Same-origin link clicks -> replayed nav. A modified click (new tab,
  // new window, etc.), a different-origin href, or an explicit opt-out
  // all fall through to the browser's own default handling untouched.
  document.addEventListener("click", (e) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    const a = e.target.closest("a[href]");
    if (!a || a.target || a.hasAttribute("download") || a.hasAttribute("data-full-reload")) return;
    let url;
    try {
      url = new URL(a.href, location.href);
    } catch (err) {
      return;
    }
    if (url.origin !== location.origin || !isOpen()) return;
    e.preventDefault();
    navigateTo(url.pathname + url.search, { push: true });
  });

  // Form submits -> replayed submit. Skips multipart forms (file
  // upload -- not a fit for a JSON-message socket) and anything already
  // prevented by the confirm-modal handler above (which re-dispatches a
  // fresh, non-prevented submit once approved).
  document.addEventListener("submit", (e) => {
    if (e.defaultPrevented) return;
    const form = e.target;
    if (!(form instanceof HTMLFormElement)) return;
    if (form.enctype === "multipart/form-data" || form.hasAttribute("data-full-reload")) return;
    if (!isOpen()) return;
    const method = (form.getAttribute("method") || "GET").toUpperCase();
    const action = form.getAttribute("action") || location.pathname;
    const params = new URLSearchParams(new FormData(form));
    // FormData omits the submit button; carry which one was clicked
    // (several forms on this page name their submit buttons "action" to
    // distinguish, e.g. link vs unlink).
    if (e.submitter && e.submitter.name) params.set(e.submitter.name, e.submitter.value);
    e.preventDefault();
    submitForm(method, action, params.toString(), { push: true });
  });

  // ---- live status push (replaces the old per-node EventSource) ----

  function currentLiveNodes() {
    const set = new Set();
    document.querySelectorAll("[data-live-node]").forEach((card) => {
      const n = card.getAttribute("data-live-node");
      if (n) set.add(n);
    });
    return set;
  }

  // Called after every content swap (new [data-live-node] cards may have
  // appeared or disappeared) and once on (re)connect, where forceAll is
  // true because a new server-side connection has no memory of what was
  // previously subscribed.
  function syncLiveSubscriptions(forceAll) {
    const wanted = currentLiveNodes();
    const previous = forceAll ? new Set() : subscribedNodes;
    wanted.forEach((n) => {
      if (!previous.has(n)) send({ type: "subscribeLive", node: n });
    });
    previous.forEach((n) => {
      if (!wanted.has(n)) send({ type: "unsubscribeLive", node: n });
    });
    subscribedNodes = wanted;
  }

  function renderPill(container, receiving) {
    const pill = document.createElement("span");
    pill.className = "status-pill " + (receiving ? "down" : "up");
    const dot = document.createElement("span");
    dot.className = "dot";
    pill.appendChild(dot);
    pill.appendChild(
      document.createTextNode(receiving ? "On the air — signal on input" : "Idle — no signal on input")
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

  // DOM built with textContent/createElement throughout (renderPill/
  // renderConnected above) so callsign/description text from the node
  // directory can never inject markup.
  function onLive(type, node, data) {
    const card = document.querySelector('[data-live-node="' + CSS.escape(node) + '"]');
    if (!card) return; // subscribed from a page we've since swapped away from
    if (type === "live") {
      const indicatorEl = card.querySelector("[data-live-indicator]");
      const pillBox = card.querySelector("[data-live-pill]");
      const connBox = card.querySelector("[data-live-connected]");
      const signalCell = card.querySelector("[data-live-signal]");
      if (indicatorEl) indicatorEl.classList.add("on");
      if (pillBox) renderPill(pillBox, data.receiving);
      if (connBox) renderConnected(connBox, data.connected);
      if (signalCell && data.signalOnInput) signalCell.textContent = data.signalOnInput;
    } else {
      const historyBox = document.querySelector('[data-live-history="' + CSS.escape(node) + '"]');
      if (historyBox && typeof data === "string" && data.length) historyBox.innerHTML = data;
    }
  }

  document.addEventListener("app:content-swapped", () => syncLiveSubscriptions(false));

  connect();

  return { navigateTo: navigateTo, isOpen: isOpen };
})();

// Node page tabs: a purely client-side grouping of already-independent
// form sections (each [data-tab-panel] wraps one or more complete,
// unmodified forms) into tabs. The active tab is remembered per node in
// localStorage and re-applied here -- both on first load and again after
// every AppSocket content swap, since saving a form always re-renders
// this same page fresh (see this function's own call sites below).
// Progressive enhancement: every panel is plain visible markup with no
// hiding CSS of its own, so with JS disabled (or before this runs) the
// page is exactly the old single long page, nothing missing.
function initTabs() {
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
}

// Node page "radio hardware" toggle and the generalized multi-group
// version of it (e.g. the WX courtesy tone form's separate Normal/WX
// "tone" vs "sound file" choices): a section is shown when
// data-radio-mode-section="existing"/"new", or
// data-type-toggle-section="<radio name>:<radio value>", matches the
// currently checked radio. The "change" handling is delegated (works on
// any swapped-in form without rebinding), but the initial visibility
// still has to be (re)applied whenever a form appears, since a freshly
// swapped-in form's checked radio might not match what was visible
// before.
document.addEventListener("change", (e) => {
  if (e.target.matches("[data-radio-mode]")) applyRadioMode();
  else if (e.target.matches("[data-type-toggle]")) applyTypeToggle();
});

function applyRadioMode() {
  const radios = document.querySelectorAll("[data-radio-mode]");
  if (!radios.length) return;
  const checked = document.querySelector("[data-radio-mode]:checked");
  const mode = checked ? checked.value : "existing";
  ["existing", "new"].forEach((key) => {
    const section = document.querySelector('[data-radio-mode-section="' + key + '"]');
    if (section) section.style.display = key === mode ? "" : "none";
  });
}

function applyTypeToggle() {
  const radios = document.querySelectorAll("[data-type-toggle]");
  if (!radios.length) return;
  const groups = new Set();
  radios.forEach((r) => groups.add(r.name));
  groups.forEach((name) => {
    const checked = document.querySelector(`[data-type-toggle][name="${name}"]:checked`);
    const value = checked ? checked.value : "";
    document.querySelectorAll(`[data-type-toggle-section^="${name}:"]`).forEach((section) => {
      const sectionValue = section.getAttribute("data-type-toggle-section").split(":")[1];
      section.style.display = sectionValue === value ? "" : "none";
    });
  });
}

// Connections/Commands tab "quick action" buttons: fills the DTMF
// sequence field with <prefix><target node>, so the operator can review
// it before sending rather than the click sending anything directly.
// Delegated, so it needs no re-binding after a swap.
document.addEventListener("click", (e) => {
  const btn = e.target.closest("[data-fill-digits]");
  if (!btn) return;
  const digitsField = document.querySelector("[data-dtmf-field]");
  if (!digitsField) return;
  const target = document.querySelector("[data-target-node]");
  const prefix = btn.getAttribute("data-fill-digits");
  const node = target ? target.value.trim() : "";
  digitsField.value = prefix + node;
  digitsField.focus();
});

// "Play" buttons next to each Custom sound files row. One shared Audio
// element for the whole page (not one per row) so starting a second clip
// always stops whichever one was already playing, and clicking the same
// row's button again toggles it off rather than restarting it.
// Delegated (click target + shared Audio/activeBtn live in this closure,
// independent of any particular swap), so it needs no re-binding either.
(function () {
  const audio = new Audio();
  let activeBtn = null;

  function stop() {
    audio.pause();
    audio.currentTime = 0;
    if (activeBtn) activeBtn.textContent = "Play";
    activeBtn = null;
  }
  audio.addEventListener("ended", stop);

  document.addEventListener("click", (e) => {
    const btn = e.target.closest("[data-play-sound]");
    if (!btn) return;
    const wasActive = activeBtn === btn;
    stop();
    if (wasActive) return; // this click was the toggle-off
    audio.src = btn.getAttribute("data-play-sound");
    audio.play();
    btn.textContent = "Stop";
    activeBtn = btn;
  });
})();

// "Preview" button on the "Create from text" card: synthesizes speech
// for whatever voice/text is currently filled in and plays it
// immediately, via fetch rather than a form submission -- generating a
// preview isn't a navigation, and the audio response is a blob AppSocket
// deliberately doesn't handle (see its own doc comment: multipart/blob
// endpoints stay on plain fetch). Delegated; every field is looked up
// fresh at click time, so it always reads whichever preview button/form
// is currently on screen.
document.addEventListener("click", async (e) => {
  const btn = e.target.closest("[data-tts-preview]");
  if (!btn) return;
  const voiceField = document.getElementById("tts_voice");
  const engineField = document.getElementById("tts_engine");
  const textField = document.getElementById("tts_text");
  const status = document.querySelector("[data-tts-preview-status]");
  if (!textField) return;

  const text = textField.value.trim();
  if (!text) {
    textField.focus();
    return;
  }
  btn.disabled = true;
  const originalLabel = btn.textContent;
  btn.textContent = "Generating…";
  if (status) status.textContent = "";
  try {
    const body = new URLSearchParams({
      tts_voice: voiceField ? voiceField.value : "",
      tts_text: text,
      tts_engine: engineField ? engineField.value : "",
    });
    const resp = await fetch(btn.getAttribute("data-tts-preview"), { method: "POST", body });
    if (!resp.ok) {
      if (status) status.textContent = await resp.text();
      return;
    }
    const blob = await resp.blob();
    const audio = new Audio();
    audio.src = URL.createObjectURL(blob);
    audio.play();
  } catch (err) {
    if (status) status.textContent = "Couldn't reach the server to generate a preview.";
  } finally {
    btn.disabled = false;
    btn.textContent = originalLabel;
  }
});

// Stats page's dashboard status pill + stat grid. Plain short-polling
// rather than a push -- for a handful of scalar values refreshed every
// few seconds this is simpler and just as "realtime" as it needs to be.
// This card only exists on Stats, so the interval has to be started and
// stopped as that page comes and goes across content swaps rather than
// running for the life of the tab -- statusPollTimer tracks the one
// currently-running interval (if any) so re-initializing never stacks a
// second one on top.
let statusPollTimer = null;
function initStatusPoll() {
  if (statusPollTimer) {
    clearInterval(statusPollTimer);
    statusPollTimer = null;
  }
  const pill = document.querySelector("[data-status-pill]");
  if (!pill) return;

  async function poll() {
    try {
      const res = await fetch("/api/status", { credentials: "same-origin" });
      if (!res.ok) throw new Error("status " + res.status);
      const s = await res.json();

      pill.classList.toggle("up", s.asterisk_running);
      pill.classList.toggle("down", !s.asterisk_running);
      pill.querySelector(".label").textContent = s.asterisk_running ? "Asterisk running" : "Asterisk stopped";

      const uptimeEl = document.querySelector("[data-uptime]");
      const hostnameEl = document.querySelector("[data-hostname]");
      if (uptimeEl) uptimeEl.textContent = s.uptime || "—";
      if (hostnameEl) hostnameEl.textContent = s.hostname || "—";
    } catch (e) {
      pill.classList.remove("up");
      pill.classList.add("down");
      pill.querySelector(".label").textContent = "Status unavailable";
    }
  }

  poll();
  statusPollTimer = setInterval(poll, 4000);
}

// Runs every stateful, content-scoped init above once on first load and
// again after every AppSocket content swap (a saved form, a link
// navigation -- anything that replaces <main>). Each function re-scans
// the live DOM itself and is safe to call repeatedly/on a page that
// doesn't have its markup at all (each starts with its own presence
// check), so there's no "did this page change since last time" logic
// needed here.
function initPageFeatures() {
  initTabs();
  applyRadioMode();
  applyTypeToggle();
  initStatusPoll();
}

document.addEventListener("app:content-swapped", initPageFeatures);
initPageFeatures();
