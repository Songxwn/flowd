# flowd

在一台 Linux 上采集指定网卡的应用层流量日志：连接两端、域名、进程名、字节数。日志写入 VictoriaLogs，用本机网页查看、过滤和排行。程序可以一直待在后台。

采集只在 Linux 上可用。发布里的 Windows 和 macOS 程序只能打开网页，查询已经写进 VictoriaLogs 的数据。不要在本机执行 `go build`。

## 用 Docker Compose 部署

在 Linux 上安装 Docker 后，进入本目录：

```bash
docker compose pull
docker compose up -d
```

然后在这台机器上打开 <http://127.0.0.1:8787>。到「设置」里勾选网卡并保存，采集马上开始。

`flowd` 使用主机网络，才能看到宿主机网卡；同时使用主机 PID 命名空间，才能把连接对应到宿主机进程。VictoriaLogs 只监听 `127.0.0.1:9428`。镜像是 `ghcr.io/songxwn/flowd`，数据默认保留 30 天。租户默认 `0:0`。要写入别的租户时加上 `FLOWD_VL_ID`，例如 `12:34`。

网页默认只绑定本机。要给别的机器访问时，设置口令并修改监听地址，然后重启容器：

```yaml
environment:
  FLOWD_LISTEN: 0.0.0.0:8787
  FLOWD_TOKEN: 换成你自己的口令
```

`FLOWD_IFACES` 如果设了，每次启动都会覆盖网页里保存的网卡列表。

## 用二进制部署

下面在同一台 Linux 上安装 VictoriaLogs 和 flowd，并用 systemd 开机自启。版本与 Compose 一致：VictoriaLogs `v1.53.0`，flowd `v0.1.1`。支持 `x86_64` 和 `aarch64`。不要执行 `go build`。

Compose 和这套二进制不要同时占用 `9428`、`8787`。已经用 Compose 时，继续用上一节即可。

### 1. 准备账号和目录

VictoriaLogs 用单独的系统账号写数据。flowd 抓包需要 root，服务单元里已经写成 `User=root`。

```bash
if ! id victorialogs >/dev/null 2>&1; then
  sudo useradd --system --home-dir /var/lib/victoria-logs --shell /usr/sbin/nologin victorialogs
fi
sudo mkdir -p /var/lib/victoria-logs /etc/flowd /usr/local/bin
sudo chown victorialogs:victorialogs /var/lib/victoria-logs
```

### 2. 安装 VictoriaLogs

它是单个可执行文件，解压后的名字是 `victoria-logs-prod`。装到固定路径 `/usr/local/bin/victoria-logs`，以后换版本只替换这个文件。数据放在 `/var/lib/victoria-logs`，保留 30 天，只监听本机 `127.0.0.1:9428`。

```bash
VL_VERSION=v1.53.0
case "$(uname -m)" in
  x86_64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "只支持 amd64 和 arm64"; exit 1 ;;
esac

tmp=$(mktemp -d)
curl -fL -o "$tmp/victoria-logs.tar.gz" \
  "https://github.com/VictoriaMetrics/VictoriaLogs/releases/download/${VL_VERSION}/victoria-logs-linux-${ARCH}-${VL_VERSION}.tar.gz"
tar -xzf "$tmp/victoria-logs.tar.gz" -C "$tmp"
sudo install -m 0755 "$tmp/victoria-logs-prod" /usr/local/bin/victoria-logs
rm -rf "$tmp"
/usr/local/bin/victoria-logs -version
```

写入 `/etc/systemd/system/victorialogs.service`（仓库里的副本是 `deploy/victorialogs.service`）：

```ini
[Unit]
Description=VictoriaLogs
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=victorialogs
Group=victorialogs
ExecStart=/usr/local/bin/victoria-logs -storageDataPath=/var/lib/victoria-logs -retentionPeriod=30d -httpListenAddr=127.0.0.1:9428
Restart=on-failure
RestartSec=2
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
```

启动并确认健康检查返回成功：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now victorialogs
sudo systemctl status victorialogs --no-pager
curl -fsS http://127.0.0.1:9428/health
echo
```

本机浏览器也可以打开 <http://127.0.0.1:9428>，那是 VictoriaLogs 自己的查询页。flowd 的网页在后面的 `8787`。

如果起不来，先看 `sudo journalctl -u victorialogs -n 80 --no-pager`。常见原因是 `9428` 已被占用，或 `/var/lib/victoria-logs` 不属于 `victorialogs`。

### 3. 安装 flowd

```bash
FLOWD_VERSION=v0.1.1
case "$(uname -m)" in
  x86_64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "只支持 amd64 和 arm64"; exit 1 ;;
esac

tmp=$(mktemp -d)
curl -fL -o "$tmp/flowd" \
  "https://github.com/Songxwn/flowd/releases/download/${FLOWD_VERSION}/flowd_${FLOWD_VERSION}_linux_${ARCH}"
sudo install -m 0755 "$tmp/flowd" /usr/local/bin/flowd
rm -rf "$tmp"
/usr/local/bin/flowd -version
```

写入 `/etc/flowd/config.yaml`。网卡先留空，等网页里勾选；保存后会写回这个文件。

```yaml
listen: 127.0.0.1:8787
token: ""
victorialogs:
  url: http://127.0.0.1:9428
  id: "0:0"
capture:
  ifaces: []
  idle_timeout: 30s
  export_interval: 15s
proc:
  enabled: true
  root: /proc
  interval: 2s
filter:
  include_domains: []
  exclude_domains: []
  include_apps: []
  exclude_apps: []
```

```bash
sudo chmod 600 /etc/flowd/config.yaml
```

写入 `/etc/systemd/system/flowd.service`（仓库里的副本是 `deploy/flowd.service`）。它排在 VictoriaLogs 之后启动：

```ini
[Unit]
Description=flowd 应用层流量日志
After=network-online.target victorialogs.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/flowd -config /etc/flowd/config.yaml
Restart=on-failure
RestartSec=2
User=root

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now flowd
sudo systemctl status flowd --no-pager
curl -fsS http://127.0.0.1:8787/api/health
echo
```

然后在这台机器上打开 <http://127.0.0.1:8787>，到「设置」里勾选网卡并保存。采集立刻开始，不用再重启。

`curl` 的结果里应有 `"ok": true`。抓包失败时日志里会写需要 root 或 `CAP_NET_RAW`：

```bash
sudo journalctl -u flowd -n 80 --no-pager
```

不用 systemd、只想前台看一眼时：

```bash
sudo /usr/local/bin/flowd -config /etc/flowd/config.yaml
```

### 4. 确认日志已经写入

选好网卡并产生一些访问后，VictoriaLogs 里应能查到 `kind:="flow"` 或 `kind:="dns"`。在 VictoriaLogs 页面执行：

```text
kind:="flow"
```

flowd 总览里的写入计数会增加。计数不增加时，先看 flowd 日志里的 VictoriaLogs 地址是不是 `http://127.0.0.1:9428`，再确认 `systemctl is-active victorialogs` 为 `active`。

### 5. 给别的机器打开网页

VictoriaLogs 继续只监听 `127.0.0.1`。只把 flowd 网页暴露出去，并设置口令。改 `/etc/flowd/config.yaml`：

```yaml
listen: 0.0.0.0:8787
token: 换成你自己的口令
```

```bash
sudo systemctl restart flowd
```

防火墙放行 `8787`，不要放行 `9428`。改 `listen` 必须重启进程；改网卡、过滤条件和 VictoriaLogs 地址可以在网页里保存，立即生效。

环境变量会盖过配置文件里的同名项，适合临时指定：

```bash
sudo FLOWD_VL_URL=http://127.0.0.1:9428 FLOWD_VL_ID=0:0 /usr/local/bin/flowd -config /etc/flowd/config.yaml
```

`FLOWD_LISTEN`、`FLOWD_TOKEN`、`FLOWD_HOSTNAME` 同样有效。`FLOWD_VL_ID` 是 VictoriaLogs 的租户，格式为 `账号:项目`，默认 `0:0`。配置里写成 `id: "0:0"` 或 `id: 0:0` 都可以。写入和查询都会带上对应的 `AccountID` 和 `ProjectID`。网页设置里也可以改，保存后立即生效。不要设置 `FLOWD_IFACES`，它会在每次启动时盖过网页里保存的网卡。

Windows 或 macOS 上的 flowd 不能抓包，只能连到已经在跑的 VictoriaLogs。Linux 上的 `9428` 没有对网卡开放时，用隧道：

```bash
ssh -L 9428:127.0.0.1:9428 user@那台Linux
```

本机配置里的 `victorialogs.url` 仍写 `http://127.0.0.1:9428`，然后运行对应系统的 Release 程序。

### 6. 保留期限、磁盘和升级

保留期限由 VictoriaLogs 的 `-retentionPeriod=30d` 决定。要改成 7 天或 90 天，编辑 `victorialogs.service` 里的这个参数，然后：

```bash
sudo systemctl daemon-reload
sudo systemctl restart victorialogs
```

数据在 `/var/lib/victoria-logs`。升级或重装时不要删除这个目录，否则已有日志会丢。

升级时停掉对应服务，换掉 `/usr/local/bin` 里的文件，再启动。flowd 仍从 [Releases](https://github.com/Songxwn/flowd/releases) 下载。VictoriaLogs 换版本时，压缩包名里的版本号要和 `VL_VERSION` 一致，例如 `victoria-logs-linux-amd64-v1.53.0.tar.gz`。

```bash
sudo systemctl stop flowd
sudo install -m 0755 新下载的flowd /usr/local/bin/flowd
sudo systemctl start flowd

sudo systemctl stop victorialogs
sudo install -m 0755 解压出的victoria-logs-prod /usr/local/bin/victoria-logs
sudo systemctl start victorialogs
```

## 网页

- 总览：抓包和写入计数、连接数曲线、域名和应用前几名。
- 流日志：按域名、应用、网卡、地址、协议筛选，也可以切到 DNS 查询。
- 排行：按域名、应用或目标地址，统计连接次数和字节数；也可以改成按 DNS 查询排名。
- 设置：选择网卡，按域名后缀或应用名包含关系过滤。排除优先于包含。

点排行里的名字会打开对应的流日志。域名来源有三种：`sni` 是 TLS ClientHello 里的服务器名，`http` 是未加密 HTTP 的 Host，`dns` 是本机看到的 DNS 响应。

## 记录什么

每条连接在空闲约 30 秒、结束，或持续超过 15 秒时写成一条日志。DNS 查询单独记一条。长连接会按导出间隔分成多条。

不保存数据包内容，也不解密 TLS。HTTP/3 往往看不到 SNI，域名来自更早的 DNS 响应。走了加密 DNS 时，这一跳对不上。只统计经过所选网卡的本机收发，不打开混杂模式，不会把同网段其他主机的流量收进来。

过滤示例：排除域名填 `example.com` 会去掉它和所有子域名。不要只填 `com` 这种过短后缀。

## 自动构建

推送到 `main` 或打 `v*` 标签时，GitHub Actions 会跑测试、交叉编译，并把 Linux amd64 / arm64 镜像推到 GHCR。打标签时另外发布各平台二进制。

镜像拉取若返回 denied，到 GitHub 包设置里把 `flowd` 设为 Public。工作流也会尝试把它改成公开。
