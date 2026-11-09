const state = { tab: "overview", range: "1h", rankBy: "domain", rankKind: "flow", metric: "bytes", settingsLoaded: false };
let analyzeCache = null;
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
  return trendChart([{ name: "all", points: points || [] }], "hits", { bytesLine: true });
}

const palette = ["#d6f25c", "#79d7c3", "#e6b35a", "#f07178", "#8bd17c", "#9aa7ff"];
const protoColor = { tcp: "#d6f25c", udp: "#79d7c3", icmp: "#e6b35a" };

function seriesColor(name, index) {
  return protoColor[name] || palette[index % palette.length];
}

function metricValue(point, metric) {
  if (!point) return 0;
  return metric === "bytes" ? Number(point.bytes) || 0 : Number(point.hits) || 0;
}

function fmtMetric(value, metric) {
  return metric === "bytes" ? fmtBytes(value) : String(Math.round(Number(value) || 0));
}

function fmtTick(ts) {
  const date = new Date(ts);
  if (Number.isNaN(date.getTime())) return "";
  const hh = String(date.getHours()).padStart(2, "0");
  const mm = String(date.getMinutes()).padStart(2, "0");
  if (state.range === "24h" || state.range === "7d") return (date.getMonth() + 1) + "/" + date.getDate() + " " + hh + ":00";
  return hh + ":" + mm;
}

function svgEl(name, attrs, text) {
  const node = document.createElementNS("http://www.w3.org/2000/svg", name);
  for (const [key, value] of Object.entries(attrs || {})) node.setAttribute(key, String(value));
  if (text != null) node.textContent = text;
  return node;
}

function alignSeries(series) {
  const times = [];
  const seen = new Set();
  for (const item of series) {
    for (const point of item.points || []) {
      if (!seen.has(point.t)) {
        seen.add(point.t);
        times.push(point.t);
      }
    }
  }
  times.sort((a, b) => a - b);
  const rows = series.map((item) => {
    const map = new Map((item.points || []).map((point) => [point.t, point]));
    return times.map((t) => map.get(t) || { t, hits: 0, bytes: 0 });
  });
  return { times, rows };
}

function trendChart(series, metric, opt) {
  opt = opt || {};
  const clean = (series || []).filter((item) => item && (item.points || []).length);
  if (!clean.length) return el("p", { class: "muted" }, "这段时间还没有连接日志。");
  const { times, rows } = alignSeries(clean);
  if (!times.length) return el("p", { class: "muted" }, "这段时间还没有连接日志。");
  const width = 720;
  const height = opt.tall ? 280 : 200;
  const left = 54;
  const right = opt.bytesLine ? 58 : 16;
  const top = 16;
  const bottom = 28;
  const plotW = width - left - right;
  const plotH = height - top - bottom;
  const values = rows.map((row) => row.map((point) => metricValue(point, metric)));
  const bytes = rows.map((row) => row.map((point) => metricValue(point, "bytes")));
  let max = 1;
  if (clean.length > 1 && clean[0].name !== "all") {
    for (let i = 0; i < times.length; i++) {
      let sum = 0;
      values.forEach((row) => { sum += row[i]; });
      if (sum > max) max = sum;
    }
  } else {
    values.forEach((row) => row.forEach((value) => { if (value > max) max = value; }));
  }
  let maxBytes = 1;
  bytes.forEach((row) => row.forEach((value) => { if (value > maxBytes) maxBytes = value; }));
  const xAt = (i) => left + (times.length === 1 ? plotW / 2 : (plotW * i) / (times.length - 1));
  const yAt = (value, peak) => top + plotH - (value / peak) * plotH;
  const svg = svgEl("svg", { viewBox: "0 0 " + width + " " + height, class: opt.tall ? "chart tall" : "chart" });
  for (let tick = 0; tick <= 3; tick++) {
    const y = top + (plotH * tick) / 3;
    svg.appendChild(svgEl("line", { x1: left, x2: width - right, y1: y, y2: y, stroke: "#2c342f" }));
    svg.appendChild(svgEl("text", { x: 4, y: y + 4, fill: "#8e9c93", "font-size": 11 }, fmtMetric(max * (1 - tick / 3), metric)));
  }
  const xs = times.map((_, i) => xAt(i));
  if (clean.length > 1 && clean[0].name !== "all") {
    const base = values[0].map(() => 0);
    values.forEach((row, index) => {
      const yTop = row.map((value, i) => yAt(base[i] + value, max));
      const yBot = base.map((value) => yAt(value, max));
      const path = svgEl("path", { d: areaPath(xs, yTop, yBot), fill: seriesColor(clean[index].name, index), "fill-opacity": "0.82" });
      const title = svgEl("title");
      title.textContent = clean[index].name;
      path.appendChild(title);
      svg.appendChild(path);
      row.forEach((value, i) => { base[i] += value; });
    });
  } else {
    const yTop = values[0].map((value) => yAt(value, max));
    const yBot = values[0].map(() => yAt(0, max));
    svg.appendChild(svgEl("path", { d: areaPath(xs, yTop, yBot), fill: "#d6f25c", "fill-opacity": "0.22" }));
    svg.appendChild(svgEl("path", { d: linePath(xs, yTop), fill: "none", stroke: "#d6f25c", "stroke-width": 2 }));
  }
  if (opt.bytesLine) {
    const yBytes = bytes[0].map((value) => yAt(value, maxBytes));
    svg.appendChild(svgEl("path", { d: linePath(xs, yBytes), fill: "none", stroke: "#79d7c3", "stroke-width": 2 }));
    svg.appendChild(svgEl("text", { x: width - 4, y: top + 10, fill: "#79d7c3", "font-size": 11, "text-anchor": "end" }, fmtBytes(maxBytes)));
  }
  const marks = times.length < 3 ? [0] : [0, Math.floor((times.length - 1) / 2), times.length - 1];
  marks.forEach((i) => {
    svg.appendChild(svgEl("text", { x: xAt(i), y: height - 8, fill: "#8e9c93", "font-size": 11, "text-anchor": i === 0 ? "start" : "middle" }, fmtTick(times[i])));
  });
  return svg;
}

function areaPath(xs, yTop, yBot) {
  let d = "";
  xs.forEach((x, i) => { d += (i ? "L" : "M") + x.toFixed(1) + "," + yTop[i].toFixed(1); });
  for (let i = xs.length - 1; i >= 0; i--) d += "L" + xs[i].toFixed(1) + "," + yBot[i].toFixed(1);
  return d + "Z";
}

function linePath(xs, ys) {
  return xs.map((x, i) => (i ? "L" : "M") + x.toFixed(1) + "," + ys[i].toFixed(1)).join("");
}

function donut(rows, metric) {
  const box = el("div");
  const used = (rows || []).filter((row) => metricValue(row, metric) > 0);
  if (!used.length) {
    box.appendChild(el("p", { class: "muted" }, "没有协议数据"));
    return box;
  }
  const total = used.reduce((sum, row) => sum + metricValue(row, metric), 0) || 1;
  const svg = svgEl("svg", { viewBox: "0 0 120 120", class: "donut" });
  const radius = 38;
  const circ = 2 * Math.PI * radius;
  let offset = 0;
  used.forEach((row, index) => {
    const frac = metricValue(row, metric) / total;
    const circle = svgEl("circle", {
      cx: 60, cy: 60, r: radius, fill: "none",
      stroke: seriesColor(row.key, index),
      "stroke-width": 16,
      "stroke-dasharray": (frac * circ).toFixed(2) + " " + circ.toFixed(2),
      "stroke-dashoffset": (-offset).toFixed(2),
      transform: "rotate(-90 60 60)",
    });
    const title = svgEl("title");
    title.textContent = row.key + " " + fmtMetric(metricValue(row, metric), metric);
    circle.appendChild(title);
    circle.style.cursor = "pointer";
    circle.addEventListener("click", () => openFilter("proto", row.key, "flow"));
    svg.appendChild(circle);
    offset += frac * circ;
  });
  svg.appendChild(svgEl("text", { x: 60, y: 58, fill: "#e7f0e9", "font-size": 11, "text-anchor": "middle" }, metric === "bytes" ? "流量" : "连接"));
  svg.appendChild(svgEl("text", { x: 60, y: 72, fill: "#8e9c93", "font-size": 10, "text-anchor": "middle" }, fmtMetric(total, metric)));
  box.appendChild(svg);
  used.forEach((row, index) => {
    const line = el("div", { class: "share" });
    const left = el("button", { class: "link", type: "button" });
    left.appendChild(el("span", { class: "swatch" }));
    left.lastChild.style.background = seriesColor(row.key, index);
    left.appendChild(document.createTextNode(" " + row.key));
    left.addEventListener("click", () => openFilter("proto", row.key, "flow"));
    line.appendChild(left);
    line.appendChild(el("span", null, Math.round(100 * metricValue(row, metric) / total) + "% · " + fmtMetric(metricValue(row, metric), metric)));
    box.appendChild(line);
  });
  return box;
}

function compareBars(title, rows, field, metric) {
  const box = el("div");
  box.appendChild(el("h2", null, title));
  const list = rows || [];
  if (!list.length) {
    box.appendChild(el("p", { class: "muted" }, "没有数据"));
    return box;
  }
  const max = Math.max(...list.map((row) => metricValue(row, metric)), 1);
  for (const row of list) {
    const line = el("div", { class: "rowline" });
    const button = el("button", { class: "link", type: "button" }, row.key);
    button.addEventListener("click", () => openFilter(field, row.key, "flow"));
    line.appendChild(button);
    line.appendChild(el("span", { class: "tag" }, fmtMetric(metricValue(row, metric), metric)));
    const bar = el("div", { class: "bar" });
    const span = document.createElement("span");
    span.style.width = Math.max(2, (metricValue(row, metric) / max) * 100) + "%";
    bar.appendChild(span);
    box.appendChild(line);
    box.appendChild(bar);
  }
  return box;
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
  const map = { domain: "f-domain", app: "f-app", dst_ip: "f-dst", proto: "f-proto", dst_port: "f-port", src_port: "f-port" };
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
  let ports = { rows: [] };
  try {
    [series, domains, apps, ports] = await Promise.all([
      api("/api/series?range=" + state.range),
      api("/api/rank?by=domain&kind=flow&range=" + state.range + "&limit=8"),
      api("/api/rank?by=app&kind=flow&range=" + state.range + "&limit=8"),
      api("/api/rank?by=dst_port&kind=flow&range=" + state.range + "&limit=8"),
    ]);
  } catch (err) {
    onErr(err);
  }
  document.getElementById("chartcard").replaceChildren(el("h2", null, "连接与流量"), chart(series.points || []));
  document.getElementById("topdomains").replaceChildren(rankList("域名", domains.rows || [], "domain", "flow"));
  document.getElementById("topapps").replaceChildren(rankList("应用", apps.rows || [], "app", "flow"));
  document.getElementById("topports").replaceChildren(rankList("目标端口", ports.rows || [], "dst_port", "flow"));
}

async function loadAnalyze() {
  const range = "range=" + encodeURIComponent(state.range);
  const [st, series, protos, domains, apps, ports, ips] = await Promise.all([
    api("/api/status"),
    api("/api/series?" + range + "&kind=flow&by=proto"),
    api("/api/rank?by=proto&kind=flow&" + range + "&limit=6"),
    api("/api/rank?by=domain&kind=flow&" + range + "&limit=8"),
    api("/api/rank?by=app&kind=flow&" + range + "&limit=8"),
    api("/api/rank?by=dst_port&kind=flow&" + range + "&limit=8"),
    api("/api/rank?by=dst_ip&kind=flow&" + range + "&limit=8"),
  ]);
  paintStatus(st);
  analyzeCache = { series: series.series || [], protos, domains, apps, ports, ips };
  paintAnalyze();
}

function paintAnalyze() {
  const data = analyzeCache;
  if (!data) return;
  const metric = state.metric === "hits" ? "hits" : "bytes";
  const trend = document.getElementById("trend");
  trend.replaceChildren(el("h2", null, metric === "bytes" ? "流量趋势" : "连接趋势"));
  const legend = el("div", { class: "legend" });
  const drawn = (data.series || []).filter((item) => item.name && item.name !== "all");
  drawn.forEach((item, index) => {
    const button = el("button", { type: "button" });
    button.appendChild(el("span", { class: "swatch" }));
    button.lastChild.style.background = seriesColor(item.name, index);
    button.appendChild(document.createTextNode(item.name));
    button.addEventListener("click", () => openFilter("proto", item.name, "flow"));
    legend.appendChild(button);
  });
  trend.appendChild(trendChart(drawn.length ? drawn : [{ name: "all", points: (data.series[0] && data.series[0].points) || [] }], metric, { tall: true }));
  if (drawn.length) trend.appendChild(legend);
  const share = document.getElementById("protoshare");
  share.replaceChildren(el("h2", null, "协议占比"));
  share.appendChild(donut(data.protos.rows || [], metric));
  document.getElementById("analyzesum").textContent = "当前范围 " + Math.round(Number(data.protos.hits) || 0) + " 条连接，流量 " + fmtBytes(data.protos.bytes);
  document.getElementById("board-domain").replaceChildren(compareBars("域名", data.domains.rows || [], "domain", metric));
  document.getElementById("board-app").replaceChildren(compareBars("应用", data.apps.rows || [], "app", metric));
  document.getElementById("board-port").replaceChildren(compareBars("目标端口", data.ports.rows || [], "dst_port", metric));
  document.getElementById("board-ip").replaceChildren(compareBars("目标地址", data.ips.rows || [], "dst_ip", metric));
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
  const name = { domain: "域名", app: "应用", dst_ip: "目标地址", proto: "协议", dst_port: "目标端口", src_port: "源端口" }[state.rankBy] || "排行";
  return name + (state.rankKind === "dns" ? " · DNS" : " · 连接");
}

async function loadRank() {
  const params = new URLSearchParams();
  params.set("by", state.rankBy);
  params.set("kind", state.rankKind);
  params.set("range", state.range);
  params.set("limit", "30");
  const proto = document.getElementById("r-proto").value;
  const port = document.getElementById("r-port").value.trim();
  if (proto) params.set("proto", proto);
  if (port) params.set("port", port);
  const [st, data] = await Promise.all([api("/api/status"), api("/api/rank?" + params.toString())]);
  paintStatus(st);
  const sum = document.getElementById("ranksum");
  sum.textContent = "当前范围 " + Math.round(Number(data.hits) || 0) + " 条，流量 " + fmtBytes(data.bytes);
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
  document.getElementById("ownweb").checked = cfg.exclude_own_web !== false;
  document.getElementById("token").value = "";
  document.getElementById("tokenHint").textContent = "留空表示不修改。保存新口令后需要重新登录。";
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
      exclude_own_web: document.getElementById("ownweb").checked,
      token: document.getElementById("token").value,
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
    else if (state.tab === "analyze") await loadAnalyze();
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
  for (const name of ["overview", "analyze", "flows", "rank", "settings"]) {
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
document.getElementById("metric").addEventListener("click", (event) => {
  const button = event.target.closest("button");
  if (!button) return;
  state.metric = button.dataset.metric;
  mark("metric", "metric", state.metric);
  if (analyzeCache) paintAnalyze();
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
document.getElementById("rankform").addEventListener("submit", (event) => {
  event.preventDefault();
  loadRank().catch(onErr);
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
document.getElementById("logout").addEventListener("click", async () => {
  try {
    await api("/api/logout", { method: "POST", body: "{}" });
  } catch (err) {
    onErr(err);
  }
  document.getElementById("login").classList.remove("hidden");
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
