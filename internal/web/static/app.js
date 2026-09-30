/* tapelog dashboard logic. SECURITY: every piece of untrusted data
   (tool names, args, results, reasons) reaches the DOM via textContent
   only — never innerHTML. Tool output is attacker-controlled. */
"use strict";

const $ = (id) => document.getElementById(id);
let lastSeq = 0;
let currentFile = "";
let chainBroken = false;

/* ---- helpers ------------------------------------------------------ */

// el builds a DOM element; any string values are assigned via
// textContent, so markup in tool output can never execute.
function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = String(text);
  return node;
}

async function api(path, opts) {
  const resp = await fetch(path, opts);
  if (!resp.ok) throw new Error(path + ": " + resp.status);
  return resp.json();
}

/* ---- approvals panel ---------------------------------------------- */

async function refreshPending() {
  let data;
  try {
    data = await api("/api/pending");
    $("status").textContent = "live";
  } catch (e) {
    $("status").textContent = "offline — showing last known data";
    return;
  }
  const list = $("pending-list");
  list.replaceChildren();
  $("pending-count").textContent = String(data.pending.length);
  if (data.pending.length === 0) {
    list.appendChild(el("p", "detail", "nothing parked — enjoy the quiet"));
    return;
  }
  for (const item of data.pending) {
    const card = el("div", "card");

    const meta = el("div", "meta");
    meta.appendChild(el("span", "", "#" + item.id));
    meta.appendChild(el("span", "", item.tool));
    meta.appendChild(el("span", "", item.age_seconds + "s"));
    card.appendChild(meta);

    const pre = el("pre", "", JSON.stringify(item.args));
    card.appendChild(pre);

    const row = el("div", "row");
    const note = el("input");
    note.type = "text";
    note.placeholder = "note (recorded)";
    const allow = el("button", "allow", "allow");
    const allowS = el("button", "allow", "allow session");
    const deny = el("button", "deny", "deny");
    allow.onclick = () => decide(item.id, "allow", note.value);
    allowS.onclick = () => decide(item.id, "allow_session", note.value);
    deny.onclick = () => decide(item.id, "deny", note.value);
    row.append(note, allow, allowS, deny);
    card.appendChild(row);

    list.appendChild(card);
  }
}

async function decide(id, verdict, note) {
  try {
    await api("/api/decide", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id, verdict, note }),
    });
    await refreshPending();
  } catch (e) {
    $("status").textContent = String(e);
  }
}

/* ---- session picker (directory mode) ------------------------------- */

const sessionInfo = {};

function showChain(info) {
  const warn = $("chain-warning");
  if (!warn) return;
  if (info && info.chain_ok === false) {
    warn.hidden = false;
    warn.textContent =
      "⚠ chain broken — first bad event: seq " + info.first_bad_seq +
      (info.problem ? " (" + info.problem + ")" : "") +
      ". This log was modified after writing; inspect it with `tapelog verify`.";
  } else {
    warn.hidden = true;
    warn.textContent = "";
  }
}

function showChainForFile(file) {
  showChain(sessionInfo[file]);
}

async function refreshSessions() {
  try {
    const data = await api("/api/sessions");
    if (!data.sessions || data.sessions.length === 0) return;
    const picker = $("sessions");
    picker.hidden = false;
    const prev = currentFile;
    picker.replaceChildren();
    for (const s of data.sessions) {
      sessionInfo[s.file] = s;
      const opt = el("option", "",
        (s.chain_ok === false ? "⚠ " : "") + s.file +
        "  ·  " + s.events + " events" + (s.denies ? "  ·  " + s.denies + " denied" : ""));
      opt.value = s.file;
      picker.appendChild(opt);
    }
    picker.onchange = () => {
      currentFile = picker.value;
      lastSeq = 0;
      chainBroken = false;
      $("log-list").replaceChildren();
      $("log-count").textContent = "0";
      showChainForFile(currentFile);
      refreshLog();
    };
    // Keep the selection across re-polls; only drive the view when the
    // picker is not actively being used to switch sessions.
    if (prev && Array.prototype.some.call(picker.options, (o) => o.value === prev)) {
      picker.value = prev;
    }
    currentFile = picker.value;
    showChainForFile(currentFile);
    if (!prev || prev !== currentFile) refreshLog();
  } catch (e) {
    /* single-session mode: no picker */
  }
}

/* ---- session log panel -------------------------------------------- */

function describe(p) {
  if (p.tool) return p.tool;
  if (p.reason) return p.reason;
  return "";
}

function addEvent(e) {
  const tbody = $("log-list");
  const p = e.payload || {};
  const cls = "decision-" + (p.verdict || "");
  const tr = el("tr", e.type === "policy/decision" ? cls : "");
  tr.appendChild(el("td", "", e.seq));
  tr.appendChild(el("td", "", String(e.ts).slice(11, 19)));
  tr.appendChild(el("td", "v", p.verdict ? p.verdict : e.type));
  tr.appendChild(el("td", "detail", describe(p)));
  tbody.prepend(tr);
  while (tbody.children.length > 500) tbody.removeChild(tbody.lastChild);
}

let refreshing = false;

async function refreshLog() {
  if (refreshing) return; // boot + intervals can overlap; never double-render
  refreshing = true;
  try {
    const url = "/api/log?after=" + lastSeq + (currentFile ? "&file=" + encodeURIComponent(currentFile) : "");
    const data = await api(url);

    // Chain status now rides on every poll (single-session mode too).
    showChain(data.chain_ok === undefined ? sessionInfo[currentFile] : data);

    // A tamper rewrites or truncates already-served events; incremental
    // `after=` polling would keep showing the pre-tamper rows. When the
    // chain just broke (or the file shrank / was replaced), re-pull the
    // whole log so the view shows what is actually on disk now; later
    // polls resume incremental appends (the recorder keeps writing).
    const broken = data.chain_ok === false;
    const shrank = data.next < lastSeq;
    if ((broken && !chainBroken) || shrank) {
      chainBroken = broken;
      lastSeq = 0;
      $("log-list").replaceChildren();
      $("log-count").textContent = "0";
      const full = await api("/api/log?after=0" + (currentFile ? "&file=" + encodeURIComponent(currentFile) : ""));
      for (const e of full.events || []) addEvent(e);
      if (full.events && full.events.length) {
        lastSeq = full.events[full.events.length - 1].seq;
      }
      $("log-count").textContent = String(lastSeq);
      return;
    }
    chainBroken = broken;

    for (const e of data.events || []) {
      addEvent(e);
      if (e.seq > lastSeq) lastSeq = e.seq;
    }
    $("log-count").textContent = String(lastSeq);
    if ($("tail").checked) window.scrollTo({ top: 0 });
  } catch (e) {
    /* status already shown by refreshPending */
  } finally {
    refreshing = false;
  }
}

/* ---- boot ---------------------------------------------------------- */

refreshSessions();
refreshPending();
refreshLog();
setInterval(refreshPending, 3000);
setInterval(refreshLog, 2000);
setInterval(refreshSessions, 5000);
