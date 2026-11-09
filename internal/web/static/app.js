const state = { tab: "overview", range: "1h", rankBy: "domain", rankKind: "flow", settingsLoaded: false };
let busy = false;

function el(tag, attrs, text) {
  const node = document.createElement(tag);
  if (attrs) {
    for (const [key, value] of Object.entries(attrs)) node.setAttribute(key, value);
  }
  if (text != null) node.textContent = text;
  return node;
}

async function api(path, opt) {
  opt = opt || {};
  const res = await fetch(path, {
    credentials: "same-origin",
    method: opt.method || "GET",
    body: opt.body,
    headers: { "Content-Type": "application/json" },
  });
  const data = await res.json().catch(() => ({}));
  if (res.status === 401 && !String(path).includes("/api/login")) {
    document.getElementById("login").classList.remove("hidden");
    throw new Error("需要口令");
  }
  if (!res.ok) throw new Error(data.error || ("HTTP " + res.status));
  return data;
}

function onErr(err) {
  if (String(err.message || err).includes("口令")) return;
  setNotice(err.message || String(err), false);
}

function setNotice(message, ok) {
  const node = document.getElementById("notice");
  node.textContent = message || "";
  node.className = message ? (ok ? "notice ok" : "notice err") : "notice";
}

function fmtBytes(value) {
  let n = Number(value) || 0;
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return (i === 0 ? n.toFixed(0) : n.toFixed(1)) + " " + units[i];
}

function fmtTime(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  return date.toLocaleString();
}

function paintStatus(st) {
  document.getElementById("hostname").textContent = st.host || "";
  document.getElementById("ver").textContent = st.version || "";
  const vl = document.getElementById("vlstate");
  vl.textContent = (st.vl_ok ? "VictoriaLogs 正常" : "VictoriaLogs 异常") + " · " + (st.vl_id || "0:0");
  vl.title = st.vl_error || "";
  vl.className = st.vl_ok ? "pill ok" : "pill bad";
  const banner = document.getElementById("banner");
  if (!st.capture_supported) {
    banner.textContent = "这个系统不能抓包，只能查询 VictoriaLogs 里已有的日志。采集需要在 Linux 上运行。";
    banner.classList.remove("hidden");
  } else if (!st.ifaces_selected) {
    banner.textContent = "还没有选择网卡。到设置里勾选要采集的网卡，然后保存。";
    banner.classList.remove("hidden");
  } else {
    banner.classList.add("hidden");
    banner.textContent = "";
  }
}

function card(label, value) {
  const node = el("div", { class: "card" });
  node.appendChild(el("div", { class: "k" }, label));
  node.appendChild(el("div", { class: "v" }, String(value ?? 0)));
  return node;
}

function chart(points) {
  if (!points.length) return el("p", { class: "muted" }, "这段时间还没有连接日志。");
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("viewBox", "0 0 640 180");
  svg.setAttribute("class", "chart");
  const max = Math.max(...points.map((p) => Number(p.hits) || 0), 1);
  const pad = 10;
  const step = points.length === 1 ? 0 : (640 - pad * 2) / (points.length - 1);
  let d = "";
  points.forEach((p, i) => {
    const x = pad + i * step;
    const y = 170 - pad - ((Number(p.hits) || 0) / max) * (170 - pad * 2);
    d += (i ? "L" : "M") + x.toFixed(1) + "," + y.toFixed(1);
  });
  const path = document.createElementNS(svg.namespaceURI, "path");
  path.setAttribute("d", d);
  path.setAttribute("fill", "none");
  path.setAttribute("stroke", "#d6f25c");
  path.setAttribute("stroke-width", "2");
  svg.appendChild(path);
  return svg;
}

function rankList(title, rows, field, kind) {
  const box = el("div");
  box.appendChild(el("h2", null, title));
  if (!rows.length) {
    box.appendChild(el("p", { class: "muted" }, "没有数据"));
    return box;
  }
  const max = Math.max(...rows.map((row) => Number(row.hits) || 0), 1);
  for (const row of rows) {
    const line = el("div", { class: "rowline" });
    const button = el("button", { class: "link", type: "button" }, row.key);
    button.addEventListener("click", () => openFilter(field, row.key, kind));
    line.appendChild(button);
    line.appendChild(el("span", { class: "mono" }, String(Math.round(Number(row.hits) || 0))));
    line.appendChild(el("span", { class: "tag" }, fmtBytes(row.bytes)));
    const bar = el("div", { class: "bar" });
    const span = document.createElement("span");
    span.style.width = Math.max(2, ((Number(row.hits) || 0) / max) * 100) + "%";
    bar.appendChild(span);
    box.appendChild(line);
    box.appendChild(bar);
  }
  return box;
}

function openFilter(field, key, kind) {
  const map = { domain: "f-domain", app: "f-app", dst_ip: "f-dst" };
  const id = map[field];
  if (id) document.getElementById(id).value = key;
  if (kind) document.getElementById("f-kind").value = kind;
  show("flows");
}

function flowTable(logs) {
  const table = document.createElement("table");
  const head = document.createElement("tr");
  for (const name of ["时间", "网卡", "应用", "域名", "识别", "协议", "源", "目标", "字节"]) {
    head.appendChild(el("th", null, name));
  }
  const thead = document.createElement("thead");
  thead.appendChild(head);
  table.appendChild(thead);
  const body = document.createElement("tbody");
  for (const row of logs) {
    const tr = document.createElement("tr");
    const src = row.src_ip ? row.src_ip + (row.src_port ? ":" + row.src_port : "") : "";
    const dst = row.dst_ip ? row.dst_ip + (row.dst_port ? ":" + row.dst_port : "") : "";
    const values = [fmtTime(row._time), row.iface || "", row.app || "", row.domain || "", row.domain_from || "", row.proto || "", src, dst, fmtBytes(row.bytes)];
    values.forEach((text, index) => {
      const cell = el("td", { class: index === 0 ? "" : "mono" }, index === 3 ? "" : text);
      if (index === 3 && text) {
        const button = el("button", { class: "link", type: "button" }, text);
        button.addEventListener("click", () => openFilter("domain", text, row.kind || "flow"));
        cell.appendChild(button);
      } else if (index !== 3) {
        cell.textContent = text;
      }
      tr.appendChild(cell);
    });
    body.appendChild(tr);
  }
  table.appendChild(body);
  return table;
}

async function loadOverview() {
  const st = await api("/api/status");
  paintStatus(st);
  document.getElementById("cards").replaceChildren(
    card("已选网卡", st.ifaces_selected),
    card("已抓包", st.packets),
    card("内核丢包", st.kernel_drops),
    card("已写入", st.inserted),
    card("写入失败", st.insert_errors),
    card("跟踪中的连接", st.tracked),
  );
  let series = { points: [] };
  let domains = { rows: [] };
  let apps = { rows: [] };
  try {
    [series, domains, apps] = await Promise.all([
      api("/api/series?range=" + state.range),
      api("/api/rank?by=domain&kind=flow&range=" + state.range + "&limit=8"),
      api("/api/rank?by=app&kind=flow&range=" + state.range + "&limit=8"),
    ]);
  } catch (err) {
    onErr(err);
  }
  document.getElementById("chartcard").replaceChildren(el("h2", null, "连接数"), chart(series.points || []));
  document.getElementById("topdomains").replaceChildren(rankList("域名", domains.rows || [], "domain", "flow"));
  document.getElementById("topapps").replaceChildren(rankList("应用", apps.rows || [], "app", "flow"));
}

async function loadFlows() {
  const params = new URLSearchParams();
  params.set("range", state.range);
  for (const [key, id] of [
    ["domain", "f-domain"],
    ["app", "f-app"],
    ["iface", "f-iface"],
    ["dst_ip", "f-dst"],
    ["src_ip", "f-src"],
    ["proto", "f-proto"],
    ["port", "f-port"],
    ["kind", "f-kind"],
    ["q", "f-q"],
  ]) {
    const value = document.getElementById(id).value.trim();
    if (value) params.set(key, value);
  }
  const [st, data] = await Promise.all([api("/api/status"), api("/api/flows?" + params.toString())]);
  paintStatus(st);
  const box = document.getElementById("flowtable");
  box.replaceChildren();
  if (!data.logs || !data.logs.length) {
    box.appendChild(el("p", { class: "muted" }, "这段时间没有符合条件的日志。"));
    return;
  }
  box.appendChild(flowTable(data.logs));
}

function rankTitle() {
  const name = { domain: "域名", app: "应用", dst_ip: "目标地址" }[state.rankBy] || "排行";
  return name + (state.rankKind === "dns" ? " · DNS" : " · 连接");
}

async function loadRank() {
  const [st, data] = await Promise.all([
    api("/api/status"),
    api("/api/rank?by=" + encodeURIComponent(state.rankBy) + "&kind=" + encodeURIComponent(state.rankKind) + "&range=" + state.range + "&limit=30"),
  ]);
  paintStatus(st);
  document.getElementById("rankbody").replaceChildren(rankList(rankTitle(), data.rows || [], state.rankBy, state.rankKind));
}

function lines(id) {
  return document.getElementById(id).value.split(/[\n,]/).map((part) => part.trim()).filter(Boolean);
}

function fillForm(cfg) {
  document.getElementById("listen").value = cfg.listen || "";
  document.getElementById("vlurl").value = cfg.vl_url || "";
  document.getElementById("vlid").value = cfg.vl_id || "0:0";
  document.getElementById("incDom").value = (cfg.include_domains || []).join("\n");
  document.getElementById("excDom").value = (cfg.exclude_domains || []).join("\n");
  document.getElementById("incApp").value = (cfg.include_apps || []).join("\n");
  document.getElementById("excApp").value = (cfg.exclude_apps || []).join("\n");
  document.getElementById("incProto").value = (cfg.include_protos || []).join("\n");
  document.getElementById("excProto").value = (cfg.exclude_protos || []).join("\n");
  document.getElementById("incPort").value = (cfg.include_ports || []).join("\n");
  document.getElementById("excPort").value = (cfg.exclude_ports || []).join("\n");
  document.getElementById("proc").checked = !!cfg.proc_enabled;
  document.getElementById("token").value = "";
  document.getElementById("clearToken").checked = false;
  document.getElementById("tokenHint").textContent = cfg.token_set ? "已设置口令。留空表示不修改。" : "当前没有口令。";
}

function renderIfaces(list) {
  const box = document.getElementById("ifaces");
  box.replaceChildren(el("h2", null, "网卡"));
  if (!list.length) {
    box.appendChild(el("p", { class: "muted" }, "没有看到网卡。"));
    return;
  }
  for (const item of list) {
    const label = el("label", { class: "iface" });
    const input = document.createElement("input");
    input.type = "checkbox";
    input.value = item.name;
    input.checked = !!item.selected;
    label.appendChild(input);
    const flags = [item.name];
    if (item.loopback) flags.push("回环");
    if (!item.up) flags.push("未启用");
    label.appendChild(el("span", null, flags.join(" · ")));
    if (item.addrs && item.addrs.length) label.appendChild(el("span", { class: "tag" }, item.addrs.join(" ")));
    if (item.error) label.appendChild(el("span", { class: "tag" }, item.error));
    else if (item.selected) label.appendChild(el("span", { class: "tag" }, "包 " + item.packets + " · 丢弃 " + item.drops));
    box.appendChild(label);
  }
  box.appendChild(el("p", { class: "muted" }, "只选实际出网的那块网卡。桥接时不要把桥和成员网卡一起选，否则同一条连接会记两次。"));
}

async function loadSettings() {
  const [cfg, ifaces, st] = await Promise.all([api("/api/config"), api("/api/ifaces"), api("/api/status")]);
  paintStatus(st);
  if (!state.settingsLoaded) {
    fillForm(cfg);
    state.settingsLoaded = true;
  }
  if (!document.activeElement || !document.getElementById("ifaces").contains(document.activeElement)) {
    renderIfaces(ifaces);
  }
}

async function saveConfig(event) {
  event.preventDefault();
  const result = await api("/api/config", {
    method: "PUT",
    body: JSON.stringify({
      listen: document.getElementById("listen").value.trim(),
      vl_url: document.getElementById("vlurl").value.trim(),
      vl_id: document.getElementById("vlid").value.trim(),
      ifaces: [...document.querySelectorAll("#ifaces input:checked")].map((input) => input.value),
      include_domains: lines("incDom"),
      exclude_domains: lines("excDom"),
      include_apps: lines("incApp"),
      exclude_apps: lines("excApp"),
      include_protos: lines("incProto"),
      exclude_protos: lines("excProto"),
      include_ports: lines("incPort"),
      exclude_ports: lines("excPort"),
      proc_enabled: document.getElementById("proc").checked,
      token: document.getElementById("token").value,
      clear_token: document.getElementById("clearToken").checked,
    }),
  });
  setNotice(result.restart_required ? "已保存。监听地址要重启进程后才会改变。" : "已保存，采集和过滤已经生效。", true);
  state.settingsLoaded = false;
  await loadSettings();
}

async function refresh() {
  if (busy) return;
  busy = true;
  try {
    if (state.tab === "overview") await loadOverview();
    else if (state.tab === "flows") await loadFlows();
    else if (state.tab === "rank") await loadRank();
    else await loadSettings();
  } catch (err) {
    onErr(err);
  } finally {
    busy = false;
  }
}

function show(tab) {
  state.tab = tab;
  for (const name of ["overview", "flows", "rank", "settings"]) {
    document.getElementById(name).classList.toggle("hidden", name !== tab);
  }
  document.querySelectorAll("button.nav").forEach((button) => {
    button.classList.toggle("on", button.dataset.tab === tab);
  });
  refresh();
}

function mark(group, attr, value) {
  document.querySelectorAll("#" + group + " button").forEach((button) => {
    button.classList.toggle("on", button.dataset[attr] === value);
  });
}

document.querySelectorAll("button.nav").forEach((button) => {
  button.addEventListener("click", () => show(button.dataset.tab));
});
document.getElementById("ranges").addEventListener("click", (event) => {
  const button = event.target.closest("button");
  if (!button) return;
  state.range = button.dataset.range;
  mark("ranges", "range", state.range);
  refresh();
});
document.getElementById("rankby").addEventListener("click", (event) => {
  const button = event.target.closest("button");
  if (!button) return;
  state.rankBy = button.dataset.by;
  mark("rankby", "by", state.rankBy);
  if (state.tab === "rank") refresh();
});
document.getElementById("rankkind").addEventListener("click", (event) => {
  const button = event.target.closest("button");
  if (!button) return;
  state.rankKind = button.dataset.kind;
  mark("rankkind", "kind", state.rankKind);
  if (state.tab === "rank") refresh();
});
document.getElementById("flowform").addEventListener("submit", (event) => {
  event.preventDefault();
  loadFlows().catch(onErr);
});
document.getElementById("settingsForm").addEventListener("submit", (event) => {
  saveConfig(event).catch(onErr);
});
document.getElementById("reload").addEventListener("click", () => {
  if (state.tab === "settings") state.settingsLoaded = false;
  refresh();
});
document.getElementById("loginForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  try {
    await api("/api/login", {
      method: "POST",
      body: JSON.stringify({ token: document.getElementById("loginToken").value }),
    });
    document.getElementById("login").classList.add("hidden");
    document.getElementById("loginErr").textContent = "";
    setNotice("");
    refresh();
  } catch (err) {
    document.getElementById("loginErr").textContent = err.message || "登录失败";
  }
});
setInterval(() => {
  if (!document.getElementById("auto").checked || state.tab === "settings") return;
  refresh();
}, 5000);
refresh();
