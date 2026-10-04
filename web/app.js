/* 舞会音乐播放器 前端逻辑（无框架、无外部依赖） */
"use strict";

const $ = (sel) => document.querySelector(sel);
const audio = $("#audio");

let state = null;          // 服务器状态（settings/categories/playlist/playlists/durations）
let curIdx = -1;           // 当前播放的歌单序号
let selectedIdx = -1;      // 单击选中的序号
let expandedCats = new Set();
let baseVol = 0.9;         // 音量滑块 0~1
let consecutiveErrors = 0;

/* ---------- 播放控制状态 ----------
   限时/渐弱由 timeupdate 驱动：每次回调读取最新设置，变更即时生效。 */
const pb = {
  fadeInterval: null,
  gapTicker: null,
  fading: false,
};

function clearPbTimers() {
  [pb.fadeInterval, pb.gapTicker].forEach((t) => { if (t) { clearTimeout(t); clearInterval(t); } });
  pb.fadeInterval = pb.gapTicker = null;
  pb.fading = false;
}

/* ---------- 工具 ---------- */
function fmtTime(sec) {
  if (!isFinite(sec) || sec < 0) return "--:--";
  sec = Math.round(sec);
  const m = Math.floor(sec / 60), s = sec % 60;
  return `${m}:${String(s).padStart(2, "0")}`;
}

let toastTimer = null;
function toast(msg, isErr = false) {
  const el = $("#toast");
  el.textContent = msg;
  el.className = isErr ? "err" : "";
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => el.classList.add("hidden"), isErr ? 5000 : 2600);
}

function audioUrl(path) { return "/audio?p=" + encodeURIComponent(path); }

async function api(path, body, method = "POST") {
  const opt = body instanceof FormData
    ? { method, body }
    : { method, headers: { "Content-Type": "application/json" }, body: body ? JSON.stringify(body) : undefined };
  const res = await fetch(path, opt);
  if (!res.ok) {
    let msg = `HTTP ${res.status}`;
    try { msg = (await res.json()).error || msg; } catch (_) {}
    throw new Error(msg);
  }
  const data = await res.json();
  if (data && data.ok === false) throw new Error(data.error || "操作失败");
  if (data && data.state) applyState(data.state);
  return data;
}

/* ---------- 状态 ---------- */
function applyState(s) {
  state = s;
  if (!s.music_root) {
    $("#setup").classList.remove("hidden");
  } else {
    $("#setup").classList.add("hidden");
    $("#libinfo-text").textContent = "曲库：" + s.music_root;
  }
  const lan = (s.lan_urls && s.lan_urls.length) || 0;
  $("#btn-qr").classList.toggle("hidden", lan === 0);
  if (lan) {
    $("#qr-img").src = "/qr?text=" + encodeURIComponent(s.lan_urls[0]) + "&scale=8";
    $("#qr-urls").innerHTML = s.lan_urls.map((u) => escapeHtml(u)).join("<br>");
  }
  renderStrips();
  renderCategories();
  renderPlaylist();
  renderSavedPlaylists();
  if (document.body.dataset.view === "comp") renderCompPage();
}

async function refresh() {
  try {
    const res = await fetch("/api/state");
    const data = await res.json();
    applyState(data.state || data);
  } catch (e) {
    toast("无法连接播放器服务：" + e.message, true);
  }
}

/* ---------- 原生目录选择（桌面弹系统对话框，Android 走 SAF） ---------- */
async function applyMusicRoot(path) {
  try {
    await api("/api/library/root", { root: path });
    toast("音乐库已设置：" + path);
  } catch (e) { toast(e.message, true); }
}

async function pickDirectory() {
  if (window.NativeShell && window.NativeShell.isAndroid) {
    window.__onNativeFolder = (res) => {
      if (res && res.path) applyMusicRoot(res.path);
      else if (res && res.reason === "storage")
        toast("请先在系统设置中允许本应用「所有文件访问」，然后重新点「选择目录」", true);
      else if (res && res.reason === "unavailable")
        toast("本设备无法打开目录选择器，请手动输入路径", true);
      else toast("未选择目录", true);
    };
    window.NativeShell.pickFolder();
    return;
  }
  try {
    const res = await fetch("/api/library/pick-dir", { method: "POST" });
    const data = await res.json();
    if (data.ok && data.path) applyMusicRoot(data.path);
    else if (data.error && data.error !== "已取消选择") toast(data.error, true);
  } catch (e) { toast(e.message, true); }
}
$("#setup-pick").onclick = pickDirectory;

/* ---------- 顶部按钮 ---------- */
$("#btn-rescan").onclick = async () => {
  try { await api("/api/library/rescan", {}); toast("音乐库已重新扫描"); }
  catch (e) { toast(e.message, true); }
};
$("#btn-qr").onclick = () => $("#dlg-qr").classList.remove("hidden");
$("#btn-libinfo").onclick = () => {
  $("#setup-root").value = state ? state.music_root || "" : "";
  $("#setup").classList.remove("hidden");
};
$("#btn-quit").onclick = async () => {
  if (!confirm("退出播放器程序？浏览器页面也会随之失效。")) return;
  try { await fetch("/api/shutdown", { method: "POST" }); } catch (_) {}
  document.body.innerHTML = '<div style="margin:auto;text-align:center;color:#8b91a3">程序已退出，可关闭此页面。</div>';
};
$("#setup-ok").onclick = async () => {
  const root = $("#setup-root").value.trim();
  if (!root) { toast("请输入音乐库目录路径", true); return; }
  try { await api("/api/library/root", { root }); toast("音乐库已设置"); }
  catch (e) { toast(e.message, true); }
};
$("#setup-root").addEventListener("keydown", (e) => { if (e.key === "Enter") $("#setup-ok").click(); });

document.querySelectorAll("[data-close]").forEach((btn) => {
  btn.onclick = () => btn.closest(".overlay").classList.add("hidden");
});
document.querySelectorAll(".overlay").forEach((ov) => {
  ov.addEventListener("mousedown", (e) => {
    if (e.target === ov && ov.id !== "setup") ov.classList.add("hidden");
  });
});

/* ---------- 页面视图切换（播放 / 歌单管理 / 点播 / 帮助） ---------- */
const PAGES = ["playlists", "comp", "help"];
function setView(v) {
  document.body.dataset.view = v;
  for (const p of PAGES) {
    $(`#page-${p}`).classList.toggle("hidden", v !== p);
  }
  if (v === "comp") renderCompPage();
  if (v === "playlists") renderSavedPlaylists();
  if (v === "help") renderHelp();
}
$("#btn-playlists").onclick = () => setView("playlists");
$("#btn-pl-back").onclick = () => setView("main");
$("#btn-comp").onclick = () => setView("comp");
$("#btn-comp-back").onclick = () => setView("main");
$("#btn-help").onclick = () => setView("help");
$("#btn-help-back").onclick = () => setView("main");

/* ---------- 播放设置条（歌单 / 点播独立，步进即改即存） ---------- */
function curSettings() {
  return comp.active ? state.settings.comp : state.settings.party;
}

function fmtLimit(sec) { return sec > 0 ? sec + " 秒" : "完整播放"; }

function renderStrips() {
  if (!state) return;
  const p = state.settings.party, c = state.settings.comp;
  $("#strip-party-limit").textContent = fmtLimit(p.play_limit_sec);
  $("#strip-party-fade").textContent = p.fadeout_sec + " 秒";
  $("#strip-party-gap").textContent = p.gap_sec + " 秒";
  $("#strip-comp-limit").textContent = fmtLimit(c.play_limit_sec);
  $("#strip-comp-fade").textContent = c.fadeout_sec + " 秒";
  $("#strip-comp-songgap").textContent = c.song_gap_sec + " 秒";
  $("#strip-comp-genregap").textContent = c.genre_gap_sec + " 秒";
}

let settingsSaveTimer = null;
function updateSettings(mut) {
  if (!state) return;
  mut(state.settings);
  renderStrips();
  clearTimeout(settingsSaveTimer);
  settingsSaveTimer = setTimeout(async () => {
    try { await api("/api/settings", state.settings); }
    catch (e) { toast("设置保存失败：" + e.message, true); }
  }, 350);
}

document.querySelectorAll(".ctrl-strip .step").forEach((b) => {
  b.addEventListener("click", () => {
    const g = b.dataset.g, f = b.dataset.f, d = parseInt(b.dataset.d);
    updateSettings((s) => {
      const grp = g === "party" ? s.party : s.comp;
      const cap = f === "play_limit_sec" ? 3600 : 600;
      grp[f] = Math.max(0, Math.min(cap, (grp[f] || 0) + d));
      if (grp.FadeoutSec > 0 && grp.PlayLimitSec > 0 && grp.FadeoutSec >= grp.PlayLimitSec) {
        grp.FadeoutSec = grp.PlayLimitSec - 1;
      }
    });
  });
});

/* ---------- 移动端标签页 ---------- */
function setMobileTab(name) {
  document.body.dataset.tab = name;
  document.querySelectorAll("#mobile-tabs button").forEach((b) => {
    b.classList.toggle("active", b.dataset.tab === name);
  });
}
document.querySelectorAll("#mobile-tabs button").forEach((b) => {
  b.addEventListener("click", () => setMobileTab(b.dataset.tab));
});
if (window.matchMedia("(max-width: 768px)").matches) {
  setMobileTab("pl");
}

/* ---------- 侧栏：舞种与生成（按分组展示） ---------- */
const GENRE_GROUPS = ["摩登", "拉丁", "交谊舞", "其它"];

function catLabel(c) { return c.name || "根目录"; }

function renderCategories() {
  const box = $("#cat-list");
  box.innerHTML = "";
  if (!state || !state.music_root) return;
  const savedCounts = JSON.parse(localStorage.getItem("pp_counts") || "{}");
  let lastGroup = null;
  for (const cat of state.categories) {
    if (cat.group !== lastGroup) {
      lastGroup = cat.group;
      const h = document.createElement("div");
      h.className = "group-title";
      h.textContent = cat.group;
      box.appendChild(h);
    }
    const name = catLabel(cat);
    const row = document.createElement("div");
    row.className = "cat-row";
    const toggle = document.createElement("button");
    toggle.className = "cat-toggle";
    toggle.textContent = expandedCats.has(name) ? "▾" : "▸";
    toggle.title = "展开曲目";
    toggle.onclick = () => {
      expandedCats.has(name) ? expandedCats.delete(name) : expandedCats.add(name);
      renderCategories();
    };
    const nm = document.createElement("span");
    nm.className = "cname";
    nm.textContent = name;
    nm.title = name;
    const cnt = document.createElement("span");
    cnt.className = "ccount";
    cnt.textContent = cat.tracks.length + " 首";
    const num = document.createElement("input");
    num.type = "number";
    num.min = "0";
    num.max = "99";
    num.value = savedCounts[name] ?? 1;
    num.dataset.cat = name;
    num.addEventListener("change", saveCounts);
    row.append(toggle, nm, cnt, num);
    box.appendChild(row);

    if (expandedCats.has(name)) {
      const list = document.createElement("div");
      list.className = "cat-tracks";
      const maxShow = 80;
      cat.tracks.slice(0, maxShow).forEach((t) => {
        const trk = document.createElement("div");
        trk.className = "trk";
        const tn = document.createElement("span");
        tn.className = "tname";
        tn.textContent = t.title;
        tn.title = t.title;
        const add = document.createElement("button");
        add.textContent = "加入";
        add.onclick = async () => {
          try { await api("/api/playlist/add", { path: t.path }); toast("已加入歌单：" + t.title); }
          catch (e) { toast(e.message, true); }
        };
        trk.append(tn, add);
        list.appendChild(trk);
      });
      if (cat.tracks.length > maxShow) {
        const more = document.createElement("div");
        more.className = "muted";
        more.style.padding = "2px 6px";
        more.textContent = `… 共 ${cat.tracks.length} 首`;
        list.appendChild(more);
      }
      box.appendChild(list);
    }
  }
}

function saveCounts() {
  const counts = {};
  document.querySelectorAll("#cat-list input[data-cat]").forEach((n) => {
    counts[n.dataset.cat] = parseInt(n.value) || 0;
  });
  localStorage.setItem("pp_counts", JSON.stringify(counts));
}

$("#btn-generate").onclick = async () => {
  const counts = {};
  let total = 0;
  document.querySelectorAll("#cat-list input[data-cat]").forEach((n) => {
    const v = parseInt(n.value) || 0;
    counts[n.dataset.cat] = v;
    total += v;
  });
  if (total === 0) { toast("请先为至少一个舞种设置数量", true); return; }
  const separate = document.querySelector('input[name="genorder"]:checked').value === "separate";
  try {
    await api("/api/playlist/generate", { counts, separate });
    stopPlayback(true);
    toast(`已生成 ${total} 首歌单`);
  } catch (e) { toast(e.message, true); }
};

$("#btn-shuffle").onclick = async () => {
  try { await api("/api/playlist/shuffle", {}); }
  catch (e) { toast(e.message, true); }
};
$("#btn-clear").onclick = async () => {
  try { await api("/api/playlist/clear", {}); stopPlayback(true); }
  catch (e) { toast(e.message, true); }
};

/* ---------- 歌单列表 ---------- */
function renderPlaylist() {
  const list = $("#pl-list");
  list.innerHTML = "";
  const pl = comp.active ? [] : (state ? state.playlist : []); // 点播进行中时保留歌单数据不清显示
  $("#pl-empty").style.display = (state && state.playlist && state.playlist.length) ? "none" : "";
  let known = 0, totalSec = 0;
  (state.playlist || []).forEach((t, i) => {
    const d = state.durations[t.path];
    if (d) { known++; totalSec += d; }
    const row = document.createElement("div");
    row.className = "pl-row" +
      (!comp.active && i === curIdx ? " playing" : "") +
      (!comp.active && i === selectedIdx ? " selected" : "") +
      (t.missing ? " missing" : "");
    row.innerHTML = `
      <div class="pl-num mono">${i + 1}</div>
      <div class="pl-main">
        <div class="pl-title" title="${escapeHtml(t.title)}（双击播放）">${escapeHtml(t.title)}${t.missing ? "（缺失）" : ""}</div>
        <div class="pl-sub">
          <span class="pl-cat-badge">${escapeHtml(t.category || "-")}</span>
          <span class="pl-dur mono" data-dur="${i}">${d ? fmtTime(d) : "--:--"}</span>
        </div>
      </div>
      <div class="pl-ops">
        <button data-act="up" title="上移">↑</button>
        <button data-act="down" title="下移">↓</button>
        <button data-act="del" class="danger" title="移除">✕</button>
      </div>`;
    row.addEventListener("click", () => { selectedIdx = i; renderPlaylist(); });
    row.addEventListener("dblclick", () => playIndex(i));
    row.querySelector('[data-act="up"]').onclick = (e) => { e.stopPropagation(); moveTrack(i, "up"); };
    row.querySelector('[data-act="down"]').onclick = (e) => { e.stopPropagation(); moveTrack(i, "down"); };
    row.querySelector('[data-act="del"]').onclick = (e) => { e.stopPropagation(); removeTrack(i); };
    list.appendChild(row);
  });
  const parts = [`${pl.length || (state.playlist || []).length} 首`];
  if (known > 0) parts.push(`已加载时长 ${fmtTime(totalSec)}` + (known < pl.length ? "+" : ""));
  $("#pl-summary").textContent = "（" + parts.join(" · ") + "）";
  if (pl.length) queueDurationProbe();
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

async function moveTrack(i, dir) {
  try { await api("/api/playlist/move", { index: i, dir }); if (curIdx === i) curIdx = dir === "up" ? i - 1 : i + 1; }
  catch (e) { toast(e.message, true); }
}
async function removeTrack(i) {
  try {
    await api("/api/playlist/remove", { index: i });
    if (curIdx === i) stopPlayback(false);
    else if (curIdx > i) curIdx--;
    if (selectedIdx === i) selectedIdx = -1;
    else if (selectedIdx > i) selectedIdx--;
  } catch (e) { toast(e.message, true); }
}

/* ---------- 时长探测（浏览器加载元数据，缓存到服务端） ---------- */
let probeQueue = [];
let probing = false;
function queueDurationProbe() {
  probeQueue = (state.playlist || [])
    .filter((t) => !state.durations[t.path] && !t.missing)
    .map((t) => t.path);
  if (!probing) probeNext();
}
function probeNext() {
  probing = false;
  if (!probeQueue.length) return;
  probing = true;
  const path = probeQueue.shift();
  const a = new Audio();
  a.preload = "metadata";
  const done = (sec) => {
    a.src = "";
    if (sec > 0) reportDuration(path, sec);
    setTimeout(probeNext, 30);
  };
  a.onloadedmetadata = () => done(a.duration);
  a.onerror = () => done(0);
  a.src = audioUrl(path);
}
let pendingReports = [];
let reportTimer = null;
function reportDuration(path, sec) {
  if (state) state.durations[path] = sec;
  renderPlaylistDurations();
  pendingReports.push({ path, seconds: sec });
  clearTimeout(reportTimer);
  reportTimer = setTimeout(flushDurations, 800);
}
async function flushDurations() {
  const items = pendingReports; pendingReports = [];
  if (!items.length) return;
  try { await fetch("/api/durations", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ items }) }); } catch (_) {}
  if (state && !$("#dlg-poster").classList.contains("hidden")) renderPoster();
}
function renderPlaylistDurations() {
  document.querySelectorAll("#pl-list [data-dur]").forEach((td) => {
    const i = parseInt(td.dataset.dur);
    const t = (state.playlist || [])[i];
    const d = t && state.durations[t.path];
    if (d) td.textContent = fmtTime(d);
  });
}

/* ---------- 播放器 ---------- */
function playIndex(i) {
  const pl = state.playlist || [];
  if (i < 0 || i >= pl.length) return;
  if (comp.active) compStop(false); // 与点播互斥
  const t = pl[i];
  if (t.missing) { toast("文件缺失，跳过：" + t.title, true); return; }
  clearPbTimers();
  curIdx = i;
  consecutiveErrors = 0;
  audio.src = audioUrl(t.path);
  applyVolume();
  audio.play().catch(() => toast("播放失败（浏览器可能阻止了自动播放），请手动点击播放", true));
  setPlayIcon(true);
  renderPlaylist();
  renderNow();
}

function currentLimitSec() {
  const s = curSettings();
  if (s.play_limit_sec > 0) return s.play_limit_sec;
  const dur = isFinite(audio.duration) ? audio.duration : 0;
  return dur > 0 ? dur : 0;
}

function startFade() {
  const s = curSettings();
  const fadeMs = Math.max(300, s.fadeout_sec * 1000);
  if (pb.fading) return;
  pb.fading = true;
  const base = audio.volume;
  const t0 = performance.now();
  pb.fadeInterval = setInterval(() => {
    const k = 1 - (performance.now() - t0) / fadeMs;
    audio.volume = Math.max(0, Math.min(1, base * k));
    if (k <= 0) clearInterval(pb.fadeInterval);
  }, 60);
  updateNowSub();
}

/* 曲目结束分发：点播模式与舞会歌单各自的推进逻辑 */
function handleTrackEnd(reason) {
  if (comp.active) compAdvance(reason);
  else endOfTrack(reason);
}

function endOfTrack(reason) {
  clearPbTimers();
  audio.pause();
  const gap = state.settings.party.gap_sec;
  const nextIdx = curIdx + 1;
  const hasNext = nextIdx < (state.playlist || []).length;
  if (!hasNext) {
    stopPlayback(true);
    toast(reason ? `${reason}，歌单播放完毕` : "歌单播放完毕");
    return;
  }
  gapThen(gap, `${reason ? reason + "，" : ""}`, () => {
    if (curIdx >= 0) playIndex(curIdx + 1);
  });
  // 间隔倒计时文案
  const nextT = state.playlist[nextIdx];
  const deadline = Date.now() + gap * 1000;
  const tick = () => {
    const left = Math.max(0, (deadline - Date.now()) / 1000);
    $("#now-sub").innerHTML = `<span class="fading">${reason ? reason + "，" : ""}${left.toFixed(0)} 秒后：下一首</span>`;
    if (left <= 0) { clearInterval(pb.gapTicker); }
  };
  if (gap > 0) { pb.gapTicker = setInterval(tick, 200); tick(); } else { playIndex(nextIdx); }
  void nextT;
}

function stopPlayback(resetIdx) {
  clearPbTimers();
  audio.pause();
  if (resetIdx) curIdx = -1;
  setPlayIcon(false);
  $("#now-title").textContent = "未在播放";
  $("#now-sub").textContent = "";
  $("#seek").value = 0;
  $("#t-cur").textContent = "0:00";
  $("#t-total").textContent = "0:00";
  renderPlaylist();
}

function togglePlay() {
  if (comp.active) {
    if (!audio.src) { compRun(); return; }
    if (audio.paused) {
      audio.play().then(() => { setPlayIcon(true); }).catch((e) => toast(e.message, true));
    } else {
      audio.pause();
      clearPbTimers();
      setPlayIcon(false);
      $("#now-sub").innerHTML = '<span class="fading">已暂停</span>';
    }
    return;
  }
  const pl = state.playlist || [];
  if (!pl.length) { toast("歌单为空，请先生成或载入歌单", true); return; }
  if (!audio.src) { playIndex(Math.max(0, curIdx < 0 ? 0 : curIdx)); return; }
  if (audio.paused) {
    audio.play().then(() => { setPlayIcon(true); }).catch((e) => toast(e.message, true));
  } else {
    audio.pause();
    clearPbTimers();
    setPlayIcon(false);
    $("#now-sub").innerHTML = '<span class="fading">已暂停</span>';
  }
}

function renderNow() {
  const t = comp.active ? compCurrentTrack() : (state.playlist || [])[curIdx];
  if (!t) return;
  $("#now-title").textContent = comp.active ? `点播 · ${t.category} · ${t.title}` : t.title;
  updateNowSub();
}

function updateNowSub() {
  const t = comp.active ? compCurrentTrack() : (state.playlist || [])[curIdx];
  if (!t) return;
  const s = curSettings();
  if (pb.fading) {
    $("#now-sub").innerHTML = `<span class="fading">${escapeHtml(t.category || "")} · 渐弱中…</span>`;
    return;
  }
  const limit = currentLimitSec();
  let leftTxt;
  if (s.play_limit_sec > 0) {
    leftTxt = `限时 ${s.play_limit_sec}s · 剩余 ${fmtTime(Math.max(0, limit - audio.currentTime))}`;
  } else {
    leftTxt = escapeHtml(t.category || "");
  }
  $("#now-sub").textContent = leftTxt;
}

$("#btn-play").onclick = togglePlay;
$("#btn-prev").onclick = () => {
  if (comp.active) return;
  if (curIdx > 0) playIndex(curIdx - 1);
};
$("#btn-next").onclick = () => {
  if (comp.active) { handleTrackEnd("跳过"); return; }
  if (curIdx < (state.playlist || []).length - 1) playIndex(curIdx + 1); else endOfTrack();
};

audio.addEventListener("timeupdate", () => {
  if (isFinite(audio.duration) && audio.duration > 0 && !seekDragging) {
    $("#seek").value = Math.round((audio.currentTime / audio.duration) * 1000);
  }
  $("#t-cur").textContent = fmtTime(audio.currentTime);
  $("#t-total").textContent = fmtTime(audio.duration);
  if (audio.paused) return;
  if (curIdx < 0 && !comp.active) return;
  const s = curSettings();
  const limit = currentLimitSec();
  if (limit > 0) {
    if (s.fadeout_sec > 0 && !pb.fading && audio.currentTime >= limit - s.fadeout_sec) {
      startFade();
    }
    if (audio.currentTime >= limit) {
      handleTrackEnd("限时到");
      return;
    }
  }
  updateNowSub();
});
audio.addEventListener("loadedmetadata", () => {
  const t = comp.active ? compCurrentTrack() : (state.playlist || [])[curIdx];
  if (t && isFinite(audio.duration) && audio.duration > 0.5) reportDuration(t.path, audio.duration);
});
audio.addEventListener("ended", () => { if (!pb.fading) handleTrackEnd(); });
audio.addEventListener("error", () => {
  if (!audio.src) return;
  consecutiveErrors++;
  const t = comp.active ? compCurrentTrack() : (state.playlist || [])[curIdx];
  toast(`无法播放：${t ? t.title : ""}（格式可能不受浏览器支持）`, true);
  if (!comp.active && consecutiveErrors < (state.playlist || []).length && curIdx + 1 < (state.playlist || []).length) {
    playIndex(curIdx + 1);
  } else if (!comp.active) {
    stopPlayback(false);
  }
});

let seekDragging = false;
$("#seek").addEventListener("input", () => { seekDragging = true; });
$("#seek").addEventListener("change", () => {
  if (isFinite(audio.duration)) audio.currentTime = ($("#seek").value / 1000) * audio.duration;
  seekDragging = false;
});

const volSlider = $("#volume");
volSlider.value = Math.round((parseFloat(localStorage.getItem("pp_volume")) || 0.9) * 100);
baseVol = volSlider.value / 100;
volSlider.addEventListener("input", () => {
  baseVol = volSlider.value / 100;
  localStorage.setItem("pp_volume", String(baseVol));
  if (!pb.fading) applyVolume();
});
function applyVolume() { audio.volume = Math.max(0, Math.min(1, baseVol)); }

function setPlayIcon(playing) {
  $("#ic-play").classList.toggle("hidden", playing);
  $("#ic-pause").classList.toggle("hidden", !playing);
}

/* ---------- 键盘快捷键 ---------- */
document.addEventListener("keydown", (e) => {
  const tag = document.activeElement && document.activeElement.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
  if (e.code === "Space") { e.preventDefault(); togglePlay(); }
  else if (e.code === "ArrowRight" && audio.src) { audio.currentTime = Math.min(audio.duration || 0, audio.currentTime + 5); }
  else if (e.code === "ArrowLeft" && audio.src) { audio.currentTime = Math.max(0, audio.currentTime - 5); }
  else if (e.code === "PageDown") { $("#btn-next").click(); }
  else if (e.code === "PageUp") { $("#btn-prev").click(); }
});

/* ---------- 添加乐曲（挂载曲库的文件树） ---------- */
$("#btn-add").onclick = () => {
  if (!state.music_root) { toast("请先设置音乐库", true); return; }
  $("#add-mount").textContent = state.music_root;
  $("#dlg-add").classList.remove("hidden");
  loadTreeLevel(state.music_root, $("#add-tree"));
};

async function loadTreeLevel(dir, container) {
  try {
    const res = await fetch("/api/library/tree?dir=" + encodeURIComponent(dir));
    const data = await res.json();
    container.innerHTML = "";
    if (!data.ok) {
      container.innerHTML = `<div class="muted small">${escapeHtml(data.error || "加载失败")}</div>`;
      return;
    }
    const v = data.dir;
    for (const name of v.dirs || []) {
      const det = document.createElement("details");
      det.className = "tree-dir";
      const sum = document.createElement("summary");
      sum.textContent = name;
      det.appendChild(sum);
      const child = document.createElement("div");
      child.className = "tree-children";
      det.appendChild(child);
      let loaded = false;
      det.addEventListener("toggle", () => {
        if (det.open && !loaded) {
          loaded = true;
          loadTreeLevel(joinPath(v.path, name), child);
        }
      });
      container.appendChild(det);
    }
    for (const f of v.files || []) {
      const row = document.createElement("div");
      row.className = "tree-file";
      const tn = document.createElement("span");
      tn.className = "tname";
      tn.textContent = f.title;
      tn.title = f.title;
      const add = document.createElement("button");
      add.textContent = "加入";
      add.onclick = async () => {
        try {
          await api("/api/playlist/add", { path: f.path });
          add.textContent = "已加入";
          add.disabled = true;
          setTimeout(() => { add.textContent = "加入"; add.disabled = false; }, 1500);
        } catch (e) { toast(e.message, true); }
      };
      row.append(tn, add);
      container.appendChild(row);
    }
    if (!(v.dirs || []).length && !(v.files || []).length) {
      container.innerHTML = '<div class="muted small">（空目录）</div>';
    }
  } catch (e) {
    container.innerHTML = '<div class="muted small">加载失败：' + escapeHtml(e.message) + "</div>";
  }
}

function joinPath(dir, name) {
  return dir.endsWith("/") || dir.endsWith("\\") ? dir + name : dir + "/" + name;
}

/* ---------- 歌单管理页 ---------- */

function renderSavedPlaylists() {
  const box = $("#pl-saved");
  if (!box) return;
  box.innerHTML = "";
  if (state && state.current) {
    $("#cur-mount").textContent = state.current.mounted_dir || "-";
    $("#cur-pl-stat").textContent = `共 ${state.current.count} 首，可返回播放页查看。保存后挂载目录即当前曲库。`;
  }
  const list = (state && state.playlists) || [];
  if (!list.length) {
    box.innerHTML = '<div class="muted small">还没有保存的歌单。先在播放页排好歌单，然后在上方保存。</div>';
    return;
  }
  for (const p of list) {
    const row = document.createElement("div");
    row.className = "saved-row";
    const nm = document.createElement("span");
    nm.className = "nm";
    nm.textContent = p.name;
    nm.title = p.name;
    const meta = document.createElement("span");
    meta.className = "meta";
    meta.innerHTML =
      `${p.count} 首 · ${new Date(p.updated).toLocaleString("zh-CN", { hour12: false })}` +
      `<br>挂载：<span class="mono">${escapeHtml(p.mounted_dir || "-")}</span>`;
    const load = document.createElement("button");
    load.textContent = "载入";
    load.className = "primary";
    load.title = "载入为当前歌单（自动切换到其挂载曲库）";
    load.onclick = async () => {
      try {
        await api("/api/playlists/load", { name: p.name });
        stopPlayback(true);
        setView("main");
        toast(`已载入歌单：${p.name}`);
      } catch (e) { toast(e.message, true); }
    };
    const mount = document.createElement("button");
    mount.textContent = "挂到当前曲库";
    mount.title = "把该歌单的挂载目录更新为当前音乐库";
    mount.onclick = async () => {
      try {
        await api("/api/playlists/mount", { name: p.name, dir: state.music_root });
        toast(`「${p.name}」已挂载到当前曲库`);
      } catch (e) { toast(e.message, true); }
    };
    const sel = document.createElement("select");
    sel.title = "导出格式";
    ["m3u", "csv", "pls", "json"].forEach((f) => {
      const o = document.createElement("option");
      o.value = f; o.textContent = f.toUpperCase();
      sel.appendChild(o);
    });
    const exp = document.createElement("button");
    exp.textContent = "导出";
    exp.onclick = () => {
      const url = `/api/playlists/export?name=${encodeURIComponent(p.name)}&format=${sel.value}`;
      const a = document.createElement("a");
      a.href = url; a.download = ""; document.body.appendChild(a); a.click(); a.remove();
    };
    const del = document.createElement("button");
    del.textContent = "删除";
    del.className = "danger";
    del.onclick = async () => {
      if (!confirm(`删除歌单「${p.name}」？`)) return;
      try { await api("/api/playlists/delete", { name: p.name }); }
      catch (e) { toast(e.message, true); }
    };
    row.append(nm, meta, sel, exp, mount, load, del);
    box.appendChild(row);
  }
}

$("#pl-save").onclick = async () => {
  const name = $("#pl-name").value.trim();
  if (!name) { toast("请输入歌单名称", true); return; }
  try {
    await api("/api/playlists/save", { name });
    $("#pl-name").value = "";
    toast(`歌单已保存：${name}`);
  } catch (e) { toast(e.message, true); }
};
$("#pl-name").addEventListener("keydown", (e) => { if (e.key === "Enter") $("#pl-save").click(); });

$("#pl-import").addEventListener("change", async () => {
  const f = $("#pl-import").files[0];
  if (!f) return;
  const fd = new FormData();
  fd.append("file", f);
  try {
    await api("/api/playlists/import", fd);
    stopPlayback(true);
    toast(`导入成功：${f.name}`);
  } catch (e) { toast("导入失败：" + e.message, true); }
  $("#pl-import").value = "";
});

/* ================= 点播模式 =================
   与舞会歌单平行的场景：按指定舞种随机点播、按用户选定顺序的多舞种轮次，
   每个舞种开头语音播报舞种名，全部播完即停止。 */
const comp = { active: false, rounds: [], ri: 0, ti: 0 };
let openTrackLists = new Set(); // 展开曲目列表的舞种

function loadCompPrefs() {
  let p = {};
  try { p = JSON.parse(localStorage.getItem("pp_comp")) || {}; } catch (_) {}
  if (p.announce === undefined) p.announce = true;
  return p;
}
function saveAnnouncePref(v) { localStorage.setItem("pp_comp", JSON.stringify({ announce: v })); }
function roundEditorState() {
  try { return JSON.parse(localStorage.getItem("pp_rounds")) || []; } catch (_) { return []; }
}
function saveRoundEditor(list) { localStorage.setItem("pp_rounds", JSON.stringify(list)); }

function compGenre(name) {
  return (state.categories || []).find((c) => c.name === name);
}

function renderCompPage() {
  if (!state.music_root) {
    $("#genre-groups").innerHTML = '<div class="muted small">请先在播放页设置音乐库</div>';
    renderRoundEditor();
    renderCompQueue();
    return;
  }
  const p = loadCompPrefs();
  $("#strip-comp-announce").checked = p.announce;

  const groupsBox = $("#genre-groups");
  groupsBox.innerHTML = "";
  let lastGroup = null;
  for (const cat of state.categories) {
    if (!cat.tracks.length) continue;
    if (cat.group !== lastGroup) {
      lastGroup = cat.group;
      const h = document.createElement("div");
      h.className = "genre-group-title";
      h.textContent = cat.group;
      groupsBox.appendChild(h);
    }
    const card = document.createElement("div");
    card.className = "genre-card";
    const head = document.createElement("div");
    head.className = "gc-head";
    head.innerHTML = `<span class="gc-name">${escapeHtml(catLabel(cat))}</span><span class="gc-count">${cat.tracks.length} 首</span>`;
    const actions = document.createElement("div");
    actions.className = "gc-actions";
    const play = document.createElement("button");
    play.className = "primary";
    play.textContent = "点播一首";
    play.title = "播报舞种名并随机播放一首，播完即停";
    play.onclick = () => {
      const t = cat.tracks[Math.floor(Math.random() * cat.tracks.length)];
      startComp([{ category: cat.name, tracks: [t] }], true);
    };
    const listBtn = document.createElement("button");
    listBtn.textContent = "曲目";
    listBtn.title = "浏览该舞种全部乐曲并指定播放";
    listBtn.onclick = () => toggleTrackList(cat, tracksBox, listBtn);
    const addBtn = document.createElement("button");
    addBtn.textContent = "加入轮次";
    addBtn.title = "把该舞种加入下方轮次编辑（按加入顺序播放）";
    addBtn.onclick = () => {
      const list = roundEditorState();
      if (list.some((e) => e.cat === cat.name)) { toast("该舞种已在轮次中", true); return; }
      list.push({ cat: cat.name, count: 1 });
      saveRoundEditor(list);
      renderRoundEditor();
      toast(`已加入轮次：${catLabel(cat)}`);
    };
    actions.append(play, listBtn, addBtn);
    const tracksBox = document.createElement("div");
    tracksBox.className = "gc-tracks hidden";
    card.append(head, actions, tracksBox);
    if (openTrackLists.has(cat.name)) {
      fillTrackList(cat, tracksBox);
      tracksBox.classList.remove("hidden");
      listBtn.textContent = "收起";
    }
    groupsBox.appendChild(card);
  }
  renderRoundEditor();
  renderCompQueue();
}

function toggleTrackList(cat, box, btn) {
  if (box.classList.contains("hidden")) {
    fillTrackList(cat, box);
    box.classList.remove("hidden");
    btn.textContent = "收起";
    openTrackLists.add(cat.name);
  } else {
    box.classList.add("hidden");
    btn.textContent = "曲目";
    openTrackLists.delete(cat.name);
  }
}

function fillTrackList(cat, box) {
  box.innerHTML = "";
  box.classList.add("add-tree");
  for (const t of cat.tracks) {
    const row = document.createElement("div");
    row.className = "tree-file";
    const tn = document.createElement("span");
    tn.className = "tname";
    tn.textContent = t.title;
    tn.title = t.title;
    const play = document.createElement("button");
    play.textContent = "播放";
    play.onclick = () => {
      startComp([{ category: cat.name, tracks: [t] }], true);
      toast(`指定播放：${t.title}`);
    };
    row.append(tn, play);
    box.appendChild(row);
  }
}

/* ---------- 轮次编辑：按用户加入的顺序播放 ---------- */
function renderRoundEditor() {
  const list = roundEditorState();
  const card = $("#round-editor-card");
  card.style.display = list.length ? "" : "none";
  const box = $("#round-list");
  box.innerHTML = "";
  list.forEach((entry, i) => {
    const row = document.createElement("div");
    row.className = "round-entry";
    const idx = document.createElement("span");
    idx.className = "re-idx mono";
    idx.textContent = String(i + 1);
    const nm = document.createElement("span");
    nm.className = "re-name";
    const c = compGenre(entry.cat);
    nm.textContent = entry.cat || "根目录";
    nm.title = c ? `${c.tracks.length} 首可用` : entry.cat;
    const cnt = document.createElement("input");
    cnt.type = "number";
    cnt.min = "1";
    cnt.max = "9";
    cnt.value = entry.count;
    cnt.title = "该舞种播放曲目数";
    cnt.addEventListener("change", () => {
      const l = roundEditorState();
      if (l[i]) { l[i].count = Math.max(1, parseInt(cnt.value) || 1); saveRoundEditor(l); }
    });
    const up = document.createElement("button");
    up.textContent = "↑";
    up.title = "上移";
    up.disabled = i === 0;
    up.onclick = () => moveRoundEntry(i, -1);
    const down = document.createElement("button");
    down.textContent = "↓";
    down.title = "下移";
    down.disabled = i === list.length - 1;
    down.onclick = () => moveRoundEntry(i, 1);
    const del = document.createElement("button");
    del.textContent = "移除";
    del.className = "danger";
    del.onclick = () => {
      const l = roundEditorState();
      l.splice(i, 1);
      saveRoundEditor(l);
      renderRoundEditor();
    };
    row.append(idx, nm, document.createTextNode("播放"), cnt, document.createTextNode("首"), up, down, del);
    box.appendChild(row);
  });
}

function moveRoundEntry(i, offset) {
  const l = roundEditorState();
  const j = i + offset;
  if (j < 0 || j >= l.length) return;
  [l[i], l[j]] = [l[j], l[i]];
  saveRoundEditor(l);
  renderRoundEditor();
}

$("#round-clear").onclick = () => { saveRoundEditor([]); renderRoundEditor(); };

$("#comp-start").onclick = () => {
  const list = roundEditorState();
  if (!list.length) { toast("轮次为空：请从上方舞种卡片点「加入轮次」", true); return; }
  const rounds = [];
  for (const entry of list) {
    const c = compGenre(entry.cat);
    if (!c || !c.tracks.length) continue;
    const pool = [...c.tracks];
    for (let i = pool.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [pool[i], pool[j]] = [pool[j], pool[i]];
    }
    rounds.push({ category: entry.cat, tracks: pool.slice(0, Math.max(1, entry.count)) });
  }
  if (!rounds.length) { toast("所选舞种暂无可用曲目", true); return; }
  startComp(rounds, false);
};

function startComp(rounds, fromQuick) {
  if (!rounds.length) return;
  stopPlayback(true);
  comp.active = true;
  comp.rounds = rounds;
  comp.ri = 0;
  comp.ti = 0;
  $("#comp-status-card").style.display = "";
  renderCompQueue();
  toast(fromQuick ? `点播：${rounds[0].category || "指定曲目"}` : `轮次开始，共 ${rounds.length} 个舞种`);
  compRun();
}

function compCurrentTrack() {
  const r = comp.rounds[comp.ri];
  return r ? r.tracks[comp.ti] : null;
}

async function compRun() {
  const round = comp.rounds[comp.ri];
  if (!round) { compFinish(); return; }
  const p = loadCompPrefs();
  const needAnnounce = comp.ti === 0 && p.announce && round.category;
  if (needAnnounce) {
    $("#now-title").textContent = `点播 · ${round.category}`;
    $("#now-sub").innerHTML = '<span class="fading">播报舞种名称…</span>';
    await announce(round.category);
    await sleep(350);
    if (!comp.active) return;
  }
  const t = round.tracks[comp.ti];
  clearPbTimers();
  audio.src = audioUrl(t.path);
  applyVolume();
  audio.play().catch(() => toast("播放失败，请点击播放按钮重试", true));
  setPlayIcon(true);
  renderNow();
  renderCompQueue();
}

function compAdvance(reason) {
  clearPbTimers();
  audio.pause();
  const round = comp.rounds[comp.ri];
  if (!round) { compFinish(); return; }
  if (comp.ti < round.tracks.length - 1) {
    const gap = state.settings.comp.song_gap_sec;
    const deadline = Date.now() + gap * 1000;
    const tick = () => {
      const left = Math.max(0, (deadline - Date.now()) / 1000);
      $("#now-sub").innerHTML = `<span class="fading">${reason ? reason + "，" : ""}${left.toFixed(0)} 秒后：下一首</span>`;
      if (left <= 0) { clearInterval(pb.gapTicker); if (comp.active) { comp.ti++; compRun(); } }
    };
    if (gap > 0) { pb.gapTicker = setInterval(tick, 200); tick(); }
    else { comp.ti++; compRun(); }
  } else {
    const gap = state.settings.comp.genre_gap_sec;
    const next = comp.rounds[comp.ri + 1];
    if (!next) { compFinish(); return; }
    comp.ri++;
    comp.ti = 0;
    const deadline = Date.now() + gap * 1000;
    const tick = () => {
      const left = Math.max(0, (deadline - Date.now()) / 1000);
      $("#now-sub").innerHTML = `<span class="fading">${reason ? reason + "，" : ""}下一位 ${next.category}，${left.toFixed(0)} 秒</span>`;
      if (left <= 0) { clearInterval(pb.gapTicker); if (comp.active) compRun(); }
    };
    if (gap > 0) { pb.gapTicker = setInterval(tick, 200); tick(); }
    else compRun();
  }
}

function compFinish() {
  comp.active = false;
  stopPlayback(true);
  renderCompQueue();
  toast("点播播放完毕");
}

function compStop(manual) {
  comp.active = false;
  clearPbTimers();
  audio.pause();
  setPlayIcon(false);
  renderCompQueue();
  if (manual) toast("已停止点播播放");
}

$("#comp-stop").onclick = () => compStop(true);
$("#strip-comp-announce").addEventListener("change", (e) => saveAnnouncePref(e.target.checked));

function renderCompQueue() {
  const box = $("#comp-queue");
  if (!box) return;
  if (!comp.active && !comp.rounds.length) { box.innerHTML = '<div class="muted small">尚未开始。</div>'; return; }
  box.innerHTML = "";
  comp.rounds.forEach((r, ri) => {
    const g = document.createElement("div");
    g.className = "comp-round" + (comp.active && ri === comp.ri ? " current" : "") + (comp.active && ri < comp.ri ? " done" : "");
    const head = document.createElement("div");
    head.className = "comp-round-head";
    head.innerHTML = `<span class="pl-cat-badge">${escapeHtml(r.category || "指定曲目")}</span>` +
      (comp.active && ri === comp.ri ? ' <span class="fading">播放中</span>' : "");
    g.appendChild(head);
    r.tracks.forEach((t, ti) => {
      const row = document.createElement("div");
      row.className = "comp-track" + (comp.active && ri === comp.ri && ti === comp.ti ? " playing" : "");
      row.textContent = (ti + 1) + ". " + t.title;
      g.appendChild(row);
    });
    box.appendChild(g);
  });
}

/* 舞种名称播报：浏览器语音合成，失败/无引擎时超时兜底继续 */
function announce(text) {
  return new Promise((resolve) => {
    let done = false;
    const finish = () => { if (!done) { done = true; clearTimeout(timer); resolve(); } };
    const timer = setTimeout(finish, 3500);
    try {
      if (!window.speechSynthesis) { finish(); return; }
      const u = new SpeechSynthesisUtterance(text);
      u.lang = "zh-CN";
      u.rate = 0.95;
      u.onend = finish;
      u.onerror = finish;
      speechSynthesis.speak(u);
    } catch (_) { finish(); }
  });
}

function sleep(ms) { return new Promise((r) => setTimeout(r, ms)); }

function gapThen(seconds, label, fn) {
  if (seconds <= 0) { fn(); return; }
  const deadline = Date.now() + seconds * 1000;
  const tick = () => {
    const left = Math.max(0, (deadline - Date.now()) / 1000);
    $("#now-sub").innerHTML = `<span class="fading">${label}${left.toFixed(0)} 秒后开始</span>`;
    if (left <= 0) { clearInterval(pb.gapTicker); fn(); }
  };
  pb.gapTicker = setInterval(tick, 200);
  tick();
}

/* ---------- 使用说明页 ---------- */
const HELP_SECTIONS = [
  {
    img: "setup.png",
    title: "1. 设置曲库目录",
    steps: [
      "首次使用或更换曲库时，点顶部「曲库」区域弹出设置框。",
      "点「选择目录」会弹出系统文件夹选择框（macOS Finder / Windows 资源管理器）。",
      "目录下每个一级子文件夹识别为一个舞种：自动归入摩登 / 拉丁 / 交谊舞分组，识别不了的归入「其它」。",
    ],
  },
  {
    img: "generate.png",
    title: "2. 舞会模式：生成歌单",
    steps: [
      "左侧按摩登 / 拉丁 / 交谊舞分组列出全部舞种，在每个舞种后填入想要的数量。",
      "选择「交错排列」（同舞种不相邻，适合舞会流程）或「按舞种分组」。",
      "点「生成歌单」即随机抽取曲目生成整场歌单，右侧列表双击任意一首即可开始播放。",
    ],
  },
  {
    img: "player.png",
    title: "3. 播放控制：限时 / 渐弱 / 间隔",
    steps: [
      "歌单页与点播页顶部各有「播放设置条」：点 − / ＋ 调整单曲时长、渐弱、间隔，即改即存。",
      "单曲时长到点自动渐弱并切下一首，设为「完整播放」则不限时；歌单与点播的设置相互独立。",
      "播放中修改立即生效；底部进度条可拖动，空格键播放/暂停。",
    ],
  },
  {
    img: "playlists.png",
    title: "4. 歌单管理：保存与载入",
    steps: [
      "顶部「歌单管理」进入独立页面；每个歌单记录自己的挂载曲库目录。",
      "输入名称点「保存」即保存当前歌单；「载入」会自动切换到该歌单的挂载曲库。",
      "支持导出 M3U / CSV / PLS / JSON，也能导入这些格式（兼容旧版 CSV）。",
    ],
  },
  {
    img: "dianbo.png",
    title: "5. 点播模式：指定舞种即点即播",
    steps: [
      "舞种卡片按摩登 / 拉丁 / 交谊舞 / 其它分栏，按赛事 A 组顺序排列。",
      "「点播一首」：播报舞种名并随机播放一首，播完即停。",
      "「曲目」展开该舞种全部乐曲，可指定播放某一首；「加入轮次」把舞种加入轮次编辑。",
    ],
  },
  {
    img: "rounds.png",
    title: "6. 点播模式：多舞种轮次",
    steps: [
      "轮次按你加入并排列的顺序播放（↑↓ 调整顺序，每舞种可设曲目数）。",
      "舞种之间按设定的间隔等待，每个舞种开头播报舞种名称。",
      "点「播放轮次」开始，整轮播完自动停止。",
    ],
  },
  {
    img: "poster.png",
    title: "7. 歌单图片：生成可打印的 A4 歌单",
    steps: [
      "歌单页点「歌单图片」，按舞种分栏自动排版成 A4 竖版（300dpi）。",
      "选择经典预设背景，或「导入自定义图片背景」使用网上下载的模板图。",
      "描边文字保证任何背景下清晰可读；点「下载 PNG」即可打印或分享。",
    ],
  },
  {
    img: "qr.png",
    title: "8. 手机接入",
    steps: [
      "以 -listen 0.0.0.0 参数启动后，顶部出现「手机接入」。",
      "手机连同一 Wi-Fi，扫码或输入地址即可打开同一播放页面。",
    ],
  },
];

function renderHelp() {
  const body = $("#help-body");
  if (body.dataset.done) return;
  body.dataset.done = "1";
  for (const s of HELP_SECTIONS) {
    const sec = document.createElement("div");
    sec.className = "card help-sec";
    const h = document.createElement("h4");
    h.textContent = s.title;
    sec.appendChild(h);
    const img = document.createElement("img");
    img.className = "help-img";
    img.src = `/help/${s.img}`;
    img.alt = s.title;
    sec.appendChild(img);
    const ol = document.createElement("ol");
    for (const step of s.steps) {
      const li = document.createElement("li");
      li.textContent = step;
      ol.appendChild(li);
    }
    sec.appendChild(ol);
    body.appendChild(sec);
  }
  if (state && state.version) {
    const v = document.createElement("p");
    v.className = "muted small center";
    v.textContent = `版本 ${state.version} · 构建于 ${state.build_time}`;
    body.appendChild(v);
  }
}


/* ================= 歌单图片（A4 印刷版渲染） =================
   以 SVG 模板思路直接在 Canvas 上排版：
   - 按歌单编辑顺序平铺（不分组），每首曲名后附舞种标签
   - 舞者线稿水印（OpenMoji 1F483/1F57A，CC BY-SA 4.0 — openmoji.org）
   - 描边文字 + 背景明度自适应，保证任意背景高可读 */
const POSTER_W = 2480, POSTER_H = 3508; // A4 竖版 300dpi
const poster = { bg: "navy", bgImage: null };

// 舞者线稿（OpenMoji，CC BY-SA 4.0）。stroke 占位符在绘制时替换为主题色。
const DANCER_WOMAN = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 72 72"><g fill="none" stroke="__C__" stroke-linecap="round" stroke-linejoin="round"><circle cx="33.9193" cy="9.8664" r="2.8598" stroke-width="2.4"/><path stroke-width="2.4" d="M42.4987,33.8888,32.68,37.6065c-.9532.3813-2.5738.858-3.6224,1.144L14.7585,42.5636a1.8051,1.8051,0,0,1-2.2878-.9533c-.286-.7626.286-1.7159,1.3346-2.1925l10.7719-4.3851a22.5882,22.5882,0,0,0,3.3365-1.8112l6.3869-4.3851c.8579-.5719,2.1925-1.62,2.86-2.1925a3.2033,3.2033,0,0,0,.6673-2.9551l-.286-.858a6.0828,6.0828,0,0,0-1.62-2.3831,6.8574,6.8574,0,0,1-.9533-.572"/><path stroke-width="2.4" d="M38.6856,35.5093l2.0972,10.2c.1907,1.0486.4767,2.7645.572,3.8131l.7626,10.772c.0953,1.0486.7626,1.9065,1.5253,1.9065a1.81,1.81,0,0,0,1.5252-1.9065l.9533-12.3925c.0953-1.0486.0953-2.7645.0953-3.8131V33.6028c0-1.0486-.0953-2.7645-.0953-3.8131l-.6673-9.5327a6.6691,6.6691,0,0,0-1.43-3.2411l-2.4785-2.5738"/><path stroke-width="3.4" d="M55.5585,6.9112l-5.72,3.7178a22.0593,22.0593,0,0,1-3.3364,1.7159l-3.5271,1.3345c-.9533.3813-2.5739.9533-3.6225,1.2393l-2.5738.7626a14.1834,14.1834,0,0,0-3.4318,1.6206l-2.6691,1.9065a22.0512,22.0512,0,0,1-3.3365,1.7159L18.667,24.07"/></g></svg>`;
const DANCER_MAN = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 72 72"><g fill="none" stroke="__C__" stroke-linecap="round" stroke-linejoin="round"><circle cx="33.9688" cy="8.0938" r="3" stroke-width="2.4"/><path stroke-width="3" d="M12.875,12l4.6963,4.6006c0.7861,0.7695,2.3115,1.2231,3.3896,1.0073l6.0782-1.2158c1.0781-0.2158,2.8584-0.4609,3.955-0.5454 l9.0118-0.6934c1.0966-0.0845,2.8808,0.003,3.9638,0.1944L59,18"/><path stroke-width="2.4" d="M29,20c0,0,0.4502,0.4502,1,1c0.5498,0.5498,1.1758,1.8823,1.3926,2.9609l1.2148,6.0782c0.2168,1.0781,0.0381,2.788-0.3945,3.7988 l-4.4258,10.3242c-0.4326,1.0108-0.5019,2.6914-0.1543,3.7354l2.7344,8.205C30.7148,57.1465,31.6748,58,32.5,58 c0.8252,0,1.3389-0.8857,1.1426-1.9678l-1.2852-7.0644c-0.1963-1.0821,0.0801-2.7549,0.6133-3.7158l3.0576-5.504 C36.5625,38.7871,37.4492,38,38,38c0.5498,0,1,0.9004,1,2v9c0,1.0996-0.0342,2.8994-0.0752,3.999l-0.4492,12.002 C38.4346,66.1006,39.1426,67,40.0508,67c0.9072,0,1.7705-0.8916,1.918-1.9824l1.7626-13.0352 c0.1475-1.0908,0.3331-2.8799,0.4112-3.9775l0.7148-10.0098c0.0781-1.0976-0.0136-2.8818-0.2051-3.9648L42,19"/></g></svg>`;

const posterImgCache = {};
function posterDancerImage(svgStr, key) {
  if (posterImgCache[key]) return posterImgCache[key];
  const img = new Image();
  img.onload = () => {
    if (state && !$("#dlg-poster").classList.contains("hidden")) renderPoster();
  };
  img.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(svgStr);
  posterImgCache[key] = img;
  return img;
}

const POSTER_PRESETS = [
  { id: "navy",  name: "午夜蓝金", dark: true,
    css: "linear-gradient(180deg,#101d3a,#233a66)",
    draw: (ctx) => { posterGrad(ctx, "#101d3a", "#243b68"); posterFrame(ctx, "#d9b45b"); } },
  { id: "black", name: "绅士黑", dark: true,
    css: "linear-gradient(180deg,#2b2b30,#101013)",
    draw: (ctx) => { posterGrad(ctx, "#2e2e34", "#0f0f12"); posterFrame(ctx, "#c8c0a8"); } },
  { id: "wine",  name: "酒红丝绒", dark: true,
    css: "linear-gradient(180deg,#7a1f2b,#3d0d15)",
    draw: (ctx) => { posterGrad(ctx, "#82222f", "#3a0c14"); posterFrame(ctx, "#e0c184"); } },
  { id: "green", name: "墨绿鎏金", dark: true,
    css: "linear-gradient(180deg,#12352a,#1e4a3a)",
    draw: (ctx) => { posterGrad(ctx, "#143a2d", "#1d4a3b"); posterFrame(ctx, "#d9b45b"); } },
  { id: "ivory", name: "香槟象牙", dark: false,
    css: "linear-gradient(180deg,#f7f1e3,#e6d7b8)",
    draw: (ctx) => { posterGrad(ctx, "#f8f2e5", "#e8dab9"); posterFrame(ctx, "#b99a52"); } },
];

function posterGrad(ctx, from, to) {
  const g = ctx.createLinearGradient(0, 0, 0, POSTER_H);
  g.addColorStop(0, from); g.addColorStop(1, to);
  ctx.fillStyle = g;
  ctx.fillRect(0, 0, POSTER_W, POSTER_H);
  const v = ctx.createRadialGradient(POSTER_W/2, POSTER_H*0.42, POSTER_H*0.2, POSTER_W/2, POSTER_H/2, POSTER_H*0.75);
  v.addColorStop(0, "rgba(0,0,0,0)"); v.addColorStop(1, "rgba(0,0,0,0.30)");
  ctx.fillStyle = v; ctx.fillRect(0, 0, POSTER_W, POSTER_H);
}
function posterFrame(ctx, color) {
  ctx.strokeStyle = color;
  ctx.lineWidth = 8; ctx.strokeRect(58, 58, POSTER_W-116, POSTER_H-116);
  ctx.lineWidth = 2; ctx.strokeRect(84, 84, POSTER_W-168, POSTER_H-168);
}

function posterBgLuminanceDark() {
  const c = document.createElement("canvas");
  c.width = 80; c.height = 113;
  const x = c.getContext("2d");
  x.drawImage($("#poster-canvas"), 0, 0, 80, 113);
  const d = x.getImageData(0, 0, 80, 113).data;
  let sum = 0;
  for (let i = 0; i < d.length; i += 4) sum += 0.2126*d[i] + 0.7152*d[i+1] + 0.0722*d[i+2];
  return (sum / (d.length / 4)) < 140;
}

function drawStrokeText(ctx, text, x, y, align, size, fill, stroke, weight, strokeW) {
  ctx.font = `${weight ? weight + " " : ""}${size}px "PingFang SC","Microsoft YaHei",serif`;
  ctx.textAlign = align;
  ctx.textBaseline = "top";
  ctx.lineWidth = strokeW ?? Math.max(3, size * 0.10);
  ctx.strokeStyle = stroke;
  ctx.lineJoin = "round";
  ctx.strokeText(text, x, y);
  ctx.fillStyle = fill;
  ctx.fillText(text, x, y);
}

// 绘制舞种标签（圆角描边胶囊 + 文字），返回实际宽度
function drawTag(ctx, text, x, yCenter, size, accent, stroke) {
  ctx.font = `${size}px "PingFang SC","Microsoft YaHei",serif`;
  const tw = ctx.measureText(text).width;
  const padX = size * 0.55, padY = size * 0.42;
  const w = tw + padX * 2, h = size + padY * 2;
  ctx.beginPath();
  if (ctx.roundRect) ctx.roundRect(x, yCenter - h/2, w, h, h/2);
  else ctx.rect(x, yCenter - h/2, w, h);
  ctx.strokeStyle = accent; ctx.lineWidth = Math.max(2, size * 0.07);
  ctx.stroke();
  ctx.font = `${size}px "PingFang SC","Microsoft YaHei",serif`;
  ctx.textAlign = "left"; ctx.textBaseline = "middle";
  ctx.lineWidth = Math.max(2, size * 0.10); ctx.strokeStyle = stroke; ctx.lineJoin = "round";
  ctx.strokeText(text, x + padX, yCenter);
  ctx.fillStyle = accent; ctx.fillText(text, x + padX, yCenter);
  return w;
}

function renderPoster() {
  const cv = $("#poster-canvas");
  const ctx = cv.getContext("2d");
  const preset = POSTER_PRESETS.find((p) => p.id === poster.bg) || POSTER_PRESETS[0];
  if (poster.bgImage) {
    const img = poster.bgImage, s = Math.max(POSTER_W/img.width, POSTER_H/img.height);
    const w = img.width*s, h = img.height*s;
    ctx.fillStyle = "#000"; ctx.fillRect(0, 0, POSTER_W, POSTER_H);
    ctx.drawImage(img, (POSTER_W-w)/2, (POSTER_H-h)/2, w, h);
  } else {
    preset.draw(ctx);
  }
  const dark = poster.bgImage ? posterBgLuminanceDark() : preset.dark;
  const ink    = dark ? "#f5efe2" : "#2b2115";
  const stroke = dark ? "rgba(8,10,16,0.9)" : "rgba(255,252,240,0.95)";
  const accent = dark ? "#e3c37a" : "#8a6d1f";
  const muted  = dark ? "rgba(245,239,226,0.78)" : "rgba(43,33,21,0.75)";
  const dancerC = dark ? "#e3c37a" : "#8a6d1f";

  const M = 190; // 页边距
  // 舞者线稿水印（背景装饰，低透明度）
  const wImg = posterDancerImage(DANCER_WOMAN.replaceAll("__C__", dancerC), "w" + dancerC);
  const mImg = posterDancerImage(DANCER_MAN.replaceAll("__C__", dancerC), "m" + dancerC);
  ctx.save();
  ctx.globalAlpha = dark ? 0.20 : 0.22;
  if (wImg.complete && wImg.naturalWidth) ctx.drawImage(wImg, POSTER_W - 1010, POSTER_H - 1560, 860, 860);
  if (mImg.complete && mImg.naturalWidth) ctx.drawImage(mImg, 120, POSTER_H - 1150, 640, 640);
  ctx.restore();

  // 半透明衬底（内容区）
  ctx.fillStyle = dark ? "rgba(6,8,14,0.38)" : "rgba(255,252,242,0.60)";
  ctx.beginPath();
  const sy = 150, sh = POSTER_H - 150 - 190;
  if (ctx.roundRect) ctx.roundRect(108, sy, POSTER_W - 216, sh, 40);
  else ctx.rect(108, sy, POSTER_W - 216, sh);
  ctx.fill();

  // 标题区
  const title = ($("#poster-title").value || "舞会歌单").trim();
  const sub = $("#poster-sub").value.trim();
  ctx.textBaseline = "top";
  drawStrokeText(ctx, title, POSTER_W/2, 268, "center", 168, ink, stroke, "bold", 14);
  ctx.fillStyle = accent;
  ctx.fillRect(POSTER_W/2 - 270, 512, 540, 7);
  if (sub) drawStrokeText(ctx, sub, POSTER_W/2, 566, "center", 64, muted, stroke, "", 4);

  // 汇总行
  const items = (state.playlist || []).filter((t) => !t.missing);
  let totalSec = 0;
  for (const t of items) totalSec += state.durations[t.path] || 0;
  const summary = `共 ${items.length} 首` + (totalSec > 0 ? ` · 约 ${Math.round(totalSec/60)} 分钟` : "");
  drawStrokeText(ctx, summary, POSTER_W/2, sub ? 700 : 640, "center", 50, accent, stroke, "", 3);

  // 曲目：按歌单编辑顺序平铺（不分组），双栏
  const listTop = (sub ? 830 : 760) + 90;
  const listBottom = POSTER_H - 260;
  const colGap = 110;
  const cols = items.length > 26 ? 3 : 2;
  const colW = (POSTER_W - M*2 - colGap*(cols-1)) / cols;
  const perCol = Math.ceil(items.length / cols);

  // 条目高度随数量自适应：少时舒展铺满整页，多时压缩保证一页排下
  const avail = listBottom - listTop;
  let entryH = Math.min(172, Math.max(96, avail / perCol));
  while (perCol * entryH > avail && entryH > 64) entryH -= 4;
  const nameSize = Math.min(58, entryH * 0.42);
  const tagSize = nameSize * 0.5;

  for (let c = 0; c < cols; c++) {
    const x0 = M + c * (colW + colGap);
    for (let i = 0; i < perCol; i++) {
      const idx = c * perCol + i;
      const t = items[idx];
      if (!t) break;
      const y = listTop + i * entryH;
      drawStrokeText(ctx, String(idx + 1), x0, y, "left", nameSize * 0.86, accent, stroke, "bold", 3);
      let tx = x0 + nameSize * 1.1;
      const durText = state.durations[t.path] ? fmtTime(state.durations[t.path]) : "";
      const tagText = (t.category || "").trim();
      ctx.font = `${nameSize}px "PingFang SC","Microsoft YaHei",serif`;
      const nameMax = colW - (tx - x0) - (durText ? 190 : 40) - (tagText ? tagText.length * tagSize * 1.6 + 30 : 0);
      let title = t.title;
      while (title.length > 1 && measureW(title, nameSize) > nameMax) title = title.slice(0, -1);
      if (title !== t.title) title += "…";
      drawStrokeText(ctx, title, tx, y, "left", nameSize, ink, stroke, "600", nameSize * 0.09);
      tx += measureW(title, nameSize) + nameSize * 0.35;
      if (tagText) tx += drawTag(ctx, tagText, tx, y + nameSize * 0.52, tagSize, accent, stroke);
      if (durText) drawStrokeText(ctx, durText, x0 + colW, y + nameSize * 0.06, "right", nameSize * 0.72, muted, stroke, "", 3);
    }
  }

  // 页脚
  drawStrokeText(ctx, `舞会音乐播放器 · ${new Date().toLocaleDateString("zh-CN")}`,
    POSTER_W/2, POSTER_H - 150, "center", 40, muted, stroke, "", 3);
}

function measureW(text, size) {
  const cv = $("#poster-canvas");
  const ctx = cv.getContext("2d");
  ctx.font = `${size}px "PingFang SC","Microsoft YaHei",serif`;
  return ctx.measureText(text).width;
}

/* ---------- 歌单图片对话框 ---------- */
$("#btn-poster").onclick = () => {
  if (!state.playlist.length) { toast("歌单为空，先生成或添加乐曲", true); return; }
  $("#poster-title").value = "舞会歌单";
  $("#poster-sub").value = new Date().toLocaleDateString("zh-CN") + " 舞会";
  renderPresetThumbs();
  $("#dlg-poster").classList.remove("hidden");
  renderPoster();
};

function renderPresetThumbs() {
  const box = $("#poster-presets");
  box.innerHTML = "";
  for (const p of POSTER_PRESETS) {
    const b = document.createElement("button");
    b.className = "preset-thumb" + (poster.bg === p.id && !poster.bgImage ? " active" : "");
    b.style.background = p.css;
    b.title = p.name;
    b.onclick = () => { poster.bg = p.id; poster.bgImage = null; renderPresetThumbs(); renderPoster(); };
    box.appendChild(b);
  }
}

$("#poster-title").addEventListener("input", renderPoster);
$("#poster-sub").addEventListener("input", renderPoster);

$("#poster-import").addEventListener("change", () => {
  const f = $("#poster-import").files[0];
  if (!f) return;
  const img = new Image();
  img.onload = () => { poster.bgImage = img; poster.bg = "custom"; renderPresetThumbs(); renderPoster(); toast("自定义背景已应用"); };
  img.onerror = () => toast("图片无法读取", true);
  img.src = URL.createObjectURL(f);
  $("#poster-import").value = "";
});

$("#poster-download").onclick = () => {
  renderPoster();
  $("#poster-canvas").toBlob((blob) => {
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = ($("#poster-title").value.trim() || "歌单") + "-A4.png";
    document.body.appendChild(a); a.click(); a.remove();
    toast("歌单图片已生成");
  }, "image/png");
};

/* ---------- 启动 ---------- */
refresh();
