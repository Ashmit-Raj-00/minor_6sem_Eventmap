const CFG = (globalThis && globalThis.__EVENTMAP_CONFIG__) || {};
const API = CFG.apiBase || "";
const tokenKey = "eventmap_token";
const emailKey = "eventmap_last_email";
const viewKey = "eventmap_view";
const mapStateKey = "eventmap_map_state_v1";

let token = localStorage.getItem(tokenKey) || "";
let me = null;
let authMode = "login"; // login | register

const supabaseLib = globalThis && globalThis.supabase;
const supabaseEnabled = Boolean(CFG.supabaseUrl && CFG.supabaseAnonKey && supabaseLib && typeof supabaseLib.createClient === "function");
const sb = supabaseEnabled ? supabaseLib.createClient(CFG.supabaseUrl, CFG.supabaseAnonKey) : null;

function explainSupabaseNotConfigured() {
  const problems = [];
  if (!CFG.supabaseUrl) problems.push("SUPABASE_URL missing");
  if (!CFG.supabaseAnonKey) problems.push("SUPABASE_ANON_KEY missing");
  if (!supabaseLib) problems.push("Supabase JS not loaded");
  const hint = problems.length ? problems.join(", ") : "Unknown reason";

  const msg = $("authMsg");
  if (msg) {
    setMsg(
      msg,
      `Supabase not configured (${hint}). If you're on Netlify: set SUPABASE_URL + SUPABASE_ANON_KEY env vars and redeploy, then open /config.js to verify.`,
      "error"
    );
  }
  console.error("Supabase not configured", { cfg: CFG, hasSupabaseLib: Boolean(supabaseLib) });
}

let map = null;
let myMarker = null;
let pinMarker = null;
let pinned = { lat: null, lng: null };
let eventMarkers = [];
let selectedEventId = "";
let allEvents = [];
let lastLocation = null; // {lat,lng}
let eventsLoading = false;

function $(id) {
  return document.getElementById(id);
}

function debounce(fn, ms) {
  let t = null;
  return (...args) => {
    if (t) clearTimeout(t);
    t = setTimeout(() => fn(...args), ms);
  };
}

function setMsg(el, text, kind) {
  el.textContent = text || "";
  el.classList.remove("ok", "error");
  if (kind) el.classList.add(kind);
}

function toast(text, kind = "") {
  const wrap = $("toasts");
  if (!wrap) return;
  const el = document.createElement("div");
  el.className = "toast" + (kind ? ` ${kind}` : "");
  el.textContent = text;
  wrap.appendChild(el);
  setTimeout(() => el.remove(), 3200);
}

async function initSupabaseAuth() {
  if (!sb) return;

  // Prefer OAuth UI when Supabase is configured
  const authModeLogin = $("authModeLogin");
  const authModeRegister = $("authModeRegister");
  const authSubmitBtn = $("authSubmitBtn");
  const regExtras = $("regExtras");

  if (authModeLogin) authModeLogin.hidden = true;
  if (authModeRegister) authModeRegister.hidden = true;
  if (authSubmitBtn) authSubmitBtn.hidden = true;
  if (regExtras) regExtras.hidden = true;

  const emailEl = $("authEmail");
  const passEl = $("authPassword");
  if (emailEl) emailEl.closest("label")?.classList.add("muted");
  if (passEl) passEl.closest("label")?.classList.add("muted");
  if (emailEl) emailEl.disabled = true;
  if (passEl) passEl.disabled = true;

  const { data } = await sb.auth.getSession();
  token = data?.session?.access_token || "";
  if (token) localStorage.setItem(tokenKey, token);
  else localStorage.removeItem(tokenKey);

  sb.auth.onAuthStateChange(async (_event, session) => {
    token = session?.access_token || "";
    if (token) localStorage.setItem(tokenKey, token);
    else localStorage.removeItem(tokenKey);
    await refreshMe();
    await refreshLeaderboards().catch(() => {});
  });
}

async function onGoogleLogin() {
  const msg = $("authMsg");
  setMsg(msg, "", "");
  if (!sb) {
    explainSupabaseNotConfigured();
    return;
  }
  try {
    await sb.auth.signInWithOAuth({
      provider: "google",
      options: { redirectTo: window.location.origin },
    });
  } catch (err) {
    const text = err?.message || "Login failed";
    setMsg(msg, text, "error");
    toast(text, "error");
  }
}

async function api(path, opts = {}) {
  const headers = Object.assign({ "Content-Type": "application/json" }, opts.headers || {});
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const res = await fetch(`${API}${path}`, Object.assign({}, opts, { headers }));
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(data.error || `HTTP ${res.status}`);
    err.status = res.status;
    err.data = data;
    throw err;
  }
  return data;
}

function initMap() {
  const start = { lat: 12.9716, lng: 77.5946 }; // fallback
  const saved = (() => {
    try {
      return JSON.parse(localStorage.getItem(mapStateKey) || "null");
    } catch {
      return null;
    }
  })();
  const initial = saved && typeof saved.lat === "number" && typeof saved.lng === "number" && typeof saved.zoom === "number" ? saved : { lat: start.lat, lng: start.lng, zoom: 12 };

  map = L.map("map", { zoomControl: true }).setView([initial.lat, initial.lng], initial.zoom);
  L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
    maxZoom: 19,
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
  }).addTo(map);

  map.on(
    "moveend",
    debounce(() => {
      const c = map.getCenter();
      localStorage.setItem(mapStateKey, JSON.stringify({ lat: c.lat, lng: c.lng, zoom: map.getZoom() }));
    }, 400)
  );

  map.on("click", (e) => {
    pinned = { lat: e.latlng.lat, lng: e.latlng.lng };
    $("pinnedLatLng").textContent = `${pinned.lat.toFixed(6)}, ${pinned.lng.toFixed(6)}`;
    if (!pinMarker) {
      pinMarker = L.marker([pinned.lat, pinned.lng], { draggable: true }).addTo(map);
      pinMarker.on("dragend", () => {
        const ll = pinMarker.getLatLng();
        pinned = { lat: ll.lat, lng: ll.lng };
        $("pinnedLatLng").textContent = `${pinned.lat.toFixed(6)}, ${pinned.lng.toFixed(6)}`;
      });
    } else {
      pinMarker.setLatLng([pinned.lat, pinned.lng]);
    }
  });
}

async function locate() {
  return new Promise((resolve, reject) => {
    if (!navigator.geolocation) return reject(new Error("Geolocation not supported"));
    navigator.geolocation.getCurrentPosition(resolve, reject, { enableHighAccuracy: true, timeout: 8000 });
  });
}

async function setMyLocation() {
  const pos = await locate();
  const lat = pos.coords.latitude;
  const lng = pos.coords.longitude;
  map.setView([lat, lng], 13);
  if (!myMarker) {
    myMarker = L.circleMarker([lat, lng], { radius: 8, color: "#7ef0c1" }).addTo(map);
  } else {
    myMarker.setLatLng([lat, lng]);
  }
  lastLocation = { lat, lng };
  updateEventDistance();
  return lastLocation;
}

function clearEventMarkers() {
  for (const m of eventMarkers) m.remove();
  eventMarkers = [];
}

function tagChipHtml(tag) {
  const t = String(tag || "");
  const color = colorFromString(t);
  const bg = `linear-gradient(180deg, rgba(255,255,255,0.16), rgba(0,0,0,0.10)), ${color}`;
  return `<span class="tagChip" style="border-color:${color};background:${bg}">${escapeHtml(t)}</span>`;
}

function addEventMarkers(events) {
  clearEventMarkers();
  for (const e of events) {
    if (typeof e.lat !== "number" || typeof e.lng !== "number") continue;
    const color = pickEventColor(e);
    const m = L.circleMarker([e.lat, e.lng], {
      radius: e.id === selectedEventId ? 10 : 8,
      color,
      fillColor: color,
      fillOpacity: 0.75,
      weight: 2,
    }).addTo(map);
    m.on("click", () => selectEvent(e.id));
    const tags = (e.tags || []).map((t) => tagChipHtml(t)).join(" ");
    m.bindPopup(
      `<div style="display:grid;gap:6px;min-width:210px">
        <div><strong>${escapeHtml(e.title)}</strong></div>
        <div class="muted" style="font-size:12px">${escapeHtml(e.address || "")}</div>
        <div style="display:flex;gap:6px;flex-wrap:wrap">${tags || '<span class="muted" style="font-size:12px">no tags</span>'}</div>
      </div>`
    );
    eventMarkers.push(m);
  }
}

function escapeHtml(s) {
  return String(s || "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function renderUser() {
  const who = $("whoami");
  const logoutBtn = $("logoutBtn");
  const createWrap = $("createWrap");
  const scoreLine = $("scoreLine");
  const levelPill = $("levelPill");
  const xpFill = $("xpFill");
  const xpText = $("xpText");
  const lbRefreshBtn = $("lbRefreshBtn");
  if (!me) {
    who.textContent = "Not signed in";
    scoreLine.textContent = "Sign in to earn XP";
    logoutBtn.hidden = true;
    createWrap.hidden = true;
    levelPill.hidden = true;
    xpFill.style.width = "0%";
    xpText.textContent = "0 / 100 XP";
    lbRefreshBtn.disabled = true;
    return;
  }
  who.textContent = `${me.email} (${me.role})`;
  logoutBtn.hidden = false;
  createWrap.hidden = !(me.role === "organizer" || me.role === "admin");
  lbRefreshBtn.disabled = false;

  const score = me.score || { points: 0, level: 1, nextLevelAt: 100 };
  const prev = Math.max(0, (score.level - 1) * 100);
  const denom = Math.max(1, score.nextLevelAt - prev);
  const pct = Math.max(0, Math.min(1, (score.points - prev) / denom));
  const nowInLevel = Math.max(0, score.points - prev);
  scoreLine.textContent = `${score.points} XP • Level ${score.level}`;
  levelPill.hidden = false;
  levelPill.textContent = `Level ${score.level}`;
  xpFill.style.width = `${Math.round(pct * 100)}%`;
  xpText.textContent = `${nowInLevel} / ${denom} XP`;
}

function renderEvents(events) {
  const el = $("eventsList");
  el.innerHTML = "";
  for (const e of events) {
    const row = document.createElement("div");
    row.className = "eventRow" + (e.id === selectedEventId ? " selected" : "");
    row.style.setProperty("--event-color", pickEventColor(e));
    row.onclick = () => selectEvent(e.id);

    const title = document.createElement("div");
    title.className = "eventTitle";
    title.textContent = e.title;

    const meta = document.createElement("div");
    meta.className = "eventMeta";
    meta.innerHTML = `
      <span>${new Date(e.startsAt).toLocaleString()}</span>
      <span class="muted">${escapeHtml(e.address || "")}</span>
    `;

    const tags = document.createElement("div");
    tags.className = "tags";
    const tagList = Array.isArray(e.tags) ? e.tags : [];
    if (tagList.length) {
      for (const t of tagList) {
        const chip = document.createElement("span");
        chip.className = "tagChip";
        chip.textContent = t;
        const color = colorFromString(String(t));
        chip.style.borderColor = color;
        chip.style.background = `linear-gradient(180deg, rgba(255,255,255,0.16), rgba(0,0,0,0.10)), ${color}`;
        chip.onclick = (ev) => {
          ev.stopPropagation();
          $("tagFilter").value = t;
          render();
        };
        tags.appendChild(chip);
      }
    } else {
      const chip = document.createElement("span");
      chip.className = "tagChip muted";
      chip.textContent = "no tags";
      tags.appendChild(chip);
    }

    row.appendChild(title);
    row.appendChild(meta);
    row.appendChild(tags);
    el.appendChild(row);
  }
}

async function refreshEvents() {
  if (eventsLoading) return;
  eventsLoading = true;
  const refreshBtn = $("refreshBtn");
  if (refreshBtn) refreshBtn.disabled = true;
  try {
    const radiusKm = Number($("radiusKm").value || "0");
    const center = map.getCenter();
    const lat = center.lat;
    const lng = center.lng;

    try {
      const data = await api(`/api/events/nearby?lat=${lat}&lng=${lng}&radius_km=${radiusKm}`, { method: "GET" });
      allEvents = data.events || [];
    } catch {
      const data = await api(`/api/events`, { method: "GET" });
      allEvents = data.events || [];
    }
    if (selectedEventId && !allEvents.some((e) => e.id === selectedEventId)) {
      selectedEventId = "";
    }
    selectEvent(selectedEventId);
  } catch (err) {
    const msg = err?.message || "Could not refresh events";
    toast(msg, "error");
  } finally {
    eventsLoading = false;
    if (refreshBtn) refreshBtn.disabled = false;
  }
}

async function loadMe() {
  if (!token) {
    me = null;
    renderUser();
    return;
  }
  try {
    me = await api("/api/me", { method: "GET" });
  } catch {
    token = "";
    localStorage.removeItem(tokenKey);
    me = null;
  }
  renderUser();
}

async function refreshMe() {
  if (!token) {
    me = null;
    renderUser();
    return;
  }
  try {
    me = await api("/api/me", { method: "GET" });
  } catch {
    token = "";
    localStorage.removeItem(tokenKey);
    me = null;
  }
  renderUser();
  renderEventDetail(allEvents.find((e) => e.id === selectedEventId) || null);
}

function setAuthMode(mode) {
  authMode = mode === "register" ? "register" : "login";
  $("authTitle").textContent = authMode.toUpperCase();
  $("regExtras").hidden = authMode !== "register";
  $("authPassword").setAttribute("autocomplete", authMode === "register" ? "new-password" : "current-password");
  $("authSubmitBtn").textContent = authMode === "register" ? "Create + Start!" : "Start!";
  setMsg($("authMsg"), "", "");
}

async function onAuthSubmit() {
  const msg = $("authMsg");
  setMsg(msg, "", "");
  const email = $("authEmail").value;
  const password = $("authPassword").value;
  localStorage.setItem(emailKey, email || "");

  try {
    if (authMode === "register") {
      await api("/api/auth/register", {
        method: "POST",
        body: JSON.stringify({
          email,
          password,
          role: $("authRole").value,
        }),
      });
    }
    const data = await api("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    });
    token = data.token;
    localStorage.setItem(tokenKey, token);
    me = data.user;
    await refreshMe();
    await refreshLeaderboards();
    toast(authMode === "register" ? "WELCOME! Account created." : "WELCOME BACK!", "ok");
    setMsg(msg, "OK", "ok");
  } catch (err) {
    setMsg(msg, err.message, "error");
    toast(err.message, "error");
  }
}

function onGuest() {
  if (sb) {
    sb.auth.signOut().catch(() => {});
  }
  token = "";
  localStorage.removeItem(tokenKey);
  me = null;
  renderUser();
  toast("Guest mode: browse events.", "ok");
}

function toRFC3339FromLocalInput(v) {
  const d = new Date(v);
  if (isNaN(d.getTime())) return "";
  return d.toISOString();
}

function parseTagsInput(v) {
  return String(v || "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
}

async function onCreateEvent() {
  const msg = $("createMsg");
  setMsg(msg, "", "");
  if (pinned.lat == null || pinned.lng == null) {
    setMsg(msg, "Pin a location on the map first.", "error");
    toast("Pin a location on the map first.", "error");
    return;
  }
  try {
    const startsAt = toRFC3339FromLocalInput($("evStart").value);
    const endsAt = toRFC3339FromLocalInput($("evEnd").value);
    await api("/api/events", {
      method: "POST",
      body: JSON.stringify({
        title: $("evTitle").value,
        description: $("evDesc").value,
        starts_at: startsAt,
        ends_at: endsAt,
        lat: pinned.lat,
        lng: pinned.lng,
        address: $("evAddress").value,
        tags: parseTagsInput($("evTags").value),
        checkin_radius_km: Number($("evCheckinRadiusKm").value || "0.2"),
      }),
    });
    setMsg(msg, "Event created.", "ok");
    await refreshMe();
    await refreshLeaderboards();
    await refreshEvents();
    toast("Event created! +50 XP", "ok");
  } catch (err) {
    setMsg(msg, err.message, "error");
    toast(err.message, "error");
  }
}

async function onLogout() {
  if (sb) {
    await sb.auth.signOut().catch(() => {});
  }
  token = "";
  localStorage.removeItem(tokenKey);
  me = null;
  renderUser();
  renderLeaderboards(null, $("globalLB"), "Sign in to view.");
  renderLeaderboards(null, $("eventLB"), "Select an event.");
  $("lbRefreshBtn").disabled = true;
  selectEvent("");
  toast("Logged out.", "ok");
}

function pickEventColor(e) {
  const tags = Array.isArray(e.tags) ? e.tags : [];
  if (tags.length) return colorFromString(tags[0]);
  return "#ffcf55";
}

function colorFromString(s) {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0;
  const hue = h % 360;
  return `hsl(${hue} 85% 70%)`;
}

function selectEvent(eventId) {
  selectedEventId = eventId || "";
  const e = allEvents.find((x) => x.id === selectedEventId) || null;
  $("selectedHint").textContent = e ? `Selected: ${e.title}` : "No event selected";
  renderEventDetail(e);
  render();
  refreshLeaderboards().catch(() => {});
}

function filterEvents(events) {
  const q = String($("searchInput").value || "").trim().toLowerCase();
  const tag = String($("tagFilter").value || "").trim().toLowerCase();
  return (events || []).filter((e) => {
    const tags = Array.isArray(e.tags) ? e.tags : [];
    if (tag && !tags.some((t) => String(t).toLowerCase() === tag)) return false;
    if (!q) return true;
    const hay = `${e.title || ""} ${e.description || ""} ${e.address || ""} ${(tags || []).join(" ")}`.toLowerCase();
    return hay.includes(q);
  });
}

function render() {
  const filtered = filterEvents(allEvents).slice();
  filtered.sort((a, b) => new Date(a.startsAt).getTime() - new Date(b.startsAt).getTime());
  renderEvents(filtered);
  addEventMarkers(filtered);
}

function renderEventDetail(e) {
  const title = $("eventTitle");
  const sub = $("eventSub");
  const focusBtn = $("eventFocusBtn");
  const joinBtn = $("eventJoinBtn");
  const checkinBtn = $("eventCheckinBtn");
  const tagBtn = $("eventTagBtn");
  const tagInput = $("eventTagInput");
  const tagsEl = $("eventTags");

  if (!e) {
    title.textContent = "Select an event";
    sub.textContent = "Tip: click a marker or an event card";
    focusBtn.disabled = true;
    joinBtn.disabled = true;
    checkinBtn.disabled = true;
    tagBtn.disabled = true;
    tagInput.value = "";
    tagsEl.innerHTML = "";
    $("eventDistance").textContent = "—";
    return;
  }

  title.textContent = e.title || "(untitled)";
  const addr = e.address ? ` • ${e.address}` : "";
  sub.textContent = `${new Date(e.startsAt).toLocaleString()}${addr}`;

  focusBtn.disabled = false;
  joinBtn.disabled = !me;
  checkinBtn.disabled = !me;
  tagBtn.disabled = !me;

  tagsEl.innerHTML = "";
  const tagList = Array.isArray(e.tags) ? e.tags : [];
  if (tagList.length) {
    for (const t of tagList) {
      const chip = document.createElement("span");
      chip.className = "tagChip";
      chip.textContent = t;
      const color = colorFromString(String(t));
      chip.style.borderColor = color;
      chip.style.background = `linear-gradient(180deg, rgba(255,255,255,0.16), rgba(0,0,0,0.10)), ${color}`;
      chip.onclick = () => {
        $("tagFilter").value = t;
        render();
      };
      tagsEl.appendChild(chip);
    }
  } else {
    const chip = document.createElement("span");
    chip.className = "tagChip muted";
    chip.textContent = "no tags";
    tagsEl.appendChild(chip);
  }

  focusBtn.onclick = () => map.setView([e.lat, e.lng], 14);
  joinBtn.onclick = async () => {
    try {
      await api(`/api/events/${encodeURIComponent(e.id)}/join`, { method: "POST", body: "{}" });
      await refreshMe();
      await refreshLeaderboards();
      toast("Joined! +10 XP", "ok");
    } catch (err) {
      toast(err.message, "error");
    }
  };
  checkinBtn.onclick = async () => {
    try {
      const ll = await setMyLocation();
      await api(`/api/events/${encodeURIComponent(e.id)}/checkin`, {
        method: "POST",
        body: JSON.stringify({ lat: ll.lat, lng: ll.lng }),
      });
      await refreshMe();
      await refreshLeaderboards();
      toast("Check-in! +30 XP", "ok");
    } catch (err) {
      toast(err.message === "forbidden" ? "Too far to check in." : err.message, "error");
    }
  };
  tagBtn.onclick = async () => {
    const t = String(tagInput.value || "").trim();
    if (!t) {
      toast("Enter a tag first.", "error");
      return;
    }
    try {
      const ll = await setMyLocation();
      await api(`/api/events/${encodeURIComponent(e.id)}/tag`, {
        method: "POST",
        body: JSON.stringify({ tag: t, lat: ll.lat, lng: ll.lng }),
      });
      tagInput.value = "";
      await refreshEvents();
      await refreshMe();
      await refreshLeaderboards();
      toast("Tagged! +5 XP", "ok");
    } catch (err) {
      toast(err.message === "forbidden" ? "Too far to tag." : err.message, "error");
    }
  };

  updateEventDistance();
}

function updateEventDistance() {
  const e = allEvents.find((x) => x.id === selectedEventId) || null;
  if (!e || !lastLocation) return;
  const km = haversineKm(lastLocation.lat, lastLocation.lng, e.lat, e.lng);
  $("eventDistance").textContent = `${km.toFixed(2)} km away`;
}

function haversineKm(lat1, lon1, lat2, lon2) {
  const R = 6371;
  const dLat = deg2rad(lat2 - lat1);
  const dLon = deg2rad(lon2 - lon1);
  const a =
    Math.sin(dLat / 2) * Math.sin(dLat / 2) +
    Math.cos(deg2rad(lat1)) * Math.cos(deg2rad(lat2)) * Math.sin(dLon / 2) * Math.sin(dLon / 2);
  const c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
  return R * c;
}

function deg2rad(d) {
  return (d * Math.PI) / 180;
}

function renderLeaderboards(entries, el, emptyText) {
  if (!Array.isArray(entries) || entries.length === 0) {
    el.textContent = emptyText || "—";
    el.classList.add("muted");
    return;
  }
  el.classList.remove("muted");
  el.innerHTML = "";
  entries.forEach((e, idx) => {
    const row = document.createElement("div");
    row.className = "lbRow";
    const left = document.createElement("div");
    left.className = "lbLeft";
    const rank = document.createElement("div");
    rank.className = "lbRank";
    rank.textContent = String(idx + 1);
    const name = document.createElement("div");
    name.className = "lbName";
    name.textContent = e.email || e.userId;
    left.appendChild(rank);
    left.appendChild(name);

    const right = document.createElement("div");
    right.className = "lbMeta";
    right.textContent = `${e.points} XP • Lv ${e.level}`;

    row.appendChild(left);
    row.appendChild(right);
    el.appendChild(row);
  });
}

async function refreshLeaderboards() {
  if (!me) return;
  const [global, event] = await Promise.all([
    api("/api/leaderboard?limit=10", { method: "GET" }).catch(() => null),
    selectedEventId ? api(`/api/events/${encodeURIComponent(selectedEventId)}/leaderboard?limit=10`, { method: "GET" }).catch(() => null) : null,
  ]);
  renderLeaderboards(global?.leaderboard || null, $("globalLB"), "No scores yet.");
  if (!selectedEventId) {
    renderLeaderboards(null, $("eventLB"), "Select an event.");
  } else if (!event) {
    renderLeaderboards(null, $("eventLB"), "Could not load.");
  } else {
    renderLeaderboards(event.leaderboard || [], $("eventLB"), "No scores yet.");
  }
}

function setView(view) {
  const v = view === "events" || view === "profile" ? view : "map";
  document.body.dataset.view = v;
  localStorage.setItem(viewKey, v);
  if (v !== "map") document.body.classList.remove("filtersOpen");

  document.querySelectorAll(".mobileNav .navBtn").forEach((btn) => {
    const isActive = btn.getAttribute("data-view") === v;
    if (isActive) btn.setAttribute("aria-current", "page");
    else btn.removeAttribute("aria-current");
  });

  if (v === "map" && map) {
    setTimeout(() => map.invalidateSize(), 80);
  }
}

function initMobileUi() {
  const filtersBtn = $("filtersToggleBtn");
  if (filtersBtn) {
    filtersBtn.onclick = () => {
      document.body.classList.toggle("filtersOpen");
      if (document.body.classList.contains("filtersOpen")) setView("map");
    };
  }

  document.querySelectorAll(".mobileNav .navBtn").forEach((btn) => {
    btn.onclick = () => setView(btn.getAttribute("data-view") || "map");
  });

  const saved = localStorage.getItem(viewKey) || "";
  setView(saved || "map");
}

async function main() {
  initMap();
  initMobileUi();

  const googleBtn = $("googleLoginBtn");
  if (googleBtn) googleBtn.onclick = onGoogleLogin;
  if (!CFG.supabaseUrl || !CFG.supabaseAnonKey) {
    const oauthWrap = $("oauthWrap");
    if (oauthWrap) oauthWrap.hidden = true;
  }

  const guestBtn = $("authGuestBtn");
  if (guestBtn) guestBtn.onclick = onGuest;

  if (!sb) {
    $("authModeLogin").onclick = () => setAuthMode("login");
    $("authModeRegister").onclick = () => setAuthMode("register");
    $("authSubmitBtn").onclick = onAuthSubmit;
  } else {
    const authTitle = $("authTitle");
    if (authTitle) authTitle.textContent = "LOGIN WITH GOOGLE";
  }

  $("logoutBtn").onclick = () => onLogout();
  $("createEventBtn").onclick = onCreateEvent;
  $("refreshBtn").onclick = refreshEvents;
  $("lbRefreshBtn").onclick = refreshLeaderboards;

  const refreshDebounced = debounce(() => refreshEvents().catch(() => {}), 700);
  map.on("moveend", refreshDebounced);
  $("radiusKm").addEventListener("input", refreshDebounced);

  $("locateBtn").onclick = async () => {
    try {
      await setMyLocation();
      await refreshEvents();
    } catch (err) {
      toast(err.message, "error");
    }
  };

  $("searchInput").oninput = render;
  $("tagFilter").oninput = render;

  $("eventTagInput").addEventListener("keydown", (e) => {
    if (e.key === "Enter") $("eventTagBtn").click();
  });
  if (!sb) {
    $("authEmail").addEventListener("keydown", (e) => {
      if (e.key === "Enter") $("authSubmitBtn").click();
    });
    $("authPassword").addEventListener("keydown", (e) => {
      if (e.key === "Enter") $("authSubmitBtn").click();
    });

    $("authEmail").value = localStorage.getItem(emailKey) || "";
    setAuthMode("login");
  }

  await initSupabaseAuth();
  await loadMe();
  await refreshEvents();
  await refreshLeaderboards().catch(() => {});
}

main();
