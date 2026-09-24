export default function DocsChangelogPage() {
  return (
    <article className="docs-article">
      <h1>更新日志</h1>
      <p className="docs-lede">按日期记录面向用户的功能与体验变化；链接指向对应说明。</p>

      <h2>2026 年 9 月</h2>

      <section className="docs-log">
        <header>
          <time>2026-09-24</time>
          <span>项目介绍与使用文档</span>
        </header>
        <ul>
          <li>新增项目介绍页（/intro）：产品定位、典型使用流程与核心能力说明，可直接进入工作台或阅读文档。</li>
          <li>新增使用文档站（/docs）：<a href="/docs">快速开始</a>、<a href="/docs/features">功能说明</a>、<a href="/docs/config">配置与常见问题</a>与本更新日志。</li>
          <li>侧栏新增「文档」入口；登录页提供产品介绍链接。</li>
        </ul>
      </section>

      <section className="docs-log">
        <header>
          <time>2026-09-24</time>
          <span>处理过程透明度与问答体验</span>
        </header>
        <ul>
          <li><a href="/docs/features#transcription">转写</a>：分片进度、时间范围、重试与限流等待原因、已完成分片文字均可见；显示服务端并发上限。</li>
          <li><a href="/docs/features#index">索引</a>：区分待建 / 排队 / 构建中 / 已建立 / 失败 / 需重建；构建中显示块数进度；服务端阻止同一视频的重复构建。</li>
          <li><a href="/docs/features#summary">摘要</a>：长转写分段生成并显示进度，失败可续跑；失败按类别提示并附诊断编号。</li>
          <li><a href="/docs/features#visual">画面证据</a>：可按视频关闭；抽帧预算覆盖全片，详情页显示证据覆盖范围；预览使用实际保存的关键帧。</li>
          <li>视频卡片四段进度条增加「入库 / 转写 / 画面分析 / 检索索引」图例与状态说明；首页显示任务的具体阶段与等待的任务类型。</li>
          <li>上传结果明确提示处理需要手动启动；命中去重时提示复用已有结果。</li>
        </ul>
      </section>

      <section className="docs-log">
        <header>
          <time>2026-09-24</time>
          <span>问答与个性化</span>
        </header>
        <ul>
          <li><a href="/docs/features#qa">问答</a>：侧栏新增独立入口，单视频 / 视频库 / 知识库三种范围独立保存会话并持续显示当前范围。</li>
          <li>新增按视频内容的推荐问题，点击直接发问；不额外调用模型。</li>
          <li>AI 回答统一 Markdown 渲染：标题、列表、引用、代码、表格均可正确显示，引用标记可继续跳转证据。</li>
          <li>回答末尾就地显示本轮状态与耗时；新增按问题的会话导航目录；执行步骤的输入 / 输出摘要可展开，并随回答保存供回看。</li>
          <li>检索受限时回答明确标记「检索降级 · 无引用」，并自动等待重试向量模型额度。</li>
          <li>每轮回答记录实际使用的模式、模型与配置档；同一会话切换 Chat / Agent 或更换配置后，历史回答保持原样。</li>
          <li><a href="/docs/features#prompt">提示词偏好</a>：查看各功能实际指令并按功能保存个人偏好。</li>
          <li><a href="/docs/features#memory">长期记忆</a>：个人总开关默认关闭，会话设置不能越过个人选择；记忆可逐条撤回或删除。</li>
        </ul>
      </section>

      <section className="docs-log">
        <header>
          <time>2026-09-24</time>
          <span>配置体验</span>
        </header>
        <ul>
          <li><a href="/docs/config#base-url">AI 服务</a>：按能力说明地址填写规则并校验常见错误；支持逐能力真实探测（含 Embedding 维度校验）。</li>
          <li><a href="/docs/config#profile-transfer">配置导入 / 导出</a>：JSON 格式，不含密钥；导入先预览再确认创建。</li>
          <li>未保存的配置在离开页面、切换标签、刷新或关闭前会请求确认。</li>
        </ul>
      </section>

      <section className="docs-log">
        <header>
          <time>2026-09-23</time>
          <span>向量维度兼容</span>
        </header>
        <ul>
          <li>向量列改为未定长并自动迁移旧列；索引与检索按模型和维度匹配，支持不同配置使用不同 Embedding 维度（如 1024）。</li>
        </ul>
      </section>

      <section className="docs-log">
        <header>
          <time>2026-09-13</time>
          <span>稳定性</span>
        </header>
        <ul>
          <li>媒体播放统一走同源接口，修复部分环境下的播放鉴权问题。</li>
          <li>配置列表保留各能力的完整字段。</li>
        </ul>
      </section>

      <section className="docs-log">
        <header>
          <time>2026-09-12</time>
          <span>Agent 工作区与知识库问答</span>
        </header>
        <ul>
          <li>知识库跨视频 Agent 工作区：自主调用工具、流式展示执行过程，支持多轮追问与跨视频比较。</li>
          <li>Agent 执行预算（工具调用、时长、Token、视觉帧）可按配置档调整；执行记录持久化，断流后可恢复查看。</li>
          <li>引入用户授权的长期记忆；答案反馈可转成回归用例。</li>
        </ul>
      </section>

      <div className="docs-callout info">
        本日志面向使用者，记录功能与体验变化；底层重构、内部指标与文档整理等不影响使用的变更不逐一列出。
      </div>
    </article>
  )
}
