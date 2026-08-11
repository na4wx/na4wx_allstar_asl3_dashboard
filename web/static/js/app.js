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

// Keeps .help-tip tooltips inside the viewport. The CSS default centers
// a tooltip on its icon and opens it upward -- fine when the icon has
// room on both sides and above, but several real headings don't:
// Home's "Right now" card sits right under the sticky header (no room
// above), and SkywarnPlus's Pushover/SkyDescribe headings and node
// edit's WX courtesy tone heading put their help icon on its own line
// right after a block-level <h2>, hard against the card's left edge
// (no room to the left of center). Confirmed each of those pushed part
// of the tooltip off-screen with no way to read it.
//
// Delegated on mouseover/focusin (not mouseenter/focus, neither of
// which bubble) so every .help icon is covered, including ones
// AppSocket swaps in later, with no per-element re-binding needed.
// getBoundingClientRect() forces the browser to apply the :hover/
// :focus-within style (and therefore display:block) before measuring,
// so the rect reflects the tooltip's real, current position.
(function () {
  const TOOLTIP_MARGIN = 12;

  function positionHelpTip(help) {
    const tip = help.querySelector(".help-tip");
    if (!tip) return;

    // Re-measured from this same neutral state every time (not from
    // whatever an earlier hover already shifted it to), so corrections
    // never compound across repeated hovers.
    tip.style.setProperty("--tip-shift", "0px");
    tip.classList.remove("tip-below");

    let rect = tip.getBoundingClientRect();
    if (rect.top < TOOLTIP_MARGIN) {
      tip.classList.add("tip-below");
      rect = tip.getBoundingClientRect();
    }

    let shift = 0;
    if (rect.right > window.innerWidth - TOOLTIP_MARGIN) {
      shift = window.innerWidth - TOOLTIP_MARGIN - rect.right;
    } else if (rect.left < TOOLTIP_MARGIN) {
      shift = TOOLTIP_MARGIN - rect.left;
    }
    if (shift) tip.style.setProperty("--tip-shift", shift + "px");
  }

  document.addEventListener("mouseover", (e) => {
    const help = e.target.closest(".help");
    if (help) positionHelpTip(help);
  });
  document.addEventListener("focusin", (e) => {
    const help = e.target.closest(".help");
    if (help) positionHelpTip(help);
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
//
// The form-level path below carries the original submitter through to
// its own requestSubmit(submitter) call for the same reason: a form with
// more than one named submit button (e.g. Home's Link/Unlink quick
// action, both sharing one data-confirm on the <form> itself) needs the
// resubmitted event's own e.submitter to still identify which button was
// actually clicked -- requestSubmit() with no argument fires a fresh
// submit with e.submitter null, which silently dropped which action was
// requested (AppSocket's submit handler had nothing to add the button's
// name/value from) until this was fixed.
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
    const submitter = e.submitter || undefined;
    confirmModal(form.getAttribute("data-confirm"), {
      danger: form.hasAttribute("data-confirm-danger"),
    }).then((ok) => {
      if (!ok) return;
      approvedForms.add(form);
      form.requestSubmit(submitter);
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
    syncRestartBar(doc);
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

  // The "Asterisk must be restarted" bar (see layout.html's own
  // restartNeeded) lives outside <main>, in the persistent header/footer
  // chrome this module otherwise never touches -- so a WS-driven action
  // that flips the flag (any config save; see config.Store's own
  // OnChange) needs its own explicit sync here, or the bar would only
  // ever appear/disappear on a real full page load.
  function syncRestartBar(doc) {
    const newBar = doc.querySelector(".restart-bar");
    const liveBar = document.querySelector(".restart-bar");
    if (newBar && !liveBar) {
      const header = document.querySelector("header.topbar");
      if (header) header.insertAdjacentElement("afterend", newBar.cloneNode(true));
    } else if (!newBar && liveBar) {
      liveBar.remove();
    }
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

  // Renders the "Connected right now" card as a table -- the same shape
  // as the historical Link activity table (see node_history.html and
  // rptstatus.BuildLstatsRows): app_rpt's own "rpt lstats" columns,
  // dynamic rather than fixed by this app, plus a directory-looked-up
  // Callsign column and a talking indicator. Mirrors home.html's own
  // server-rendered markup exactly, so the first paint and every live
  // update after it look identical.
  function renderConnectedTable(container, headers, rows) {
    if (!rows || !rows.length) {
      const hint = document.createElement("div");
      hint.className = "hint";
      hint.textContent = "Nothing connected.";
      container.replaceChildren(hint);
      return;
    }
    const scroll = document.createElement("div");
    scroll.className = "table-scroll";
    const table = document.createElement("table");
    table.className = "data-table";

    const thead = document.createElement("thead");
    const headRow = document.createElement("tr");
    (headers || []).forEach((h) => {
      const th = document.createElement("th");
      th.textContent = h;
      headRow.appendChild(th);
    });
    const statusTh = document.createElement("th");
    statusTh.textContent = "Status";
    headRow.appendChild(statusTh);
    headRow.appendChild(document.createElement("th"));
    thead.appendChild(headRow);
    table.appendChild(thead);

    const tbody = document.createElement("tbody");
    rows.forEach((row) => {
      const tr = document.createElement("tr");
      if (row.keyed) tr.className = "keyed-row";
      (row.fields || []).forEach((v) => {
        const td = document.createElement("td");
        td.textContent = v;
        tr.appendChild(td);
      });
      const statusTd = document.createElement("td");
      if (row.keyed) {
        const badge = document.createElement("span");
        badge.className = "talking-badge";
        badge.textContent = "talking";
        statusTd.appendChild(badge);
      }
      tr.appendChild(statusTd);

      const peerTd = document.createElement("td");
      const peerBtn = document.createElement("button");
      peerBtn.type = "button";
      peerBtn.className = "btn btn-sm";
      peerBtn.setAttribute("data-peer-topology-btn", "");
      peerBtn.title = "Ask AllStarLink's own public status service (stats.allstarlink.org) what this station is currently connected to. Only works if that node publishes its status there — plenty of real nodes don't.";
      peerBtn.textContent = "Who's connected to them?";
      peerTd.appendChild(peerBtn);
      tr.appendChild(peerTd);

      tbody.appendChild(tr);
    });
    table.appendChild(tbody);
    scroll.appendChild(table);
    container.replaceChildren(scroll);
  }

  // DOM built with textContent/createElement throughout (renderPill/
  // renderConnectedTable above) so callsign/description text from the
  // node directory can never inject markup.
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
      if (connBox) renderConnectedTable(connBox, data.connectedHeaders, data.connectedRows);
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

  // ?tab=... (e.g. the navbar's own "Update available" link, which
  // points at /system?tab=control) wins over whatever tab was last
  // open here -- an explicit link to a specific tab should always land
  // on that tab, not wherever this browser happened to leave off.
  // activate() below still records it as the new "last open tab" via
  // its own localStorage.setItem, so a plain revisit to /system after
  // following that link keeps landing on the same place.
  const wanted = new URLSearchParams(location.search).get("tab");

  let initial = null;
  try {
    initial = localStorage.getItem(storageKey);
  } catch (e) {}
  activate(wanted || initial || buttons[0].getAttribute("data-tab-target"));
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

// "Find by callsign" (Home page's own "Connect node N to another node"
// card) -- collapsed by default behind a small icon button next to the
// node-number field, rather than a second always-visible field, since
// the common case is already knowing the node number. Scoped to the
// clicked button's own .field/card throughout (Home can show one of
// these per configured node), never a bare document-wide query.
document.addEventListener("click", (e) => {
  const toggle = e.target.closest("[data-callsign-toggle]");
  if (!toggle) return;
  const field = toggle.closest(".field");
  const wrap = field && field.querySelector("[data-callsign-search-wrap]");
  if (!wrap) return;
  wrap.hidden = !wrap.hidden;
  if (!wrap.hidden) {
    const input = wrap.querySelector("[data-callsign-search]");
    if (input) input.focus();
  }
});

// Looks up a node by callsign via GET /node-search, the reverse of the
// number->callsign labels shown everywhere else in this app. Debounced
// so typing doesn't fire a request per keystroke; delegated so it needs
// no re-binding after a swap. "input" bubbles the same as "click" does
// elsewhere in this file, so this is the same pattern, just a
// different event.
let callsignSearchTimer = null;
document.addEventListener("input", (e) => {
  const input = e.target.closest("[data-callsign-search]");
  if (!input) return;
  const wrap = input.closest("[data-callsign-search-wrap]");
  const results = wrap && wrap.querySelector("[data-callsign-results]");
  if (!results) return;

  clearTimeout(callsignSearchTimer);
  const query = input.value.trim();
  if (query.length < 2) {
    results.hidden = true;
    results.replaceChildren();
    return;
  }
  callsignSearchTimer = setTimeout(() => {
    fetch("/node-search?q=" + encodeURIComponent(query))
      .then((r) => (r.ok ? r.json() : Promise.reject()))
      .then((matches) => {
        results.replaceChildren();
        if (!matches || matches.length === 0) {
          const li = document.createElement("li");
          li.className = "hint";
          li.textContent = "No matching callsigns";
          results.appendChild(li);
        } else {
          matches.forEach((m) => {
            const li = document.createElement("li");
            li.tabIndex = 0;
            li.dataset.node = m.number;
            let text = m.number + " — " + m.callsign;
            if (m.location) text += " (" + m.location + ")";
            li.textContent = text;
            results.appendChild(li);
          });
        }
        results.hidden = false;
      })
      .catch(() => {
        results.hidden = true;
      });
  }, 250);
});

// Picking a result fills that same field's own [data-target-node] and
// collapses the search box back down -- same end state as never having
// opened it at all, except the field is now populated.
document.addEventListener("click", (e) => {
  const li = e.target.closest("[data-callsign-results] li[data-node]");
  if (!li) return;
  const field = li.closest(".field");
  const target = field && field.querySelector("[data-target-node]");
  const wrap = li.closest("[data-callsign-search-wrap]");
  if (target) target.value = li.dataset.node;
  if (wrap) {
    const input = wrap.querySelector("[data-callsign-search]");
    if (input) input.value = "";
    wrap.hidden = true;
  }
});

// Clicking anywhere outside an open search box collapses it -- same
// convention as a native <select> or browser autofill dropdown.
document.addEventListener("click", (e) => {
  if (e.target.closest("[data-callsign-toggle]") || e.target.closest("[data-callsign-search-wrap]")) return;
  document.querySelectorAll("[data-callsign-search-wrap]:not([hidden])").forEach((wrap) => {
    wrap.hidden = true;
  });
});

document.addEventListener("keydown", (e) => {
  if (e.key !== "Escape") return;
  const wrap = e.target.closest("[data-callsign-search-wrap]");
  if (!wrap) return;
  wrap.hidden = true;
  const field = wrap.closest(".field");
  const toggle = field && field.querySelector("[data-callsign-toggle]");
  if (toggle) toggle.focus();
});

// Show/Hide button next to a password field (see .password-field in
// style.css) -- data-toggle-password names the input's own id, rather
// than relying on DOM position, so the button and field don't have to
// be direct siblings. Delegated, so it works on every such field,
// including ones swapped in later by AppSocket, with no re-binding.
document.addEventListener("click", (e) => {
  const btn = e.target.closest("[data-toggle-password]");
  if (!btn) return;
  const input = document.getElementById(btn.getAttribute("data-toggle-password"));
  if (!input) return;
  const show = input.type === "password";
  input.type = show ? "text" : "password";
  btn.textContent = show ? "Hide" : "Show";
  btn.setAttribute("aria-pressed", show ? "true" : "false");
});

// Home's "Connected right now" table: clicking a row fills that same
// node's own "Other node's number" field with the clicked peer, so
// linking/unlinking someone already on screen is a click instead of
// retyping their number. Delegated (works on both the server-rendered
// table and AppSocket's own live-updated one -- see renderConnectedTable
// -- without needing to know which one is showing) and reads the DOM
// directly rather than needing new data attributes: the row's own first
// cell is always the peer's node number (see rptstatus.BuildLstatsRows,
// which always puts app_rpt's own NODE column first), and the target
// field's id is already scoped per-node ("quick_target_<this node>").
document.addEventListener("click", (e) => {
  // Excludes the row's own "Who's connected to them?" button (see
  // below) -- that button lives inside the row now, and without this
  // check every click on it would also refill the quick-connect field
  // as an unwanted side effect.
  if (e.target.closest("[data-peer-topology-btn]")) return;
  const row = e.target.closest("[data-live-connected] tbody tr");
  if (!row) return;
  const card = row.closest("[data-live-node]");
  const firstCell = row.querySelector("td");
  if (!card || !firstCell) return;
  const target = document.getElementById("quick_target_" + card.getAttribute("data-live-node"));
  if (!target) return;
  target.value = firstCell.textContent.trim();
  target.focus();
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

// Toasts: the server still renders a plain flash div at the top of
// <main> (see layout.html) -- kept as the single source of truth for
// what happened, and as the plain-HTTP fallback's only notification --
// but with JS running it's immediately pulled out and shown as a
// floating toast instead, so the result of an action is seen no matter
// how far down the page the operator has scrolled (a long node-edit
// page, a long history table). One shared container for the life of the
// tab; a toast auto-dismisses on its own or on click.
let toastContainer = null;
function getToastContainer() {
  if (!toastContainer) {
    toastContainer = document.createElement("div");
    toastContainer.className = "toast-container";
    document.body.appendChild(toastContainer);
  }
  return toastContainer;
}

function showToast(kind, message) {
  if (!message) return;
  const container = getToastContainer();
  const toast = document.createElement("div");
  toast.className = "toast" + (kind ? " " + kind : "");
  toast.textContent = message;
  container.appendChild(toast);

  let dismissed = false;
  const dismiss = () => {
    if (dismissed) return;
    dismissed = true;
    clearTimeout(timer);
    toast.classList.remove("show");
    toast.addEventListener("transitionend", () => toast.remove(), { once: true });
    // Fallback in case the transition never fires (prefers-reduced-motion,
    // or the toast was already detached) -- never leave a dead toast
    // sitting in the container forever.
    setTimeout(() => toast.remove(), 400);
  };
  toast.addEventListener("click", dismiss);
  const timer = setTimeout(dismiss, kind === "error" ? 8000 : 5000);

  requestAnimationFrame(() => toast.classList.add("show"));
}

// The flash div is always main's own first child when present (see
// layout.html: `<main>{{if .Flash}}<div class="flash ...">{{end}}...`),
// never nested inside the page's own content -- so this selector is
// exact, not a guess.
function convertFlashToToast() {
  const flash = document.querySelector("main > .flash");
  if (!flash) return;
  const kind = flash.classList.contains("error") ? "error" : flash.classList.contains("ok") ? "ok" : "";
  const message = flash.textContent.trim();
  flash.remove();
  showToast(kind, message);
}

// Each row's own "Who's connected to them?" button on Home's
// "Connected right now" table: fetches internal/allstarapi's
// peer-status endpoint (see internal/server/peertopology.go) for that
// one row's node and shows the result in a modal. AllStarLink's own
// public status service is the only way this app has to see a REMOTE
// node's own connections, since nothing about that node runs on this
// machine's own Asterisk. Built lazily into document.body, same
// pattern as confirmModal above, so it survives every content swap
// untouched.
const peerTopologyModal = (function () {
  let backdrop, titleEl, bodyEl;

  function build() {
    backdrop = document.createElement("div");
    backdrop.className = "modal-backdrop";
    backdrop.hidden = true;

    const card = document.createElement("div");
    card.className = "modal-card modal-card--wide";
    card.setAttribute("role", "dialog");
    card.setAttribute("aria-modal", "true");

    const header = document.createElement("div");
    header.className = "modal-card-header";
    titleEl = document.createElement("h2");
    const closeBtn = document.createElement("button");
    closeBtn.type = "button";
    closeBtn.className = "modal-close";
    closeBtn.setAttribute("aria-label", "Close");
    closeBtn.textContent = "×";
    closeBtn.addEventListener("click", hide);
    header.appendChild(titleEl);
    header.appendChild(closeBtn);
    card.appendChild(header);

    bodyEl = document.createElement("div");
    bodyEl.className = "modal-card-body";
    card.appendChild(bodyEl);

    backdrop.appendChild(card);
    document.body.appendChild(backdrop);

    backdrop.addEventListener("click", (e) => {
      if (e.target === backdrop) hide();
    });
    document.addEventListener("keydown", (e) => {
      if (!backdrop.hidden && e.key === "Escape") hide();
    });
  }

  function hide() {
    if (backdrop) backdrop.hidden = true;
  }

  // Built with createElement/textContent throughout -- callsign/
  // location text ultimately comes from stats.allstarlink.org, an
  // external, unauthenticated source, so none of it is ever treated as
  // markup.
  function renderPeerGroup(peer) {
    const group = document.createElement("div");
    group.className = "peer-topology-group";

    const h3 = document.createElement("h3");
    h3.textContent = peer.number + (peer.callsign ? " — " + peer.callsign : "");
    group.appendChild(h3);

    if (!peer.ok) {
      const hint = document.createElement("div");
      hint.className = "hint";
      hint.textContent = peer.error || "Couldn't look up this node.";
      group.appendChild(hint);
      return group;
    }

    if (!peer.connectedTo || !peer.connectedTo.length) {
      const hint = document.createElement("div");
      hint.className = "hint";
      hint.textContent = "Not connected to anything else right now.";
      group.appendChild(hint);
      return group;
    }

    const ul = document.createElement("ul");
    ul.className = "peer-topology-list";
    peer.connectedTo.forEach((p) => {
      const li = document.createElement("li");
      let text = p.number;
      if (p.callsign) text += " — " + p.callsign;
      if (p.location) text += " (" + p.location + ")";
      li.textContent = text;
      ul.appendChild(li);
    });
    group.appendChild(ul);
    return group;
  }

  function show(node) {
    if (!backdrop) build();
    titleEl.textContent = "Who else is node " + node + " connected to?";
    bodyEl.replaceChildren();
    const loading = document.createElement("div");
    loading.className = "hint";
    loading.textContent = "Checking AllStarLink…";
    bodyEl.appendChild(loading);
    backdrop.hidden = false;

    fetch("/peer-status/" + encodeURIComponent(node))
      .then((r) => {
        if (!r.ok) throw new Error("request failed");
        return r.json();
      })
      .then((data) => {
        bodyEl.replaceChildren();
        bodyEl.appendChild(renderPeerGroup(data));
      })
      .catch(() => {
        bodyEl.replaceChildren();
        const hint = document.createElement("div");
        hint.className = "hint";
        hint.textContent = "Couldn't reach this node's status right now. Try again in a moment.";
        bodyEl.appendChild(hint);
      });
  }

  return { show };
})();

// Row's own button, not the row-click-to-fill handler above: reads the
// node number the same way that one does (the row's own first cell --
// see rptstatus.BuildLstatsRows, which always puts app_rpt's own NODE
// column first), so this needs no data-node attribute of its own and
// works identically on both the server-rendered table and AppSocket's
// live-updated one.
document.addEventListener("click", (e) => {
  const btn = e.target.closest("[data-peer-topology-btn]");
  if (!btn) return;
  const row = btn.closest("tr");
  const firstCell = row && row.querySelector("td");
  if (!firstCell) return;
  peerTopologyModal.show(firstCell.textContent.trim());
});

// System page's "Check for updates" modal -- same build()/show() shape
// as peerTopologyModal above, plus a second phase (runUpdate) that
// opens a dedicated WebSocket (not the shared /ws nav socket -- this
// carries raw install.sh output, not a page to swap in) and streams
// output live into a <pre> block.
const updateModal = (function () {
  let backdrop, bodyEl;

  function build() {
    backdrop = document.createElement("div");
    backdrop.className = "modal-backdrop";
    backdrop.hidden = true;

    const card = document.createElement("div");
    card.className = "modal-card modal-card--wide";
    card.setAttribute("role", "dialog");
    card.setAttribute("aria-modal", "true");

    const header = document.createElement("div");
    header.className = "modal-card-header";
    const titleEl = document.createElement("h2");
    titleEl.textContent = "Software updates";
    const closeBtn = document.createElement("button");
    closeBtn.type = "button";
    closeBtn.className = "modal-close";
    closeBtn.setAttribute("aria-label", "Close");
    closeBtn.textContent = "×";
    closeBtn.addEventListener("click", hide);
    header.appendChild(titleEl);
    header.appendChild(closeBtn);
    card.appendChild(header);

    bodyEl = document.createElement("div");
    bodyEl.className = "modal-card-body";
    card.appendChild(bodyEl);

    backdrop.appendChild(card);
    document.body.appendChild(backdrop);

    backdrop.addEventListener("click", (e) => {
      if (e.target === backdrop) hide();
    });
    document.addEventListener("keydown", (e) => {
      if (!backdrop.hidden && e.key === "Escape") hide();
    });
  }

  function hide() {
    if (backdrop) backdrop.hidden = true;
  }

  function setBody(...children) {
    bodyEl.replaceChildren(...children);
  }

  function hintEl(text) {
    const div = document.createElement("div");
    div.className = "hint";
    div.textContent = text;
    return div;
  }

  function checkStatus() {
    setBody(hintEl("Checking for updates…"));
    fetch("/system/update/check")
      .then((r) => {
        if (!r.ok) throw new Error("request failed");
        return r.json();
      })
      .then(renderStatus)
      .catch(() => setBody(hintEl("Couldn't check for updates right now. Try again in a moment.")));
  }

  function renderStatus(st) {
    if (!st.available) {
      setBody(hintEl("Update checking isn't set up on this device — it's only available when the app was installed by running install.sh from a real git checkout, not deployed as a standalone binary."));
      return;
    }
    if (st.error) {
      setBody(hintEl(st.error));
      return;
    }
    if (st.upToDate) {
      const label = st.branch ? " (branch " + st.branch + ")" : "";
      setBody(hintEl("You're up to date" + label + "."));
      return;
    }

    const nodes = [];
    const summary = document.createElement("p");
    summary.className = "section-intro";
    const count = st.behind + (st.behind === 1 ? " update" : " updates");
    summary.textContent = count + " available on branch " + st.branch + ":";
    nodes.push(summary);

    if (st.commits && st.commits.length) {
      const pre = document.createElement("pre");
      pre.className = "raw-block";
      pre.textContent = st.commits.join("\n") + (st.behind > st.commits.length ? "\n…" : "");
      nodes.push(pre);
    }

    const actions = document.createElement("div");
    actions.className = "actions";
    const runBtn = document.createElement("button");
    runBtn.type = "button";
    runBtn.className = "primary";
    runBtn.textContent = "Run update now";
    runBtn.addEventListener("click", () => {
      confirmModal("Pull and rebuild now? This restarts the dashboard once it finishes — any open pages will need a reload, and the radio itself is unaffected.", { okLabel: "Run update" }).then((ok) => {
        if (ok) runUpdate();
      });
    });
    actions.appendChild(runBtn);
    nodes.push(actions);

    setBody(...nodes);
  }

  function runUpdate() {
    const pre = document.createElement("pre");
    pre.className = "raw-block";
    pre.style.maxHeight = "320px";
    pre.style.overflowY = "auto";
    pre.textContent = "";
    setBody(pre);

    const appendLine = (text) => {
      pre.textContent += (pre.textContent ? "\n" : "") + text;
      pre.scrollTop = pre.scrollHeight;
    };

    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const socket = new WebSocket(proto + "//" + location.host + "/system/update/run");
    let finished = false;

    socket.addEventListener("message", (ev) => {
      let msg;
      try {
        msg = JSON.parse(ev.data);
      } catch {
        return;
      }
      if (msg.type === "line") {
        appendLine(msg.line);
      } else if (msg.type === "error") {
        finished = true;
        appendLine("");
        appendLine("Failed: " + msg.line);
      } else if (msg.type === "done") {
        finished = true;
        appendLine("");
        appendLine("Update complete — restarting…");
        waitForRestart();
      }
    });

    socket.addEventListener("close", () => {
      // A close with no "done"/"error" ever received is the expected
      // shape of a *successful* run (see update.go's own doc comment:
      // install.sh's own last step restarts this process, which always
      // wins the race against that final message landing first) -- so
      // this reads as success, not silently doing nothing.
      if (!finished) {
        appendLine("");
        appendLine("Connection closed — the dashboard is most likely restarting now.");
        waitForRestart();
      }
    });
  }

  // Polls / every couple seconds until it answers again, then offers a
  // reload -- install.sh's own final act is restarting this very
  // process, so there's a real gap (a few seconds, typically) where
  // nothing is listening at all.
  function waitForRestart() {
    const status = document.createElement("p");
    status.className = "hint";
    status.textContent = "Waiting for the dashboard to come back…";
    bodyEl.appendChild(status);

    const poll = () => {
      fetch("/", { method: "HEAD", cache: "no-store" })
        .then(() => {
          status.textContent = "Back up.";
          const actions = document.createElement("div");
          actions.className = "actions";
          const reloadBtn = document.createElement("button");
          reloadBtn.type = "button";
          reloadBtn.className = "primary";
          reloadBtn.textContent = "Reload page";
          reloadBtn.addEventListener("click", () => location.reload());
          actions.appendChild(reloadBtn);
          bodyEl.appendChild(actions);
        })
        .catch(() => setTimeout(poll, 2000));
    };
    setTimeout(poll, 2000);
  }

  function show() {
    if (!backdrop) build();
    backdrop.hidden = false;
    checkStatus();
  }

  return { show };
})();

document.addEventListener("click", (e) => {
  if (!e.target.closest("[data-check-updates-btn]")) return;
  updateModal.show();
});

// Runs every stateful, content-scoped init above once on first load and
// again after every AppSocket content swap (a saved form, a link
// navigation -- anything that replaces <main>). Each function re-scans
// the live DOM itself and is safe to call repeatedly/on a page that
// doesn't have its markup at all (each starts with its own presence
// check), so there's no "did this page change since last time" logic
// needed here.
function initPageFeatures() {
  convertFlashToToast();
  initTabs();
  applyRadioMode();
  applyTypeToggle();
  initStatusPoll();
}

document.addEventListener("app:content-swapped", initPageFeatures);
initPageFeatures();
