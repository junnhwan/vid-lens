/* 映知 VidLens · 摘要体验原型 · 演示数据
   全部为虚构内容，视频号、UP 主、截图均为演示素材，不对应真实 B 站视频。 */
(function () {
  const A = 'assets/'

  const mainDoc = {
    title: 'PostgreSQL 连接池实战：PgBouncer 从配置、压测到 3 类常见报错排查（含 transaction 模式避坑）',
    mapTitle: 'PgBouncer 连接池',
    resolvedMode: 'image_text',
    modeReason: '视频包含配置文件、终端输出和图表，Agent 选择了图文',
    overview: '视频演示了在 PostgreSQL 前部署 PgBouncer 的完整过程：先解释短连接为什么会拖垮数据库，再给出一份可直接使用的最小配置，比较三种 pool_mode，用 pgbench 压测确定池大小，最后排查上线后最常见的三类报错。',
    keyPoints: [
      'Web 服务优先使用 **transaction** 模式：少量数据库连接即可服务大量客户端，但会话级特性会失效。',
      '池大小从「CPU 核数 × 2」起步，以压测为准。视频中 8 核数据库在 20 个连接附近达到峰值，继续加连接反而变慢。',
      '「prepared statement does not exist」是最常见的上线问题：1.21+ 开启 `max_prepared_statements`，旧版本在驱动侧关闭服务端预处理。'
    ],
    fit: '适合连接数经常打满、出现 too many clients 报错的后端服务；不能替代慢查询优化。',
    blocks: [
      {
        id: 'c1', title: '为什么需要连接池', short: '为什么需要连接池', start: 42, end: 230,
        paras: [
          { id: 'c1p1', t: 64, md: 'PostgreSQL 为每个客户端连接启动一个独立的后端进程。视频中的 Web 服务每个请求都新建连接，高峰期连接数接近 `max_connections`（默认 100），数据库开始返回 `sorry, too many clients already`。' },
          { id: 'c1p2', t: 121, md: '连接池放在应用与数据库之间：应用仍然可以开很多**客户端连接**，PgBouncer 只用少量**服务端连接**真正连到 PostgreSQL，并在请求之间复用它们。' },
          { id: 'c1p3', t: 196, md: '> 视频特别提醒：连接池解决的是「连接太多」，不能替代慢查询优化。查询本身慢，加连接池也不会变快。' }
        ],
        figures: [{ id: 'f1', after: 'c1p2' }],
        concepts: [
          { id: 'k11', label: '每个连接一个后端进程', para: 'c1p1' },
          { id: 'k12', label: '客户端连接 vs 服务端连接', para: 'c1p2' },
          { id: 'k13', label: '不能替代慢查询优化', para: 'c1p3' }
        ]
      },
      {
        id: 'c2', title: '安装与最小可用配置', short: '安装与最小配置', start: 230, end: 570,
        paras: [
          { id: 'c2p1', t: 251, md: '演示环境是 Ubuntu 22.04，用系统包安装 PgBouncer，配置文件位于 `/etc/pgbouncer/pgbouncer.ini`。视频给出的最小配置包含四部分：\n\n- `[databases]`：把逻辑库名 `appdb` 指向真实数据库地址；\n- `listen_port = 6432`：应用改连这个端口；\n- `auth_type = scram-sha-256` 与 `auth_file`：口令与数据库保持一致；\n- `pool_mode = transaction`、`default_pool_size = 20`、`max_client_conn = 500`。' },
          { id: 'c2p2', t: 448, md: '改完配置后执行 `systemctl reload pgbouncer` 生效，再用 `psql -h 127.0.0.1 -p 6432 -U app appdb` 验证能连通。应用侧只需要把端口从 5432 改成 6432。' }
        ],
        figures: [{ id: 'f2', after: 'c2p1' }],
        concepts: [
          { id: 'k21', label: '[databases] 映射', para: 'c2p1' },
          { id: 'k22', label: '端口 6432', para: 'c2p1' },
          { id: 'k23', label: 'scram-sha-256 认证', para: 'c2p1' },
          { id: 'k24', label: 'reload 后用 psql 验证', para: 'c2p2' }
        ]
      },
      {
        id: 'c3', title: '选对 pool_mode', short: '选对 pool_mode', start: 570, end: 785,
        paras: [
          { id: 'c3p1', t: 588, md: 'PgBouncer 有三种模式，区别在于服务端连接什么时候还回池里：\n\n- **session**：客户端断开才归还，最兼容，但几乎不省连接；\n- **transaction**：事务结束即归还，Web 服务的推荐选择；\n- **statement**：每条语句后归还，不允许多语句事务，很少使用。' },
          { id: 'c3p2', t: 731, md: 'transaction 模式下，同一个客户端的两个事务可能落在不同的服务端连接上，依赖会话状态的写法会出问题：会话级 `SET`、`LISTEN/NOTIFY`、会话级 advisory lock、跨事务的临时表，以及服务端预处理语句。视频建议上线前先在代码里搜一遍这些用法。' }
        ],
        figures: [{ id: 'f3', after: 'c3p1' }],
        concepts: [
          { id: 'k31', label: 'session · 最兼容', para: 'c3p1' },
          { id: 'k32', label: 'transaction · 推荐', para: 'c3p1' },
          { id: 'k33', label: 'statement · 少用', para: 'c3p1' },
          { id: 'k34', label: '会话级特性失效', para: 'c3p2' }
        ]
      },
      {
        id: 'c4', title: '池大小怎么定：用 pgbench 压测', short: '池大小与压测', start: 785, end: 1100,
        paras: [
          { id: 'c4p1', t: 802, md: '视频用 PostgreSQL wiki 的经验公式起步：`连接数 ≈ CPU 核数 × 2 + 有效磁盘数`。演示数据库是 8 核 SSD，起点约为 17～20。' },
          { id: 'c4p2', t: 905, md: '随后用 `pgbench -c 200 -j 8 -T 60` 模拟 200 个客户端，逐步调整 `default_pool_size`：TPS 在 20 左右达到峰值（约 4,100），提高到 60 后反而降到约 3,300，因为数据库开始在锁竞争和上下文切换上消耗时间。' },
          { id: 'c4p3', t: 1062, md: '结论：池大小以压测为准。客户端多不代表服务端连接要多，让请求在 PgBouncer 排队，通常比把数据库压垮更好。' }
        ],
        figures: [{ id: 'f4', after: 'c4p2' }],
        concepts: [
          { id: 'k41', label: '核数 × 2 起步', para: 'c4p1' },
          { id: 'k42', label: 'pgbench 逐步加压', para: 'c4p2' },
          { id: 'k43', label: '峰值约在 20', para: 'c4p2' }
        ]
      },
      {
        id: 'c5', title: '三类常见报错排查', short: '三类常见报错', start: 1100, end: 1390,
        paras: [
          { id: 'c5p1', t: 1118, md: '**① no more connections allowed (max_client_conn)**：客户端连接数超过了 PgBouncer 的上限。调大 `max_client_conn`，同时确认系统文件描述符上限（`ulimit -n`）足够，否则会换成另一种连接失败。' },
          { id: 'c5p2', t: 1195, md: '**② prepared statement "S_1" does not exist**：transaction 模式下，驱动在一个服务端连接上准备了语句，下一次执行却落到另一个连接。视频给出两种处理：PgBouncer 1.21 及以上版本设置 `max_prepared_statements = 100`；版本较旧时，在驱动侧关闭服务端预处理，例如 JDBC 加 `prepareThreshold=0`，pgx 使用 `default_query_exec_mode=simple_protocol`。' },
          { id: 'c5p3', t: 1236, md: '**③ 请求变慢、偶发 query_wait_timeout**：连到管理库执行 `SHOW POOLS;`。如果 `cl_waiting` 持续大于 0，而 `sv_active` 已经等于池大小，说明池被占满：先查是否有长事务或连接泄漏，再考虑调大池。' }
        ],
        figures: [{ id: 'f5', after: 'c5p1' }, { id: 'f6', after: 'c5p3' }],
        concepts: [
          { id: 'k51', label: 'max_client_conn 超限', para: 'c5p1' },
          { id: 'k52', label: '预处理语句失效', para: 'c5p2' },
          { id: 'k53', label: '池满排队 · SHOW POOLS', para: 'c5p3' }
        ]
      },
      {
        id: 'c6', title: '上线前检查清单', short: '上线检查清单', start: 1390, end: 1476,
        paras: [
          { id: 'c6p1', t: 1398, md: '- 应用连接串已改到 6432 端口，健康检查也经过 PgBouncer；\n- 代码中没有依赖会话状态的写法，或已改为事务内的 `SET LOCAL`；\n- 预处理语句已按版本选择处理方式；\n- `max_client_conn` 与 `ulimit -n` 匹配预估的客户端数量；\n- 监控 `SHOW POOLS` 的 `cl_waiting` 与 `maxwait`，并设置告警。' },
          { id: 'c6p2', t: 1452, md: '视频最后提醒：PgBouncer 是单线程的，单实例 CPU 打满时，需要部署多个实例或开启 `so_reuseport`。' }
        ],
        figures: [],
        concepts: [
          { id: 'k61', label: '连接串与健康检查', para: 'c6p1' },
          { id: 'k62', label: '监控 cl_waiting', para: 'c6p1' },
          { id: 'k63', label: '单线程 · 多实例', para: 'c6p2' }
        ]
      }
    ],
    figures: {
      f1: { src: A + 'fig-architecture.svg', t: 138, caption: '视频中的部署结构：3 台应用服务器共 600 个客户端连接，经 PgBouncer 后只占用 20 个数据库连接。', alt: '架构图：三台 App 服务器连接 PgBouncer，PgBouncer 再连接 PostgreSQL', supports: '画面给出了字幕没有读出的连接数对比' },
      f2: { src: A + 'fig-config.svg', t: 372, caption: 'pgbouncer.ini 的关键配置。画面中 `default_pool_size` 为 20、`max_client_conn` 为 500，字幕只读出了前者。', alt: '代码编辑器中的 pgbouncer.ini，高亮池参数', supports: '补充字幕中缺失的 max_client_conn 取值' },
      f3: { src: A + 'fig-poolmode.svg', t: 700, caption: '三种 pool_mode 的对比表。高亮的 transaction 列标出了不可用的会话级特性。', alt: '对比表：session、transaction、statement 三种模式的特性', supports: '对比表比口述更完整' },
      f4: { src: A + 'fig-pgbench.svg', t: 965, caption: 'pgbench 压测结果：横轴为 default_pool_size，纵轴为 TPS，峰值出现在 20 附近。', alt: '折线图：TPS 随池大小先升后降，峰值在 20', supports: '图中的具体数值支撑「池越大不一定越快」' },
      f5: { src: A + 'fig-error.svg', t: 1142, caption: '应用日志中的报错：发布约 3 分钟后出现，此时客户端连接正好打到 500。', alt: '终端日志，高亮 no more connections allowed 报错', supports: '画面显示了报错时间和连接数' },
      f6: { src: A + 'fig-showpools.svg', t: 1247, caption: '管理控制台中的 SHOW POOLS 输出：`cl_waiting` 为 37，`sv_active` 为 20，说明请求在排队。', alt: 'psql 终端中的 SHOW POOLS 表格，高亮 cl_waiting 与 sv_active', supports: '用真实输出说明如何判断池满' }
    }
  }

  const mainCues = [
    [0, '大家好，这期我们把 PgBouncer 从零配到能上线。'], [18, '先说结论，Web 服务基本都选 transaction 模式。'],
    [42, '先看为什么需要连接池。'], [64, 'PostgreSQL 每来一个连接，就会 fork 一个后端进程。'], [92, '我们这个服务每个请求都新建连接，高峰的时候直接就打满了。'],
    [121, '连接池的思路是，应用那边可以开很多连接，'], [138, '但真正连到数据库的只有这二十个。'], [168, '这个图大家可以截一下。'], [196, '但是注意，连接池不是用来解决慢查询的。'],
    [230, '好，接下来装 PgBouncer。'], [251, '我这里是 Ubuntu 22.04，直接 apt 安装。'], [300, 'databases 这一段，把 appdb 指到真实的数据库地址。'], [340, '认证方式用 scram-sha-256，和数据库保持一致。'],
    [372, '池参数这里，default_pool_size 先填 20。'], [410, 'admin_users 记得加上，后面排查要用。'], [448, '改完 reload 一下，用 psql 连 6432 试试。'], [520, '应用那边只改端口就可以了。'],
    [570, '然后是最容易踩坑的 pool_mode。'], [588, '一共三种模式，session、transaction、statement。'], [640, 'session 模式最兼容，但基本省不了连接。'], [700, '大家看这张表，transaction 这一列红色的都要注意。'],
    [731, '因为两个事务可能落在不同的连接上，会话状态就丢了。'], [760, '上线前在代码里搜一下 SET、LISTEN 这些关键字。'],
    [785, '池子到底设多大？'], [802, '先用 wiki 上的公式，核数乘二加磁盘数。'], [860, '我们来压测验证一下。'], [905, 'pgbench 两百个客户端，跑六十秒。'], [965, '看这个曲线，二十左右就到顶了。'],
    [1010, '开到六十反而掉到三千三左右。'], [1062, '所以排队比把数据库压垮要好。'],
    [1100, '最后讲上线之后最常见的三个报错。'], [1118, '第一个，no more connections allowed。'], [1142, '这是我们发布三分钟后的日志，客户端刚好五百。'], [1170, '除了调大 max_client_conn，还要看 ulimit。'],
    [1195, '第二个，prepared statement S_1 does not exist。'], [1212, '1.21 以上可以开 max_prepared_statements。'], [1224, '老版本就在驱动里关掉服务端预处理。'],
    [1236, '第三个，请求变慢，甚至 query_wait_timeout。'], [1247, '连管理库执行 SHOW POOLS，看 cl_waiting。'], [1290, '三十七个在等，sv_active 已经是二十，池满了。'], [1330, '先查长事务和连接泄漏，别急着加大。'],
    [1390, '最后给一个上线检查清单。'], [1420, '监控 cl_waiting 和 maxwait，配上告警。'], [1452, 'PgBouncer 是单线程，CPU 打满就多开几个实例。'], [1468, '这期就到这里，我们下期见。']
  ].map(([t, text]) => ({ t, text }))

  /* 预置修订：按段落 id。steps = 改成可照着操作的步骤；concise = 更简洁。 */
  const revisions = {
    steps: {
      c5p2: {
        md: '**② prepared statement "S_1" does not exist**\n\n按下面顺序处理：\n\n1. 确认版本：执行 `pgbouncer --version`。\n2. 1.21 及以上：在 `pgbouncer.ini` 中设置 `max_prepared_statements = 100`，执行 `systemctl reload pgbouncer`。\n3. 版本较旧：在驱动侧关闭服务端预处理。JDBC 在连接串中加 `prepareThreshold=0`；pgx 使用 `default_query_exec_mode=simple_protocol`。\n4. 重新发布应用，观察日志确认报错不再出现。',
        concept: { id: 'k52', label: '预处理语句 · 4 步处理' }
      },
      c5p1: {
        md: '**① no more connections allowed (max_client_conn)**\n\n1. 在 `pgbouncer.ini` 中调大 `max_client_conn`，略高于预估的客户端连接总数。\n2. 执行 `ulimit -n`，确认文件描述符上限大于 `max_client_conn` 加服务端连接数。\n3. 不够时在 systemd 单元中设置 `LimitNOFILE`，然后重启 PgBouncer。',
        concept: { id: 'k51', label: 'max_client_conn · 3 步处理' }
      },
      c5p3: {
        md: '**③ 请求变慢、偶发 query_wait_timeout**\n\n1. 连接管理库：`psql -p 6432 -U pgbouncer pgbouncer`。\n2. 执行 `SHOW POOLS;`，看 `cl_waiting` 是否持续大于 0、`sv_active` 是否等于池大小。\n3. 两者都成立时，先在数据库查长事务和未释放的连接。\n4. 排除泄漏后，再小步调大 `default_pool_size` 并重新压测。',
        concept: { id: 'k53', label: '池满排队 · 4 步排查' }
      },
      c2p1: {
        md: '演示环境是 Ubuntu 22.04。按下面步骤完成最小配置：\n\n1. 安装：`sudo apt install pgbouncer`。\n2. 编辑 `/etc/pgbouncer/pgbouncer.ini`，在 `[databases]` 中加入 `appdb = host=10.0.2.15 port=5432 dbname=appdb`。\n3. 设置 `listen_port = 6432`、`auth_type = scram-sha-256`、`auth_file = /etc/pgbouncer/userlist.txt`。\n4. 填写池参数：`pool_mode = transaction`、`default_pool_size = 20`、`max_client_conn = 500`。',
        concept: { id: 'k21', label: '4 步完成最小配置' }
      }
    },
    concise: {
      overview: { md: '在 PostgreSQL 前部署 PgBouncer：用 transaction 模式、按压测确定池大小，并提前处理三类常见报错。' },
      c1p1: { md: 'PostgreSQL 每个连接对应一个后端进程，短连接多了会打满 `max_connections`（默认 100）。' },
      c3p2: { md: 'transaction 模式会丢失会话状态：会话级 `SET`、`LISTEN`、advisory lock、跨事务临时表和服务端预处理语句都要提前排查。' },
      c4p2: { md: '`pgbench -c 200` 压测显示：TPS 在池大小 20 附近达到峰值（约 4,100），增大到 60 反而降到约 3,300。' }
    }
  }

  /* 后台再次生成的新原稿：只改动了 c4 图注与一个段落，用于演示“不覆盖用户版本” */
  const regenerated = {
    note: '字幕重新校对后，后台生成了新的摘要原稿：第 4 章补充了压测环境说明。',
    patch: { c4p1: '视频用 PostgreSQL wiki 的经验公式起步：`连接数 ≈ CPU 核数 × 2 + 有效磁盘数`。演示数据库是 8 核 SSD、16 GB 内存，起点约为 17～20。' }
  }

  /* 预置问答 */
  const answers = [
    {
      topic: 'prepared', match: /prepared|预处理|S_1|statement.*exist|JDBC|prepareThreshold|驱动/i,
      text: '这个报错的原因是：transaction 模式下，驱动在一个服务端连接上准备了语句，下一次执行被分配到另一个连接，那里并没有这条预处理语句。{{t:1195}}\n\n视频给出的处理方式按版本区分：\n\n1. **PgBouncer 1.21 及以上**：在 `pgbouncer.ini` 中设置 `max_prepared_statements = 100`，由 PgBouncer 跟踪并在需要时重新准备语句。{{t:1212}}\n2. **旧版本**：在驱动侧关闭服务端预处理，例如 JDBC 的 `prepareThreshold=0`，pgx 的 `default_query_exec_mode=simple_protocol`。{{t:1224}}\n\n如果可以升级，优先用第一种：应用代码不用改，也能保留预处理带来的性能。',
      follow: '视频没有逐项讲每个驱动，这里按视频提到的两个驱动说明：\n\n- **Java / JDBC**：在连接串末尾加 `?prepareThreshold=0`，例如 `jdbc:postgresql://pgb:6432/appdb?prepareThreshold=0`。\n- **Go / pgx v5**：连接串加 `default_query_exec_mode=simple_protocol`，或在代码中设置 `config.DefaultQueryExecMode`。\n\n改完后重新发布，并在日志中确认不再出现 `S_1 does not exist`。{{t:1224}}\n\n注意：这部分驱动参数来自视频口述，其他驱动的写法视频没有覆盖，需要查对应文档。',
      suggestions: ['那这个怎么设置？', '升级到 1.21 需要注意什么？', '怎么确认问题已经解决？']
    },
    {
      topic: 'poolsize', match: /池大小|pool_size|多大|设多少|default_pool|连接数.*(多少|设置)|核数/i,
      text: '视频的建议是「公式起步，压测定值」：\n\n1. 起点用 `CPU 核数 × 2 + 有效磁盘数`，8 核 SSD 约为 17～20。{{t:802}}\n2. 用 `pgbench -c 200 -j 8 -T 60` 逐步调整 `default_pool_size`。演示中 20 左右 TPS 最高（约 4,100），60 时降到约 3,300。{{t:965}}\n\n所以在类似配置下，**20 是合理起点**。客户端更多时，应提高 `max_client_conn` 让请求排队，而不是盲目加大池。',
      follow: '视频压测的是 8 核 SSD 的数据库，换成你的环境需要重新测：\n\n1. 用与线上相近的数据量准备库；\n2. 从公式值开始，每次增减 5，跑 60 秒 pgbench；\n3. 记录 TPS 和 p95 延迟，取 TPS 接近峰值、延迟最稳定的那个值。{{t:905}}\n\n视频没有给出读写混合场景的结果，这一点需要你自己验证。',
      suggestions: ['那这个怎么设置？', 'cl_waiting 一直大于 0 怎么办？']
    },
    {
      topic: 'mode', match: /transaction|session|模式|pool_mode|SET|LISTEN|限制|会话/i,
      text: 'transaction 模式下，事务结束就归还服务端连接，同一客户端的下一个事务可能用的是另一个连接。{{t:731}} 因此这些写法会出问题：\n\n- 会话级 `SET`（改用事务内的 `SET LOCAL`）；\n- `LISTEN / NOTIFY`；\n- 会话级 advisory lock；\n- 跨事务使用的临时表；\n- 服务端预处理语句（1.21+ 可用 `max_prepared_statements` 处理）。\n\n视频的对比表中，这些都在 transaction 列标红。{{t:700}}',
      follow: '可以按视频的建议，上线前在代码里搜索这些关键字：`SET `、`LISTEN`、`pg_advisory_lock`、`CREATE TEMP`。{{t:760}}\n\n找到后有两种处理：能改为事务内完成的就改（例如 `SET LOCAL`）；必须依赖会话的功能，可以单独开一个 session 模式的 PgBouncer 库别名给它用。后一种做法视频只是简单提到，没有演示。',
      suggestions: ['那这个怎么设置？', '池大小应该设多少？']
    },
    {
      topic: 'waiting', match: /cl_waiting|排队|变慢|timeout|SHOW POOLS|wait/i,
      text: '先连到管理库执行 `SHOW POOLS;`。{{t:1247}} 视频里的判断方法是：\n\n- `cl_waiting` 持续大于 0，同时 `sv_active` 已经等于池大小（演示中分别是 37 和 20），说明池被占满。{{t:1290}}\n- 这时先查长事务和连接泄漏，确认没有问题再小步调大池。{{t:1330}}\n\n直接加大池可能把压力转移到数据库，结果更慢。',
      follow: '查长事务可以在 PostgreSQL 中执行：\n\n`SELECT pid, now() - xact_start AS dur, state, query FROM pg_stat_activity WHERE xact_start IS NOT NULL ORDER BY dur DESC;`\n\n这条 SQL 视频没有给出，是通用排查方法；视频只提到「先查长事务和连接泄漏」。{{t:1330}}',
      suggestions: ['那这个怎么设置？', 'max_client_conn 应该设多大？']
    },
    {
      topic: 'maxclient', match: /max_client|ulimit|no more connections|客户端连接/i,
      text: '`no more connections allowed` 表示客户端连接数超过了 `max_client_conn`。{{t:1118}} 视频中的日志显示，发布 3 分钟后客户端刚好打到 500 时开始报错。{{t:1142}}\n\n处理时要同时改两处：\n\n1. 调大 `max_client_conn`；\n2. 确认 `ulimit -n` 足够，演示机器只有 1024，调大后会换成文件描述符不足的错误。{{t:1170}}',
      follow: '视频的做法是在 systemd 单元里加 `LimitNOFILE=65536`，然后重启 PgBouncer。经验上文件描述符上限至少要大于 `max_client_conn` 加上所有服务端连接数。{{t:1170}}',
      suggestions: ['那这个怎么设置？', 'cl_waiting 一直大于 0 怎么办？']
    }
  ]

  const suggestions = ['transaction 模式下哪些写法会出问题？', 'default_pool_size 应该设多少？', 'prepared statement 报错怎么彻底解决？']

  /* ---------- 视频库中的其他示例 ---------- */
  function brief(o) {
    return {
      title: o.title, mapTitle: o.mapTitle, resolvedMode: 'text', modeReason: '以讲解为主，Agent 选择了文字摘要',
      overview: o.overview, keyPoints: o.points, fit: o.fit, figures: {},
      blocks: o.blocks.map((b, i) => ({
        id: 'b' + (i + 1), title: b[0], short: b[0], start: b[1], end: o.blocks[i + 1] ? o.blocks[i + 1][1] : o.duration,
        paras: [{ id: 'b' + (i + 1) + 'p1', t: b[1] + 8, md: b[2] }], figures: [],
        concepts: b[3].map((c, j) => ({ id: 'b' + (i + 1) + 'k' + j, label: c, para: 'b' + (i + 1) + 'p1' }))
      }))
    }
  }

  const library = [
    {
      id: 'k8s', title: 'Kubernetes Ingress 证书自动续期失败排查：cert-manager 的 5 个常见坑', up: '云原生小队', bv: 'BV1qW4y1k7Zd', duration: 1880, ago: 2 * 864e5,
      thumb: { hue: 212, glyph: 'K8s', sub: 'cert-manager' }, tags: [['t-k8s', 'auto'], ['t-cert', 'auto'], ['t-trouble', 'auto']],
      doc: brief({
        title: 'Kubernetes Ingress 证书自动续期失败排查：cert-manager 的 5 个常见坑', mapTitle: 'cert-manager 续期', duration: 1880,
        overview: '视频从一次证书过期事故出发，梳理 cert-manager 自动续期失败的排查路径：先看 Certificate 与 Order 状态，再定位 HTTP-01 挑战失败、Ingress class 不匹配和速率限制。',
        points: ['先 `kubectl describe certificate`，顺着 CertificateRequest → Order → Challenge 往下查。', 'HTTP-01 挑战失败最常见的原因是 Ingress class 写错或 80 端口被重定向。'],
        fit: '适合使用 cert-manager 管理 Let’s Encrypt 证书的集群。',
        blocks: [['续期失败的现象', 60, '证书在到期前 30 天应自动续期，事故中续期 Order 一直停在 pending，最终证书过期，浏览器报 NET::ERR_CERT_DATE_INVALID。', ['到期前 30 天续期', 'Order 卡在 pending']],
          ['顺着资源链排查', 420, '按 Certificate、CertificateRequest、Order、Challenge 的顺序执行 `kubectl describe`，在 Challenge 的事件里看到 HTTP-01 自检返回 404。', ['四层资源链', 'Challenge 事件']],
          ['五个常见坑', 980, 'Ingress class 不匹配、HTTP 强制跳转 HTTPS 拦截了挑战、DNS 未生效、命中 Let’s Encrypt 速率限制、ClusterIssuer 引用了错误的 Secret。', ['class 不匹配', '速率限制']]]
      })
    },
    {
      id: 'explain', title: 'PostgreSQL 慢查询诊断：手把手读懂 EXPLAIN ANALYZE 输出', up: '后端札记', bv: 'BV1mN411f7Hc', duration: 1085, ago: 4 * 864e5,
      thumb: { hue: 205, glyph: 'EXPLAIN', sub: 'PostgreSQL' }, tags: [['t-pg', 'auto'], ['t-db', 'manual'], ['t-perf', 'auto'], ['t-toread', 'manual']],
      doc: brief({
        title: 'PostgreSQL 慢查询诊断：手把手读懂 EXPLAIN ANALYZE 输出', mapTitle: 'EXPLAIN ANALYZE', duration: 1085,
        overview: '视频以一条 3 秒的订单查询为例，逐行解读 EXPLAIN ANALYZE：先比较估算行数与实际行数，再找耗时最多的节点，最后通过补索引和更新统计信息把查询降到 40 毫秒。',
        points: ['估算行数与实际行数差 10 倍以上，通常说明统计信息过期。', '`Rows Removed by Filter` 很大时，优先考虑索引。'],
        fit: '适合已经定位到慢 SQL、需要判断原因的场景。',
        blocks: [['读懂一行计划', 40, '每个节点包含 cost 估算、实际耗时、行数和循环次数，嵌套循环的总耗时要乘以 loops。', ['cost 与 actual', 'loops 相乘']],
          ['找到最慢的节点', 380, '从最内层往外看，Seq Scan 过滤掉 98% 的行是本例的瓶颈。', ['Seq Scan', 'Rows Removed']],
          ['优化与验证', 760, '为 (user_id, created_at) 建复合索引并执行 ANALYZE，计划变为 Index Scan，耗时从 3.1 秒降到 40 毫秒。', ['复合索引', 'ANALYZE']]]
      })
    },
    {
      id: 'redis', title: 'Redis 缓存击穿、穿透与雪崩：三种方案的取舍与压测对比', up: '架构笔记本', bv: 'BV1Ls4y1Q7pX', duration: 1368, ago: 6 * 864e5,
      thumb: { hue: 4, glyph: 'Redis', sub: '击穿 · 穿透 · 雪崩' }, tags: [['t-redis', 'auto'], ['t-cache', 'auto'], ['t-perf', 'auto']],
      doc: brief({
        title: 'Redis 缓存击穿、穿透与雪崩：三种方案的取舍与压测对比', mapTitle: '缓存三大问题', duration: 1368,
        overview: '视频分别复现缓存击穿、穿透和雪崩，比较互斥锁、逻辑过期、布隆过滤器和随机过期时间的效果，并给出各自适用的业务场景。',
        points: ['热点 key 击穿：互斥锁更一致，逻辑过期更快。', '雪崩：过期时间加随机抖动，并为缓存层做降级。'],
        fit: '适合读多写少、存在热点数据的业务。',
        blocks: [['击穿：热点 key 过期', 50, '压测中热点 key 过期瞬间 QPS 全部落到数据库，互斥锁方案把数据库峰值压到原来的 3%。', ['互斥锁', '逻辑过期']],
          ['穿透：查询不存在的数据', 520, '布隆过滤器拦截了 99% 的无效请求，缓存空值适合 key 空间较小的场景。', ['布隆过滤器', '缓存空值']],
          ['雪崩：大量 key 同时过期', 960, '过期时间加 0～300 秒随机值，并在 Redis 不可用时降级为本地缓存。', ['随机过期', '降级']]]
      })
    },
    {
      id: 'go', title: 'Go 并发模式：用 errgroup 和 context 优雅地取消一组任务', up: 'Gopher 日常', bv: 'BV1Hu411m7Ke', duration: 912, ago: 9 * 864e5,
      thumb: { hue: 190, glyph: 'Go', sub: 'errgroup' }, tags: [['t-go', 'auto'], ['t-conc', 'auto']],
      doc: brief({
        title: 'Go 并发模式：用 errgroup 和 context 优雅地取消一组任务', mapTitle: 'errgroup 与 context', duration: 912,
        overview: '视频用并发抓取 20 个接口的例子，演示 errgroup.WithContext 在任一任务失败时取消其余任务，并用 SetLimit 控制并发数。',
        points: ['`errgroup.WithContext` 返回的 ctx 在首个错误时被取消。', '`g.SetLimit(n)` 可以替代自己写的信号量。'],
        fit: '适合需要「一个失败全部取消」的并发任务。',
        blocks: [['为什么不用 WaitGroup', 40, 'WaitGroup 只负责等待，不传播错误也不取消其他任务，需要额外的 channel 和锁。', ['只等待不取消']],
          ['errgroup 基本用法', 300, '在 g.Go 中返回 error，g.Wait 返回第一个错误；任务内部要监听 ctx.Done()。', ['WithContext', '监听 ctx.Done']],
          ['限制并发数', 640, 'g.SetLimit(5) 后，第 6 个 g.Go 会阻塞直到有任务完成。', ['SetLimit']]]
      })
    },
    {
      id: 'goescape', title: 'Golang 内存逃逸分析入门：从 -gcflags 输出看懂堆分配', up: '本地文件', file: 'go-escape-analysis.mp4', size: '186 MB', duration: 760, ago: 12 * 864e5,
      thumb: { hue: 170, glyph: 'Golang', sub: '-gcflags=-m' }, tags: [['t-golang', 'auto'], ['t-perf', 'auto']], status: 'partial',
      doc: brief({
        title: 'Golang 内存逃逸分析入门：从 -gcflags 输出看懂堆分配', mapTitle: '逃逸分析', duration: 760,
        overview: '视频通过 `go build -gcflags=-m` 的输出，讲解变量何时逃逸到堆上，以及返回指针、闭包捕获和 interface 转换三种常见情况。',
        points: ['返回局部变量指针会逃逸，但不一定更慢，要用基准测试判断。', '`fmt.Println` 的 interface 参数会导致逃逸。'],
        fit: '适合排查 GC 压力和分配过多的问题。',
        blocks: [['读懂 -m 输出', 30, '`moved to heap` 表示变量逃逸，`does not escape` 表示留在栈上。', ['moved to heap']],
          ['三种常见逃逸', 260, '返回指针、闭包捕获外部变量、赋值给 interface，都会让编译器把变量放到堆上。', ['返回指针', '闭包', 'interface']],
          ['要不要优化', 560, '用 `go test -bench -benchmem` 对比分配次数，只有热点路径才值得改。', ['benchmem']]]
      })
    },
    {
      id: 'nginx', title: 'Nginx 反向代理 502/504 排查全流程（附 upstream 超时参数速查表）', up: '运维那些事', bv: 'BV1Tg4y1c7Wm', duration: 1623, ago: 15 * 864e5,
      thumb: { hue: 140, glyph: '502', sub: 'Nginx upstream' }, tags: [['t-nginx', 'auto'], ['t-net', 'auto'], ['t-trouble', 'auto'], ['t-toread', 'manual']],
      doc: brief({
        title: 'Nginx 反向代理 502/504 排查全流程（附 upstream 超时参数速查表）', mapTitle: '502 / 504 排查', duration: 1623,
        overview: '视频区分 502 与 504 的含义，按「看错误日志 → 测 upstream → 调超时与 keepalive」的顺序排查，并整理了常用超时参数。',
        points: ['502 多为 upstream 拒绝或断开连接，504 是等待响应超时。', 'upstream keepalive 需要同时设置 `proxy_http_version 1.1` 和清空 Connection 头。'],
        fit: '适合 Nginx 作为反向代理的 Web 服务。',
        blocks: [['502 与 504 的区别', 45, '502 表示从 upstream 收到无效响应或连接被拒绝；504 表示在 proxy_read_timeout 内没有收到响应。', ['502 连接问题', '504 超时']],
          ['从错误日志定位', 480, '`connect() failed (111)` 说明后端未监听，`upstream prematurely closed` 多为后端主动断开。', ['error.log', '111 拒绝']],
          ['超时与 keepalive', 1050, '按链路设置 connect、send、read 三个超时，并开启 upstream keepalive 减少连接建立。', ['三个超时', 'keepalive']]]
      })
    },
    {
      id: 'docker', title: 'Docker 多阶段构建：把 Go 服务镜像从 1.2GB 压到 18MB', up: '容器手记', bv: 'BV1Nk4y1w7Fq', duration: 658, ago: 20 * 864e5,
      thumb: { hue: 200, glyph: 'Docker', sub: '1.2GB → 18MB' }, tags: [['t-docker', 'auto'], ['t-image', 'auto'], ['t-go', 'auto']],
      doc: brief({
        title: 'Docker 多阶段构建：把 Go 服务镜像从 1.2GB 压到 18MB', mapTitle: '多阶段构建', duration: 658,
        overview: '视频把一个 Go 服务的 Dockerfile 改成多阶段构建：在 golang 镜像中编译静态二进制，再复制到 distroless 镜像，体积从 1.2GB 降到 18MB。',
        points: ['`CGO_ENABLED=0` 编译静态二进制，才能放进无 libc 的镜像。', '先复制 go.mod 再下载依赖，可以利用构建缓存。'],
        fit: '适合编译型语言的服务镜像。',
        blocks: [['单阶段的问题', 30, '直接基于 golang 镜像运行，包含了编译器和源码，体积 1.2GB。', ['包含编译器']],
          ['多阶段改造', 190, '第一阶段编译，第二阶段 `COPY --from=builder` 只拿二进制，基础镜像用 distroless/static。', ['COPY --from', 'distroless']],
          ['缓存与安全', 450, '分层复制依赖文件以命中缓存，使用 nonroot 用户运行。', ['构建缓存', 'nonroot']]]
      })
    },
    {
      id: 'vite', title: 'Vite 6 迁移记录：从 webpack 切换后构建快了 8 倍', up: '前端周刊', bv: 'BV1Rm4y1v7Tn', duration: 1170, ago: 600e3,
      thumb: { hue: 268, glyph: 'Vite', sub: 'webpack → Vite' }, tags: [], status: 'processing', slow: true,
      doc: brief({
        title: 'Vite 6 迁移记录：从 webpack 切换后构建快了 8 倍', mapTitle: 'Vite 迁移', duration: 1170,
        overview: '视频记录了一个中型 React 项目从 webpack 5 迁移到 Vite 6 的过程，包括环境变量、别名、CommonJS 依赖和构建产物差异的处理。',
        points: ['`process.env` 需要改为 `import.meta.env`。', '部分 CommonJS 依赖需要加入 optimizeDeps.include。'],
        fit: '适合中型 SPA 项目的构建迁移。',
        blocks: [['迁移前的准备', 40, '先统计 loader 与插件，列出需要替换的部分，并锁定 Node 版本。', ['盘点插件']],
          ['常见改动', 330, '环境变量、路径别名、SVG 导入与 CommonJS 依赖是改动最多的四项。', ['import.meta.env', 'optimizeDeps']],
          ['结果对比', 860, '冷启动从 48 秒降到 1.9 秒，生产构建从 96 秒降到 12 秒。', ['冷启动', '构建时间']]]
      }),
      autoTags: [['t-fe', 'auto'], ['t-vite', 'auto']]
    },
    {
      id: 'loom', title: 'Spring Boot 3 虚拟线程实测：Tomcat 吞吐提升与 synchronized 的坑', up: 'Java 进阶路', bv: 'BV1Vc411e7Jp', duration: 1302, ago: 1 * 864e5,
      thumb: { hue: 28, glyph: 'Loom', sub: 'Virtual Threads' }, tags: [], status: 'failed',
      doc: brief({
        title: 'Spring Boot 3 虚拟线程实测：Tomcat 吞吐提升与 synchronized 的坑', mapTitle: '虚拟线程实测', duration: 1302,
        overview: '视频在 Spring Boot 3.2 中开启虚拟线程，对比 I/O 密集接口的吞吐，并演示 synchronized 导致载体线程被钉住（pinning）的问题。',
        points: ['`spring.threads.virtual.enabled=true` 一行即可开启。', '在 synchronized 块内做阻塞 I/O 会钉住载体线程，改用 ReentrantLock。'],
        fit: '适合 I/O 密集、线程池经常打满的 Java 服务。',
        blocks: [['开启方式', 40, 'Spring Boot 3.2 起支持配置项开启，Tomcat 会为每个请求使用虚拟线程。', ['一行配置']],
          ['吞吐对比', 400, '模拟 200ms 下游延迟时，吞吐从约 950 rps 提升到约 4,600 rps。', ['I/O 密集收益大']],
          ['pinning 问题', 880, '用 `-Djdk.tracePinnedThreads=full` 定位，synchronized 改为 ReentrantLock 后恢复。', ['tracePinnedThreads', 'ReentrantLock']]]
      }),
      autoTags: [['t-java', 'auto'], ['t-conc', 'auto'], ['t-perf', 'auto']]
    }
  ]

  const tags = {
    't-pg': { name: 'PostgreSQL', origin: 'agent', aliases: ['Postgres', 'PG'] },
    't-db': { name: '数据库', origin: 'manual', aliases: [] },
    't-perf': { name: '性能调优', origin: 'agent', aliases: ['性能优化'] },
    't-k8s': { name: 'Kubernetes', origin: 'agent', aliases: ['K8s'] },
    't-cert': { name: '证书', origin: 'agent', aliases: ['TLS 证书'] },
    't-trouble': { name: '故障排查', origin: 'agent', aliases: ['排错'] },
    't-redis': { name: 'Redis', origin: 'agent', aliases: [] },
    't-cache': { name: '缓存', origin: 'agent', aliases: [] },
    't-go': { name: 'Go', origin: 'agent', aliases: [] },
    't-golang': { name: 'Golang', origin: 'agent', aliases: [] },
    't-conc': { name: '并发', origin: 'agent', aliases: [] },
    't-nginx': { name: 'Nginx', origin: 'agent', aliases: [] },
    't-net': { name: '网络排错', origin: 'agent', aliases: [] },
    't-docker': { name: 'Docker', origin: 'agent', aliases: [] },
    't-image': { name: '镜像优化', origin: 'agent', aliases: [] },
    't-toread': { name: '待读', origin: 'manual', aliases: [] }
  }

  /* 主示例导入后 Agent 的分类过程 */
  const mainTagging = {
    links: [['t-pg', 'auto'], ['t-pool', 'auto'], ['t-pgb', 'auto'], ['t-trouble', 'auto'], ['t-perf', 'auto']],
    create: { 't-pool': { name: '连接池', origin: 'agent', aliases: [] }, 't-pgb': { name: 'PgBouncer', origin: 'agent', aliases: ['pgbouncer'] } },
    log: [
      { kind: 'reuse', text: '复用已有标签：PostgreSQL、故障排查、性能调优' },
      { kind: 'create', text: '新建标签：连接池、PgBouncer' },
      { kind: 'merge', text: '去重：候选「pgbouncer」与「PgBouncer」只有大小写不同，已归为同一个' },
      { kind: 'skip', text: '未添加：「数据库运维」与你手动维护的「数据库」含义重叠，留作建议' }
    ],
    suggestion: '数据库运维'
  }

  const mergeSuggestions = [
    { id: 'm1', from: 't-golang', to: 't-go', reason: '两者指同一门编程语言。合并后「Golang」保留为「Go」的别名，相关视频统一使用「Go」。' }
  ]

  window.DEMO = {
    main: {
      id: 'pg', bv: 'BV1Gx4y1R7pQ', up: '后端札记', duration: 1476, part: 1, parts: 1,
      url: 'https://www.bilibili.com/video/BV1Gx4y1R7pQ?p=1&spm_id_from=333.1007',
      cover: A + 'cover-pgbouncer.svg', doc: mainDoc, cues: mainCues, tagging: mainTagging
    },
    revisions, regenerated, answers, suggestions, library, tags, mergeSuggestions,
    extraTags: { 't-fe': { name: '前端工程', origin: 'agent', aliases: [] }, 't-vite': { name: 'Vite', origin: 'agent', aliases: [] }, 't-java': { name: 'Java', origin: 'agent', aliases: [] } },
    focusExamples: ['重点整理配置步骤和常见错误', '只要结论和适用条件', '保留所有命令和参数']
  }
})()
