const cfg = window.__EVENTMAP_CONFIG__ || { apiBase: "" };

const state = {
  token: localStorage.getItem("token") || "",
  user: null,
  stats: null,
  events: [],
  selectedEvent: null,
  tasks: [],
  chatSource: null,
  notifications: [],
};

const $ = (id) => document.getElementById(id);

function toast(msg, isError = false) {
  const el = $("toast");
  el.hidden = false;
  el.textContent = msg;
  el.style.borderColor = isError ? "rgba(255,95,122,0.5)" : "rgba(110,231,255,0.30)";
  setTimeout(() => (el.hidden = true), 2400);
}

function setMsg(id, msg, isError = false) {
  const el = $(id);
  el.textContent = msg || "";
  el.className = "msg" + (isError ? " err" : "");
}

async function api(path, opts = {}) {
  const headers = opts.headers ? { ...opts.headers } : {};
  if (!headers["Content-Type"] && !(opts.body instanceof FormData)) {
    headers["Content-Type"] = "application/json";
  }
  if (state.token) headers["Authorization"] = `Bearer ${state.token}`;
  const res = await fetch(cfg.apiBase + path, { ...opts, headers });
  const text = await res.text();
  let data = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = { raw: text };
  }
  if (!res.ok) {
    const err = new Error(data?.error || `HTTP_${res.status}`);
    err.status = res.status;
    err.data = data;
    throw err;
  }
  return data;
}

function requireAuthUI(enabled) {
  $("refreshMeBtn").disabled = !enabled;
  $("reloadEventsBtn").disabled = !enabled;
  $("createEventToggleBtn").disabled = !enabled;
  $("reloadTasksBtn").disabled = !enabled;
  $("createTaskToggleBtn").disabled = !enabled;
  $("joinEventBtn").disabled = !enabled || !state.selectedEvent;
  $("openDashboardBtn").disabled = !enabled || !state.selectedEvent;
  $("chatConnectBtn").disabled = !enabled || !state.selectedEvent;
  $("chatInput").disabled = !enabled || !state.selectedEvent;
  $("chatSendBtn").disabled = !enabled || !state.selectedEvent;
  $("openActivityBtn").disabled = !enabled;
  $("roleSelect").disabled = !enabled;
  $("switchRoleBtn").disabled = !enabled;
}

function renderUser() {
  if (!state.user) {
    $("userLine").textContent = "Not signed in";
    $("logoutBtn").hidden = true;
    $("roleSelect").value = "operator";
    $("xpLine").textContent = "—";
    $("lvlLine").textContent = "—";
    $("approvalLine").textContent = "—";
    requireAuthUI(false);
    return;
  }
  $("userLine").textContent = `${state.user.name} • ${state.user.email}`;
  $("logoutBtn").hidden = false;
  $("roleSelect").value = state.user.role;
  $("xpLine").textContent = String(state.user.xp ?? 0);
  $("lvlLine").textContent = String(state.stats?.level ?? "—");
  const ratio = state.stats?.approvalRatio ?? null;
  $("approvalLine").textContent = ratio === null ? "—" : `${Math.round(ratio * 100)}%`;
  requireAuthUI(true);
}

function renderEvents() {
  const list = $("eventsList");
  list.innerHTML = "";
  if (!state.events.length) {
    const d = document.createElement("div");
    d.className = "muted small";
    d.textContent = "No events yet.";
    list.appendChild(d);
    return;
  }
  for (const ev of state.events) {
    const item = document.createElement("div");
    item.className = "listItem";
    item.style.cursor = "pointer";
    item.innerHTML = `
      <div class="title">${escapeHtml(ev.title)}</div>
      <div class="pill"><b>${ev.status}</b> • ${ev.visibility} • ${new Date(ev.startsAt).toLocaleString()}</div>
    `;
    item.onclick = () => selectEvent(ev.id).catch((e) => toast(e?.data?.error || e.message, true));
    list.appendChild(item);
  }
}

function renderSelectedEvent() {
  const ev = state.selectedEvent;
  if (!ev) {
    $("eventTitle").textContent = "Select an event";
    $("eventMeta").textContent = "";
    $("tasksList").innerHTML = "";
    $("joinEventBtn").disabled = true;
    $("openDashboardBtn").disabled = true;
    $("chatConnectBtn").disabled = true;
    $("chatInput").disabled = true;
    $("chatSendBtn").disabled = true;
    return;
  }
  $("eventTitle").textContent = ev.title;
  $("eventMeta").textContent = `${ev.visibility} • ${ev.status} • Goal: ${ev.goalTarget ? `${ev.goalTarget} ${ev.goalUnit || ""}` : (ev.goal || "—")}`;
  $("joinEventBtn").disabled = !state.user;
  $("openDashboardBtn").disabled = !(state.user && state.user.role === "commander");
  $("createTaskToggleBtn").disabled = !(state.user && state.user.role === "commander");
  $("announceBtn").disabled = !(state.user && state.user.role === "commander");
}

function renderTasks() {
  const list = $("tasksList");
  list.innerHTML = "";
  if (!state.tasks.length) {
    const d = document.createElement("div");
    d.className = "muted small";
    d.textContent = "No tasks.";
    list.appendChild(d);
    return;
  }

  for (const t of state.tasks) {
    const item = document.createElement("div");
    item.className = "listItem";
    const meta = [
      `status=${t.status}`,
      `type=${t.type}`,
      `priority=${t.priority}`,
      `difficulty=${t.difficulty}`,
      t.assignedTo ? `assignedTo=${t.assignedTo}` : null,
      t.hasLocation ? "geo=required" : null,
    ].filter(Boolean).join(" • ");

    const actions = document.createElement("div");
    actions.className = "row";
    actions.style.marginTop = "10px";

    const btnStart = mkBtn("Start", "btn btn-secondary", async () => {
      await api(`/api/tasks/${t.id}/start`, { method: "POST", body: "{}" });
      await reloadTasks();
      toast("Task started");
    });

    const btnSubmit = mkBtn("Submit proof", "btn", async () => {
      await openSubmitFlow(t.id);
    });

    const btnProof = mkBtn("View proof", "btn btn-secondary", async () => {
      const sub = await api(`/api/tasks/${t.id}/latest-submission`, { method: "GET" });
      openProofViewer(sub);
    });

    const btnApprove = mkBtn("Approve", "btn", async () => {
      const feedback = prompt("Approval note (optional):") || "";
      const quality = Number(prompt("Quality 1-5 (optional):") || "0");
      await api(`/api/tasks/${t.id}/review`, { method: "POST", body: JSON.stringify({ action: "approve", feedback, quality }) });
      await reloadTasks();
      toast("Approved");
    });

    const btnReject = mkBtn("Reject", "btn btn-secondary", async () => {
      const feedback = prompt("Rejection feedback (required):") || "";
      if (!feedback.trim()) return toast("Feedback required", true);
      await api(`/api/tasks/${t.id}/review`, { method: "POST", body: JSON.stringify({ action: "reject", feedback, quality: 0 }) });
      await reloadTasks();
      toast("Rejected");
    });

    if (state.user) {
      if (t.status === "pending" || t.status === "rejected") actions.appendChild(btnStart);
      if (t.status === "in_progress") actions.appendChild(btnSubmit);
      if (t.status === "submitted") {
        actions.appendChild(btnProof);
        if (state.user.role === "commander") {
          actions.appendChild(btnApprove);
          actions.appendChild(btnReject);
        }
      }
    }

    item.innerHTML = `
      <div class="title">${escapeHtml(t.title)}</div>
      <div class="muted small">${escapeHtml(t.description || "")}</div>
      <div class="pill">${escapeHtml(meta)}</div>
      ${t.lastFeedback ? `<div class="small" style="margin-top:8px;color:var(--danger)">Feedback: ${escapeHtml(t.lastFeedback)}</div>` : ""}
    `;
    item.appendChild(actions);
    list.appendChild(item);
  }
}

function mkBtn(text, cls, onClick) {
  const b = document.createElement("button");
  b.textContent = text;
  b.className = cls;
  b.type = "button";
  b.onclick = async () => {
    try {
      b.disabled = true;
      await onClick();
    } catch (e) {
      toast(e?.data?.error || e.message, true);
    } finally {
      b.disabled = false;
    }
  };
  return b;
}

function escapeHtml(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#039;" }[c]));
}

async function refreshMe() {
  const data = await api("/api/me");
  state.user = data.user;
  state.stats = data.stats;
  renderUser();
}

async function reloadEvents() {
  const data = await api("/api/events");
  state.events = data.events || [];
  renderEvents();
}

async function selectEvent(eventId) {
  state.selectedEvent = await api(`/api/events/${eventId}`);
  $("dashboardBox").hidden = true;
  $("createTaskBox").hidden = true;
  renderSelectedEvent();
  try {
    await reloadTasks();
  } catch (e) {
    // Likely not joined yet; still show event details.
    state.tasks = [];
    renderTasks();
  }
  clearChat();
}

async function reloadTasks() {
  if (!state.selectedEvent) return;
  const data = await api(`/api/events/${state.selectedEvent.id}/tasks`);
  state.tasks = data.tasks || [];
  renderTasks();
}

async function joinSelectedEvent() {
  if (!state.selectedEvent) return;
  try {
    await api(`/api/events/${state.selectedEvent.id}/join`, { method: "POST", body: "{}" });
    toast("Joined event");
  } catch (e) {
    if (e.status === 409) toast("Already joined");
    else throw e;
  }
}

async function createEvent() {
  const tags = ($("evTags").value || "").split(",").map((s) => s.trim()).filter(Boolean);
  const payload = {
    title: $("evTitle").value,
    description: $("evDesc").value,
    goal: $("evGoal").value,
    goalTarget: Number($("evGoalTarget").value || "0"),
    goalUnit: $("evGoalUnit").value,
    instructions: $("evInstructions").value,
    visibility: $("evVisibility").value,
    startsAt: $("evStart").value,
    endsAt: $("evEnd").value,
    lat: Number($("evLat").value || "0"),
    lng: Number($("evLng").value || "0"),
    address: $("evAddress").value,
    tags,
  };
  const ev = await api("/api/events", { method: "POST", body: JSON.stringify(payload) });
  toast("Event created");
  $("createEventBox").hidden = true;
  await reloadEvents();
  await selectEvent(ev.id);
}

async function createTask() {
  if (!state.selectedEvent) return;
  const hasLocation = $("tkHasLocation").value === "true";
  const payload = {
    title: $("tkTitle").value,
    description: $("tkDesc").value,
    type: $("tkType").value,
    priority: $("tkPriority").value,
    difficulty: Number($("tkDifficulty").value || "2"),
    deadline: $("tkDeadline").value || "",
    assignedTo: $("tkAssignedTo").value || "",
    hasLocation,
    lat: Number($("tkLat").value || "0"),
    lng: Number($("tkLng").value || "0"),
  };
  await api(`/api/events/${state.selectedEvent.id}/tasks`, { method: "POST", body: JSON.stringify(payload) });
  toast("Task created");
  $("createTaskBox").hidden = true;
  await reloadTasks();
}

async function openSubmitFlow(taskId) {
  const file = await pickFile();
  if (!file) return;
  const comment = prompt("Comment (optional):") || "";

  let coords = null;
  if (confirm("Attach geo-location (recommended if task requires geo)?")) {
    coords = await getGeo();
  }

  const fd = new FormData();
  fd.append("image", file);
  fd.append("comment", comment);
  if (coords) {
    fd.append("lat", String(coords.lat));
    fd.append("lng", String(coords.lng));
  }
  await api(`/api/tasks/${taskId}/submit`, { method: "POST", body: fd, headers: {} });
  toast("Submitted");
  await reloadTasks();
}

function pickFile() {
  return new Promise((resolve) => {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = "image/*";
    input.onchange = () => resolve(input.files?.[0] || null);
    input.click();
  });
}

function getGeo() {
  return new Promise((resolve, reject) => {
    if (!navigator.geolocation) return resolve(null);
    navigator.geolocation.getCurrentPosition(
      (pos) => resolve({ lat: pos.coords.latitude, lng: pos.coords.longitude }),
      () => resolve(null),
      { enableHighAccuracy: true, timeout: 7000, maximumAge: 0 }
    );
  });
}

function openProofViewer(sub) {
  const url = sub.imageUrl || sub.imageURL || sub.image_url;
  const w = window.open("", "_blank");
  w.document.write(`<title>Proof</title><img style="max-width:100%;height:auto" src="${url}"/><pre>${escapeHtml(JSON.stringify(sub, null, 2))}</pre>`);
}

async function openDashboard() {
  if (!state.selectedEvent) return;
  $("dashboardBox").hidden = false;
  const d = await api(`/api/events/${state.selectedEvent.id}/dashboard`);
  const lines = [];
  lines.push(`Progress: ${d.progressPct}%`);
  lines.push(`Tasks: ${d.completedTasks}/${d.totalTasks} completed, ${d.pendingApprovals} pending approvals`);
  lines.push(`Participants: ${d.activeParticipants}`);
  const top = d.topContributors || [];
  if (top.length) {
    lines.push("Top contributors:");
    for (const c of top) lines.push(`- ${c.name || c.userId}: ${c.xp} XP`);
  }
  $("dashboardContent").textContent = lines.join("\n");
}

async function sendAnnouncement() {
  if (!state.selectedEvent) return;
  const body = $("announceBody").value;
  await api(`/api/events/${state.selectedEvent.id}/announce`, { method: "POST", body: JSON.stringify({ body }) });
  $("announceBody").value = "";
  toast("Announcement sent");
}

function clearChat() {
  if (state.chatSource) {
    state.chatSource.close();
    state.chatSource = null;
  }
  $("chatLog").innerHTML = "";
  $("chatInput").value = "";
}

function appendChat(msg) {
  const el = $("chatLog");
  const wrap = document.createElement("div");
  wrap.className = "chatMsg";
  wrap.innerHTML = `
    <div class="meta">${escapeHtml(msg.userId)} • ${new Date(msg.createdAt).toLocaleTimeString()}</div>
    <div class="body">${escapeHtml(msg.body)}</div>
  `;
  el.appendChild(wrap);
  el.scrollTop = el.scrollHeight;
}

async function connectChat() {
  if (!state.selectedEvent) return;
  clearChat();
  const history = await api(`/api/events/${state.selectedEvent.id}/chat?limit=50`);
  for (const m of history.messages || []) appendChat(m);
  const src = new EventSource(`/api/events/${state.selectedEvent.id}/chat/stream?token=${encodeURIComponent(state.token)}`);
  src.addEventListener("message", (ev) => {
    try { appendChat(JSON.parse(ev.data)); } catch {}
  });
  src.addEventListener("ping", () => {});
  src.onerror = () => {
    // Browser auto-reconnects; keep UI quiet.
  };
  state.chatSource = src;
  toast("Chat connected");
}

async function sendChat() {
  if (!state.selectedEvent) return;
  const body = $("chatInput").value.trim();
  if (!body) return;
  $("chatInput").value = "";
  await api(`/api/events/${state.selectedEvent.id}/chat`, { method: "POST", body: JSON.stringify({ body }) });
}

async function loadNotifications() {
  if (!state.user) return;
  const data = await api("/api/notifications?limit=50");
  state.notifications = data.notifications || [];
  const unread = state.notifications.filter((n) => !n.readAt).length;
  $("notifLine").textContent = String(unread);
}

async function openActivity() {
  $("activityBox").hidden = false;
  const data = await api("/api/me/activity?limit=50");
  const list = $("activityList");
  list.innerHTML = "";
  for (const x of data.xpLogs || []) {
    const item = document.createElement("div");
    item.className = "listItem";
    item.innerHTML = `
      <div class="title">+${x.amount} XP • ${escapeHtml(x.reason)}</div>
      <div class="muted small">${new Date(x.createdAt).toLocaleString()} • task=${escapeHtml(x.taskId || "—")}</div>
    `;
    list.appendChild(item);
  }
}

function closeActivity() {
  $("activityBox").hidden = true;
}

function onLogout() {
  state.token = "";
  state.user = null;
  state.stats = null;
  state.events = [];
  state.selectedEvent = null;
  state.tasks = [];
  localStorage.removeItem("token");
  clearChat();
  renderUser();
  renderEvents();
  renderSelectedEvent();
}

async function devLogin() {
  setMsg("authMsg", "");
  const name = $("devName").value;
  const email = $("devEmail").value;
  const data = await api("/api/auth/dev", { method: "POST", body: JSON.stringify({ name, email }) });
  state.token = data.token;
  localStorage.setItem("token", state.token);
  await refreshMe();
  await reloadEvents();
  await loadNotifications();
  toast("Signed in");
}

async function switchRole() {
  const role = $("roleSelect").value;
  const data = await api("/api/me/role", { method: "POST", body: JSON.stringify({ role }) });
  state.token = data.token;
  localStorage.setItem("token", state.token);
  state.user = data.user;
  await refreshMe();
  renderSelectedEvent();
  toast("Role updated");
}

function wire() {
  $("devLoginBtn").onclick = () => devLogin().catch((e) => setMsg("authMsg", e?.data?.error || e.message, true));
  $("refreshMeBtn").onclick = () => refreshMe().then(loadNotifications).catch((e) => toast(e?.data?.error || e.message, true));
  $("logoutBtn").onclick = onLogout;
  $("switchRoleBtn").onclick = () => switchRole().catch((e) => toast(e?.data?.error || e.message, true));

  $("reloadEventsBtn").onclick = () => reloadEvents().catch((e) => toast(e?.data?.error || e.message, true));
  $("createEventToggleBtn").onclick = () => ($("createEventBox").hidden = !$("createEventBox").hidden);
  $("createEventBtn").onclick = () => createEvent().catch((e) => setMsg("createEventMsg", e?.data?.error || e.message, true));

  $("joinEventBtn").onclick = () => joinSelectedEvent().catch((e) => toast(e?.data?.error || e.message, true));
  $("reloadTasksBtn").onclick = () => reloadTasks().catch((e) => toast(e?.data?.error || e.message, true));
  $("createTaskToggleBtn").onclick = () => ($("createTaskBox").hidden = !$("createTaskBox").hidden);
  $("createTaskBtn").onclick = () => createTask().catch((e) => setMsg("createTaskMsg", e?.data?.error || e.message, true));

  $("openDashboardBtn").onclick = () => openDashboard().catch((e) => toast(e?.data?.error || e.message, true));
  $("announceBtn").onclick = () => ($("announceBox").hidden = !$("announceBox").hidden);
  $("sendAnnounceBtn").onclick = () => sendAnnouncement().catch((e) => setMsg("announceMsg", e?.data?.error || e.message, true));

  $("chatConnectBtn").onclick = () => connectChat().catch((e) => toast(e?.data?.error || e.message, true));
  $("chatSendBtn").onclick = () => sendChat().catch((e) => toast(e?.data?.error || e.message, true));
  $("chatInput").addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") sendChat().catch(() => {});
  });

  $("openActivityBtn").onclick = () => openActivity().catch((e) => toast(e?.data?.error || e.message, true));
  $("closeActivityBtn").onclick = closeActivity;
}

async function bootstrap() {
  wire();
  renderUser();
  renderEvents();
  renderSelectedEvent();

  if (!state.token) return;
  try {
    await refreshMe();
    await reloadEvents();
    await loadNotifications();
  } catch {
    onLogout();
  }
}

bootstrap();
