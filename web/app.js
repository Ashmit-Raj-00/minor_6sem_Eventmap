const API = "";
const tokenKey = "eventmap_token";

let token = localStorage.getItem(tokenKey) || "";
let me = null;

let map = null;
let myMarker = null;
let pinMarker = null;
let pinned = { lat: null, lng: null };
let eventMarkers = [];

function $(id) {
  return document.getElementById(id);
}

function setMsg(el, text, kind) {
  el.textContent = text || "";
  el.classList.remove("ok", "error");
  if (kind) el.classList.add(kind);
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
  map = L.map("map", { zoomControl: true }).setView([start.lat, start.lng], 12);
  L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
    maxZoom: 19,
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
  }).addTo(map);

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
}

function clearEventMarkers() {
  for (const m of eventMarkers) m.remove();
  eventMarkers = [];
}

function addEventMarkers(events) {
  clearEventMarkers();
  for (const e of events) {
    if (typeof e.lat !== "number" || typeof e.lng !== "number") continue;
    const m = L.marker([e.lat, e.lng]).addTo(map);
    m.bindPopup(`<strong>${escapeHtml(e.title)}</strong><br/><span>${escapeHtml(e.address || "")}</span>`);
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
  if (!me) {
    who.textContent = "Not signed in";
    logoutBtn.hidden = true;
    createWrap.hidden = true;
    return;
  }
  who.textContent = `${me.email} (${me.role})`;
  logoutBtn.hidden = false;
  createWrap.hidden = !(me.role === "organizer" || me.role === "admin");
}

function renderEvents(events) {
  const el = $("eventsList");
  el.innerHTML = "";
  for (const e of events) {
    const row = document.createElement("div");
    row.className = "eventRow";
    const title = document.createElement("div");
    title.className = "eventTitle";
    title.textContent = e.title;

    const meta = document.createElement("div");
    meta.className = "eventMeta";
    meta.innerHTML = `
      <span class="mono">id: ${escapeHtml(e.id)}</span>
      <span>${new Date(e.startsAt).toLocaleString()}</span>
      <span class="muted">${escapeHtml(e.address || "")}</span>
    `;

    const actions = document.createElement("div");
    actions.className = "eventActions";

    const focusBtn = document.createElement("button");
    focusBtn.className = "btn btn-quiet";
    focusBtn.textContent = "Focus";
    focusBtn.onclick = () => {
      map.setView([e.lat, e.lng], 14);
    };

    const joinBtn = document.createElement("button");
    joinBtn.className = "btn";
    joinBtn.textContent = "Join";
    joinBtn.onclick = async () => {
      try {
        await api(`/api/events/${encodeURIComponent(e.id)}/join`, { method: "POST", body: "{}" });
        alert("Joined!");
      } catch (err) {
        alert(err.message);
      }
    };

    actions.appendChild(focusBtn);
    actions.appendChild(joinBtn);

    row.appendChild(title);
    row.appendChild(meta);
    row.appendChild(actions);
    el.appendChild(row);
  }
}

async function refreshEvents() {
  const radiusKm = Number($("radiusKm").value || "0");
  let center = map.getCenter();
  let lat = center.lat;
  let lng = center.lng;

  try {
    const data = await api(`/api/events/nearby?lat=${lat}&lng=${lng}&radius_km=${radiusKm}`, { method: "GET" });
    renderEvents(data.events || []);
    addEventMarkers(data.events || []);
  } catch (err) {
    // fallback to list-all if unauth, etc.
    const data = await api(`/api/events`, { method: "GET" });
    renderEvents(data.events || []);
    addEventMarkers(data.events || []);
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

async function onLogin() {
  const msg = $("authMsg");
  setMsg(msg, "", "");
  try {
    const data = await api("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({
        email: $("loginEmail").value,
        password: $("loginPassword").value,
      }),
    });
    token = data.token;
    localStorage.setItem(tokenKey, token);
    me = data.user;
    renderUser();
    setMsg(msg, "Logged in.", "ok");
  } catch (err) {
    setMsg(msg, err.message, "error");
  }
}

async function onRegister() {
  const msg = $("authMsg");
  setMsg(msg, "", "");
  try {
    await api("/api/auth/register", {
      method: "POST",
      body: JSON.stringify({
        email: $("regEmail").value,
        password: $("regPassword").value,
        role: $("regRole").value,
      }),
    });
    setMsg(msg, "Account created. Now login.", "ok");
  } catch (err) {
    setMsg(msg, err.message, "error");
  }
}

function toRFC3339FromLocalInput(v) {
  const d = new Date(v);
  if (isNaN(d.getTime())) return "";
  return d.toISOString();
}

async function onCreateEvent() {
  const msg = $("createMsg");
  setMsg(msg, "", "");
  if (pinned.lat == null || pinned.lng == null) {
    setMsg(msg, "Click the map to pin a location first.", "error");
    return;
  }
  try {
    const startsAt = toRFC3339FromLocalInput($("evStart").value);
    const endsAt = toRFC3339FromLocalInput($("evEnd").value);
    const data = await api("/api/events", {
      method: "POST",
      body: JSON.stringify({
        title: $("evTitle").value,
        description: $("evDesc").value,
        starts_at: startsAt,
        ends_at: endsAt,
        lat: pinned.lat,
        lng: pinned.lng,
        address: $("evAddress").value,
      }),
    });
    setMsg(msg, `Created event ${data.id}`, "ok");
    await refreshEvents();
  } catch (err) {
    setMsg(msg, err.message, "error");
  }
}

function onLogout() {
  token = "";
  localStorage.removeItem(tokenKey);
  me = null;
  renderUser();
}

async function main() {
  initMap();

  $("loginBtn").onclick = onLogin;
  $("registerBtn").onclick = onRegister;
  $("logoutBtn").onclick = onLogout;
  $("createEventBtn").onclick = onCreateEvent;
  $("refreshBtn").onclick = refreshEvents;
  $("locateBtn").onclick = async () => {
    try {
      await setMyLocation();
      await refreshEvents();
    } catch (err) {
      alert(err.message);
    }
  };

  await loadMe();
  await refreshEvents();
}

main();

