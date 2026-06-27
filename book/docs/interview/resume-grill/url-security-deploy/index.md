# URL 安全与部署拷打

> 目标：把 URL 下载和服务器部署讲成安全边界、环境差异和真实排障，不把第一层校验吹成生产级能力。

### 1. URL 下载为什么是安全问题，不只是功能问题？

- 题目：SSRF 风险。
- 面试官想听什么：服务端代用户访问 URL，本质上打开了网络访问面。
- 简答：URL 下载不是简单调用 yt-dlp。用户输入会让服务器主动访问外部网络，如果不校验，可能访问内网、localhost、云 metadata、带 token 的私有链接，或者拉超大资源拖垮服务。
- 深答：

  <details>
  <summary>展开深答</summary>

  本地上传是用户把文件传给后端，风险主要是文件大小和格式；URL 下载则是后端拿着服务器网络权限去访问用户给的地址。攻击者可能传 `http://127.0.0.1`、内网 IP、DNS 指向内网的域名，或者利用跳转链绕过第一层校验。

  VidLens 当前把 URL 下载任务化之前，也同步补了第一层安全校验：scheme、host、白名单、DNS 私网 IP 检查、sanitized URL 入库和日志脱敏。面试里要把它说成“第一层防护”，不要说完整生产级 SSRF 沙箱。
  </details>

- 延伸追问：
  - SSRF 能造成什么后果？
  - yt-dlp 自己会不会防？
  - URL 下载和普通文件上传安全重点有什么不同？
- 项目证据：
  - `internal/service/media.go:150` `UploadByURL` 入口。
  - `internal/service/media.go:152` 先校验 URL。
  - `docs/troubleshooting-and-interview-notes.md:2741` URL 下载安全记录。
  - `docs/troubleshooting-and-interview-notes.md:2783` SSRF 风险根因。
- 当前边界：当前不是浏览器级或容器沙箱级隔离，仍需后续 hardening。

### 2. 当前 URL 校验具体做了哪些层？

- 题目：实现细节。
- 面试官想听什么：能按代码顺序讲清校验链。
- 简答：先 parse URL，限制 http/https；拒绝空 host 和 localhost；按允许平台域名白名单匹配；DNS 解析 host 后拒绝 private、loopback、link-local、multicast 等地址；最后返回 sanitized URL，去掉 userinfo、query、fragment。
- 深答：

  <details>
  <summary>展开深答</summary>

  `RemoteVideoURLValidator` 的顺序很关键。先做语法和 scheme，避免 file、ftp、gopher 这类协议；再检查 host 不能为空、不能是 localhost；然后做允许域名匹配，比如 YouTube、B 站这类平台；之后解析 DNS，任何解析结果落到私网或特殊地址都拒绝。

  最后入库的不是原始 URL，而是 sanitized URL。这样即使用户链接里有 `?token=`、分享参数或 fragment，也不会被存进数据库和日志。这个设计解决的是安全和排障之间的平衡：能知道用户提交的是哪个公开视频平台，但不保存敏感 query。
  </details>

- 延伸追问：
  - 白名单是精确匹配还是后缀匹配？
  - DNS 返回多个 IP 怎么处理？
  - 为什么 query 不入库？
- 项目证据：
  - `internal/service/remote_video_url.go:52` validate 入口。
  - `internal/service/remote_video_url.go:68` 拒绝 localhost。
  - `internal/service/remote_video_url.go:71` host allowlist。
  - `internal/service/remote_video_url.go:94` 返回 sanitized URL。
  - `internal/service/remote_video_url.go:126` sanitized URL 实现。
- 当前边界：白名单和 DNS 检查只覆盖下载前入口，redirect-chain 仍需更强校验。

### 3. 为什么不能说 URL 下载已经生产级安全？

- 题目：安全边界。
- 面试官想听什么：知道 SSRF 防护难点，不乱吹。
- 简答：因为 yt-dlp 内部可能跟随重定向，DNS 也有 rebinding 风险；当前还缺硬下载大小/时间限制、redirect-chain 校验、用户级 cookies 加密管理、网络出口隔离和完整审计策略。
- 深答：

  <details>
  <summary>展开深答</summary>

  入口校验通过，只能说明“用户提交的原始 URL”符合规则。但下载工具后续可能访问重定向后的 URL，也可能解析到不同 IP。DNS rebinding 场景下，第一次解析是公网 IP，后续访问时可能变成内网 IP。只做一次 DNS check 不足以覆盖所有 SSRF 绕过。

  生产级还要限制资源消耗：最大下载时长、最大文件大小、速率、content-type、临时文件清理、cookies 和 proxy 的权限边界。VidLens 当前做了第一层安全和 720p 限制，所以面试里要诚实说“能挡住明显危险输入，但不是完整生产级 URL 下载沙箱”。
  </details>

- 延伸追问：
  - redirect-chain 怎么验证？
  - DNS rebinding 怎么防？
  - 为什么容器网络隔离也重要？
- 项目证据：
  - `docs/troubleshooting-and-interview-notes.md:2864` DNS 检查不足以覆盖 yt-dlp redirects。
  - `docs/troubleshooting-and-interview-notes.md:2872` 当前 URL 下载安全限制。
  - `AGENTS.md:123` URL 下载 SSRF 是高优先级 future work。
- 当前边界：不要在简历里写“生产级 SSRF 防护”，只能写“第一层 URL 下载安全校验”。

### 4. `source_url` 为什么要脱敏？去掉 query 会不会有问题？

- 题目：日志和数据最小化。
- 面试官想听什么：入库字段要避免保存用户 token。
- 简答：很多分享链接 query 里可能带追踪参数、临时 token 或用户标识。VidLens 入库 `source_url` 保存 sanitized 版本，去掉 userinfo、query、fragment，降低日志和数据库泄露风险。代价是某些依赖 query 的链接可能无法用于后续复现。
- 深答：

  <details>
  <summary>展开深答</summary>

  URL 下载的真实下载发生在任务创建后，consumer 拿原始 URL 下载；但数据库里长期保留的 `source_url` 没必要保存完整敏感参数。尤其公开部署中，日志、数据库备份、错误追踪都有可能被更多人接触，少保存就是少泄露。

  去掉 query 的确有代价。有些平台链接的 `v=`、临时授权或 playlist 信息可能在 query 里。当前选择是：下载流程使用校验后的原始 URL，持久化展示用 sanitized URL。面试里可以说这是安全优先的折中，后续如果要支持私有链接，需要用户级加密 cookies/token，而不是把敏感 URL 明文存库。
  </details>

- 延伸追问：
  - YouTube 的 `v=` 去掉后还能展示吗？
  - 原始 URL 是否会进日志？
  - 私有视频怎么支持？
- 项目证据：
  - `internal/service/media.go:167` `SourceURL` 使用 checked sanitized URL。
  - `internal/service/remote_video_url.go:94` validator 返回 sanitized。
  - `docs/troubleshooting-and-interview-notes.md:2810` 记录 sanitized SourceURL。
  - `docs/troubleshooting-and-interview-notes.md:2868` 去 query 可能影响部分链接。
- 当前边界：当前没有完整用户级私有链接凭证管理。

### 5. B 站 412 怎么定位，为什么不是简单后端 bug？

- 题目：第三方平台反爬排障。
- 面试官想听什么：能区分后端逻辑错误和外部平台策略。
- 简答：B 站 412 是平台反爬/风控响应，不能只看后端 500。排查时要在服务器环境直接跑 yt-dlp，看是否同样失败，再检查 cookies、User-Agent、systemd PATH 和工具版本。
- 深答：

  <details>
  <summary>展开深答</summary>

  URL 下载失败时，第一反应不能是改业务代码。VidLens 遇到 B 站 412 时，排查路径是：确认后端调用 yt-dlp 的命令、确认 systemd 服务里能找到 yt-dlp、再在服务器上用同一个 URL 直接执行 yt-dlp。结果说明平台返回 412，属于反爬或 cookies 问题。

  后端能做的是把错误包装得更清楚，提示需要 cookies 或平台限制，而不是让用户看到模糊的下载失败。面试里可以把它讲成“我用命令行复现把问题边界从后端代码缩小到第三方平台访问策略”。
  </details>

- 延伸追问：
  - cookies 怎么配置才安全？
  - 为什么本地能下服务器不能下？
  - 遇到第三方平台变化怎么监控？
- 项目证据：
  - `internal/pkg/ytdlp/ytdlp.go:54` 支持 cookies 参数。
  - `internal/pkg/ytdlp/ytdlp.go:64` BiliBili 412 错误包装。
  - `README.md:146` B 站 412/cookies 说明。
  - `docs/troubleshooting-and-interview-notes.md:980` B 站 412 口语化回答。
- 当前边界：当前 cookies 是部署配置级能力，不是完善的用户级 cookies 管理。

### 6. YouTube 为什么只给 yt-dlp 配 proxy，不做全局代理？

- 题目：部署网络边界。
- 面试官想听什么：代理应该局部作用，避免污染数据库/中间件/AI 访问。
- 简答：YouTube 下载可能需要代理，但 MySQL、Redis、MinIO、Milvus 和 AI API 不应该被同一个代理影响。VidLens 把 `tools.proxy_url` 只传给 yt-dlp，降低部署环境变量对其他依赖的副作用。
- 深答：

  <details>
  <summary>展开深答</summary>

  服务器上本地 shell 和 systemd 服务不是同一个环境。shell 里可能有代理变量，systemd service 默认没有；即使设置全局代理，也可能让数据库、对象存储或内网服务访问异常。更稳的方式是把代理作为工具配置，只在 yt-dlp 命令参数里生效。

  这也方便排障：如果 YouTube 失败，就检查 yt-dlp + proxy；如果 Milvus 或 MySQL 出问题，就不会被全局 HTTP_PROXY 干扰。面试里可以说这是部署隔离意识，而不是简单“加个代理”。
  </details>

- 延伸追问：
  - systemd 为什么读不到 shell 代理？
  - AI API 要不要走代理？
  - proxy_url 会不会泄露？
- 项目证据：
  - `internal/pkg/ytdlp/ytdlp.go:57` yt-dlp 命令支持 proxy。
  - `README.md:148` proxy 只传给 yt-dlp。
  - `docs/troubleshooting-and-interview-notes.md:1096` YouTube 代理排查回答。
  - `docs/troubleshooting-and-interview-notes.md:1097` 说明不影响 DB/MinIO/Milvus/AI。
- 当前边界：当前没有按用户配置代理，也没有代理可用性的自动探测。

### 7. 为什么限制 720p，不让 yt-dlp 默认拉最高画质？

- 题目：资源控制。
- 面试官想听什么：业务目标是理解内容，不是高清视频存储。
- 简答：VidLens 关注 ASR、摘要和 RAG，通常不需要 4K 原视频。限制最高 720p 能减少下载时间、磁盘占用、MinIO 存储和后续处理压力，也避免 URL 下载接口因为超大视频长期占用资源。
- 深答：

  <details>
  <summary>展开深答</summary>

  yt-dlp 默认可能选择很高清的格式，但对视频理解来说，ASR 主要依赖音频，RAG 依赖转写文本。拉 4K 视频不仅浪费存储，还会让下载、上传到 MinIO、MD5 计算、临时文件清理都变慢。

  720p 是一个实用上限：保留基本可预览质量，同时控制资源。面试里可以说这不是性能优化口号，而是从真实 YouTube 下载阻塞问题里收敛出来的限制。
  </details>

- 延伸追问：
  - 只下载音频行不行？
  - 用户想看高清原视频怎么办？
  - 720p 是不是写死？
- 项目证据：
  - `internal/pkg/ytdlp/ytdlp.go:49` yt-dlp 格式限制 720p。
  - `README.md:148` 说明 720p 避免拉 4K 源视频。
  - `docs/troubleshooting-and-interview-notes.md:1044` YouTube 默认格式过大根因。
  - `docs/troubleshooting-and-interview-notes.md:1108` 720p 对业务足够。
- 当前边界：当前还没有按视频时长、文件大小和用户等级做动态策略。

### 8. Milvus 端口监听为什么不代表部署 ready？

- 题目：中间件 readiness。
- 面试官想听什么：容器 running 和端口 open 不是服务可用。
- 简答：Milvus Standalone 依赖 Proxy、DataCoord、QueryCoord、IndexNode、etcd 和对象存储。端口监听只说明进程开了 socket，内部组件或 MinIO 凭证错误仍会让客户端连接失败。
- 深答：

  <details>
  <summary>展开深答</summary>

  部署时遇到过 Milvus 容器 running、19530 端口也监听，但后端初始化仍提示 Proxy not ready。继续看 Milvus 日志，发现它访问 MinIO 的 access key 不对。根因是 compose 环境变量名和 Milvus 2.4.15 实际读取的变量名不一致，配置被忽略。

  修复时没有重建全部中间件，而是只修 Milvus 配置、重建 Milvus 容器、等待日志稳定后重启后端。这段经历适合面试里讲：部署验证不能只看端口，要看依赖日志、readiness 和应用侧初始化结果。
  </details>

- 延伸追问：
  - 为什么不清空 Milvus 数据目录？
  - 后端连不上 Milvus 是否应该启动失败？
  - readiness 应该怎么做？
- 项目证据：
  - `cmd/server/main.go:135` Milvus 初始化使用超时。
  - `cmd/server/main.go:147` Milvus 失败时降级继续启动。
  - `docs/troubleshooting-and-interview-notes.md:781` Milvus MinIO 凭证变量根因。
  - `docs/troubleshooting-and-interview-notes.md:830` Milvus 部署口语化回答。
- 当前边界：当前是单机 Milvus Standalone，没有生产级 Milvus 集群运维和独立 healthcheck。

