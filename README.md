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

flowd 装成本机程序，用 systemd 开机自启。VictoriaLogs 二选一：本机二进制，或者只用 Docker 跑 VictoriaLogs。版本与上面的 Compose 一致：VictoriaLogs `v1.53.0`，flowd `v0.1.4`。flowd 支持 `x86_64` 和 `aarch64`。不要执行 `go build`。

整套 Compose 和这套二进制不要同时占用 `9428`、`8787`。已经用上一节的 Compose 时，不用再往下做。VictoriaLogs 的两种装法也不要一起占用 `9428`。

### 1. 准备目录

```bash
sudo mkdir -p /etc/flowd /usr/local/bin
```

选本机二进制安装 VictoriaLogs 时，再给它单独的系统账号。只用 Docker 跑 VictoriaLogs 时跳过这一段。

```bash
if ! id victorialogs >/dev/null 2>&1; then
  sudo useradd --system --home-dir /var/lib/victoria-logs --shell /usr/sbin/nologin victorialogs
fi
sudo mkdir -p /var/lib/victoria-logs
sudo chown victorialogs:victorialogs /var/lib/victoria-logs
```

flowd 抓包需要 root，服务单元里已经写成 `User=root`。

### 2. 安装 VictoriaLogs

两种都只对本机开放 `127.0.0.1:9428`，日志保留 30 天。flowd 里的地址都写 `http://127.0.0.1:9428`。

#### 选项 A：本机二进制

它是单个可执行文件，解压后的名字是 `victoria-logs-prod`。装到固定路径 `/usr/local/bin/victoria-logs`，以后换版本只替换这个文件。数据放在 `/var/lib/victoria-logs`。

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

#### 选项 B：只用 Docker 跑 VictoriaLogs

本机已安装 Docker 时，可以不装 VictoriaLogs 二进制。容器里的进程监听 `:9428`，发布到宿主机时只绑定 `127.0.0.1:9428`。数据在 Docker 卷 `vl-data` 里。仓库里的副本是 `deploy/victorialogs-compose.yml`。

写入 `/etc/flowd/victorialogs-compose.yml`：

```yaml
name: flowd-vlogs

services:
  victorialogs:
    image: docker.io/victoriametrics/victoria-logs:v1.53.0
    container_name: victorialogs
    restart: unless-stopped
    command:
      - -storageDataPath=/victoria-logs-data
      - -retentionPeriod=30d
      - -httpListenAddr=:9428
    ports:
      - "127.0.0.1:9428:9428"
    volumes:
      - vl-data:/victoria-logs-data

volumes:
  vl-data:
```

```bash
sudo docker compose -f /etc/flowd/victorialogs-compose.yml pull
sudo docker compose -f /etc/flowd/victorialogs-compose.yml up -d
sudo docker compose -f /etc/flowd/victorialogs-compose.yml ps
curl -fsS http://127.0.0.1:9428/health
echo
```

`restart: unless-stopped` 会在 Docker 守护进程起来后把容器拉起来。本机浏览器打开 <http://127.0.0.1:9428> 是 VictoriaLogs 自己的查询页。flowd 的网页仍是后面的 `8787`。

起不来时看容器日志。`9428` 已被占用，或镜像没拉下来，是常见原因。

```bash
sudo docker logs victorialogs
```

没有 Compose 插件时，用这一条等效命令：

```bash
sudo docker volume create flowd-vl-data
sudo docker run -d --name victorialogs --restart unless-stopped \
  -p 127.0.0.1:9428:9428 \
  -v flowd-vl-data:/victoria-logs-data \
  docker.io/victoriametrics/victoria-logs:v1.53.0 \
  -storageDataPath=/victoria-logs-data \
  -retentionPeriod=30d \
  -httpListenAddr=:9428
```

### 3. 安装 flowd

```bash
FLOWD_VERSION=v0.1.4
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
  exclude_own_web: true
proc:
  enabled: true
  root: /proc
  interval: 2s
filter:
  include_domains: []
  exclude_domains: []
  include_apps: []
  exclude_apps: []
  include_protos: []
  exclude_protos: []
  include_ports: []
  exclude_ports: []
```

```bash
sudo chmod 600 /etc/flowd/config.yaml
```

写入 `/etc/systemd/system/flowd.service`（仓库里的副本是 `deploy/flowd.service`）。VictoriaLogs 用本机二进制时，它排在 `victorialogs.service` 之后启动。VictoriaLogs 用 Docker 时，把下面的 `After=` 改成 `After=network-online.target docker.service`，先确认 `curl http://127.0.0.1:9428/health` 成功，再启动 flowd。

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

flowd 总览里的写入计数会增加。计数不增加时，先看 flowd 日志里的 VictoriaLogs 地址是不是 `http://127.0.0.1:9428`。本机二进制用 `systemctl is-active victorialogs`，Docker 用 `sudo docker ps --filter name=victorialogs`。两边都再执行一次 `curl -fsS http://127.0.0.1:9428/health`。

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

保留期限由 VictoriaLogs 的 `-retentionPeriod=30d` 决定。

本机二进制要改成 7 天或 90 天时，编辑 `victorialogs.service` 里的这个参数，然后：

```bash
sudo systemctl daemon-reload
sudo systemctl restart victorialogs
```

数据在 `/var/lib/victoria-logs`。升级或重装时不要删除这个目录，否则已有日志会丢。

Docker 部署时改 `/etc/flowd/victorialogs-compose.yml` 里的 `-retentionPeriod` 或镜像标签，然后重新创建容器。不要执行 `docker volume rm`，卷 `vl-data` 里是已有日志。用 `docker run` 时对应的卷名是 `flowd-vl-data`。

```bash
sudo docker compose -f /etc/flowd/victorialogs-compose.yml pull
sudo docker compose -f /etc/flowd/victorialogs-compose.yml up -d
```

升级 flowd 时仍从 [Releases](https://github.com/Songxwn/flowd/releases) 下载，换掉程序再启动：

```bash
sudo systemctl stop flowd
sudo install -m 0755 新下载的flowd /usr/local/bin/flowd
sudo systemctl start flowd
```

VictoriaLogs 本机二进制换版本时，压缩包名里的版本号要和 `VL_VERSION` 一致，例如 `victoria-logs-linux-amd64-v1.53.0.tar.gz`。

```bash
sudo systemctl stop victorialogs
sudo install -m 0755 解压出的victoria-logs-prod /usr/local/bin/victoria-logs
sudo systemctl start victorialogs
```

## 网页

- 总览：抓包和写入计数、连接数曲线、域名和应用前几名。
- 流日志：按域名、应用、网卡、地址、协议和端口筛选，也可以切到 DNS 查询。
- 排行：按域名、应用或目标地址，统计连接次数和字节数；也可以改成按 DNS 查询排名。
- 设置：选择网卡，按域名后缀、应用名、协议或端口过滤。协议只接受 tcp、udp、icmp。端口匹配源端口或目标端口，只作用于 tcp 和 udp。排除优先于包含。

点排行里的名字会打开对应的流日志。域名来源有三种：`sni` 是 TLS ClientHello 里的服务器名，`http` 是未加密 HTTP 的 Host，`dns` 是本机看到的 DNS 响应。

## 记录什么

每条连接在空闲约 30 秒、结束，或持续超过 15 秒时写成一条日志。DNS 查询单独记一条。长连接会按导出间隔分成多条。

不保存数据包内容，也不解密 TLS。HTTP/3 往往看不到 SNI，域名来自更早的 DNS 响应。走了加密 DNS 时，这一跳对不上。只统计经过所选网卡的本机收发，不打开混杂模式，不会把同网段其他主机的流量收进来。默认不记录访问 flowd 自己网页的连接：监听在具体地址上时只排除该地址和端口，监听 `0.0.0.0` 或 `::` 时排除本机所有地址上的这个端口。其它主机上的同端口仍会记录。设置里可以关掉这项。改监听地址后要重启，排除才跟着新端口走。

过滤示例：排除域名填 `example.com` 会去掉它和所有子域名。不要只填 `com` 这种过短后缀。排除端口 `853` 会去掉源端口或目标端口是 853 的 TCP、UDP。只保留端口 `443` 时，其它 TCP、UDP 不记录，ICMP 仍由协议列表决定。ICMP 和 ICMPv6 都记为 `icmp`，没有端口号。

## 自动构建

推送到 `main` 或打 `v*` 标签时，GitHub Actions 会跑测试、交叉编译，并把 Linux amd64 / arm64 镜像推到 GHCR。打标签时另外发布各平台二进制。

镜像拉取若返回 denied，到 GitHub 包设置里把 `flowd` 设为 Public。工作流也会尝试把它改成公开。
