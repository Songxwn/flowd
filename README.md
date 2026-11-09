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

`flowd` 使用主机网络，才能看到宿主机网卡；同时使用主机 PID 命名空间，才能把连接对应到宿主机进程。VictoriaLogs 只监听 `127.0.0.1:9428`。镜像是 `ghcr.io/songxwn/flowd`，数据默认保留 30 天。

网页默认只绑定本机。要给别的机器访问时，设置口令并修改监听地址，然后重启容器：

```yaml
environment:
  FLOWD_LISTEN: 0.0.0.0:8787
  FLOWD_TOKEN: 换成你自己的口令
```

`FLOWD_IFACES` 如果设了，每次启动都会覆盖网页里保存的网卡列表。

## 下载二进制

从 [Releases](https://github.com/Songxwn/flowd/releases) 下载对应系统的 `flowd`。Linux 上需要 root，或者给程序 `CAP_NET_RAW` 和 `CAP_NET_ADMIN`。

```bash
sudo ./flowd_v0.1.0_linux_amd64 -config /etc/flowd/config.yaml
```

配置示例是 `config.example.yaml`。用 systemd 时可以参考 `deploy/flowd.service`。VictoriaLogs 需要另外运行，地址用 `victorialogs.url` 或环境变量 `FLOWD_VL_URL`。

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
