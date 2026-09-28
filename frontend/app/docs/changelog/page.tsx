

export default function DocsChangelogPage() {
  return (
    <article className="docs-article">
      <h1>更新日志</h1>
      <p className="docs-lede">按项目变更日期记录面向用户的功能与体验变化；链接指向对应说明。</p>

      <h2>2026 年 9 月</h2>

      <section className="docs-log">
        <header>
          <time>2026-09-28</time>
          <h3>AI 修订笔记与可编辑知识画布</h3>
        </header>
        <ul>
          <li><a href="/docs/features#artifact-edit">Agent 修订</a>：可针对全文或选中段落及其子段落提出要求，选择「只核对」「先看修改方案」或「直接保存修改」。</li>
          <li>修订结果展示新增、修改、删除与移动的差异及依据；保存为新版本，允许撤销时可撤销本次修改，历史版本仍保留。</li>
          <li><a href="/docs/features#canvas">知识画布</a>：支持拖动并固定节点、折叠子级与隐藏节点、横向或纵向排版、局部 AI 排版及布局撤销 / 重做。</li>
          <li>图中手动改名、添加相关 / 依赖 / 对比关系会进入笔记草稿，保存后生成内容版本；节点也可交给 Agent 修订，布局单独保存。</li>
        </ul>
      </section>

      <section className="docs-log">
        <header>
          <time>2026-09-28</time>
          <h3>个人摘要修订与术语规则</h3>
        </header>
        <ul>
          <li><a href="/docs/features#summary-revision">摘要修订</a>：输入修改要求，先查看逐处差异再确认保存；修订属于当前账号，可下载当前摘要 Markdown。</li>
          <li>可选择「只改这次」或为当前视频保存名称规则，填写适用上下文与排除条件；规则可停用，原始转写与共享生成原稿保持原样。</li>
          <li>可同时指定一份笔记进行修改，摘要与笔记独立保存并分别报告结果；生成原稿更新时，可选择保留个人修订或改用新原稿。</li>
        </ul>
      </section>

      <section className="docs-log">
        <header>
          <time>2026-09-28</time>
          <h3>继续学习、段落追问与回答收录</h3>
        </header>
        <ul>
          <li><a href="/docs/features#study-resume">继续学习</a>：首页可回到已保存的视频时间或笔记段落；原段落或版本变化时，回退到可读位置。</li>
          <li>从笔记段落进入当前视频的问答，携带该段落与来源上下文，并提供返回原段落的入口；有未保存修改时先处理草稿。</li>
          <li><a href="/docs/features#qa">回答收进笔记</a>：同视频的完整、已保存回答可预览目标笔记、正文与引用后收录为新版本；引用无法匹配时，需取消或明确作为无来源个人补充保存。</li>
          <li>完善编辑冲突时的草稿保留、引用标签与学习位置恢复；避免较早提交的画布保存或 AI 排版结果覆盖后续修改。</li>
        </ul>
      </section>

      <section className="docs-log">
        <header>
          <time>2026-09-27</time>
          <h3>学习笔记、导图与证据工作区</h3>
        </header>
        <ul>
          <li><a href="/docs/features#study-notes">学习笔记</a>：选择一个已有转写的视频，填写学习目标并启动后台生成；笔记与思维导图共用同一份内容，导图不额外调用模型。</li>
          <li><a href="/docs/features#tasks">任务中心</a>：查看生成阶段、结果、失败或取消状态；离开页面不会取消已提交的笔记生成任务。</li>
          <li>证据栏可切换段落关联的多条转写、画面文字或画面观察来源；有可靠时间时可回看视频，并区分粗粒度时间与无法定位的来源。</li>
          <li>支持修改标题与正文、新增 / 移动 / 合并 / 删除段落、保存新版本、查看历史版本及导出已保存版本的 Markdown；生成结果保留待核对提示。</li>
          <li>任务与视频页面补充画面分析阶段和帧处理进度，便于区分未执行、处理中及已完成的分支。</li>
        </ul>
      </section>

      <section className="docs-log">
        <header>
          <time>2026-09-27</time>
          <h3>模型可用性探测</h3>
        </header>
        <ul>
          <li><a href="/docs/config#base-url">模型可用性检查</a>：依次探测各项能力，显示等待 / 探测中 / 可用 / 需要处理、耗时与失败原因；配置修改后清除旧探测结果。</li>
          <li>Embedding 探测显示实际维度；ASR 探测明确区分静音样本请求与真实语音质量，提示用真实片段复核。</li>
        </ul>
      </section>

      <section className="docs-log">
        <header>
          <time>2026-09-24</time>
          <h3>项目介绍与使用文档</h3>
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
          <h3>处理过程透明度与问答体验</h3>
        </header>
        <ul>
          <li><a href="/docs/features#transcription">转写</a>：分片进度、时间范围、重试与限流等待原因、已完成分片文字均可见；显示服务端并发上限。</li>
          <li><a href="/docs/features#index">索引</a>：区分待建 / 排队 / 构建中 / 已建立 / 失败 / 需重建；构建中显示块数进度；服务端阻止同一视频的重复构建。</li>
          <li><a href="/docs/features#summary">摘要</a>：按配置的上下文容量选择整篇或分段生成，分段任务显示进度并支持失败续跑；失败按类别提示并附诊断编号。</li>
          <li><a href="/docs/features#visual">画面证据</a>：可按视频关闭；抽帧预算覆盖全片，详情页显示证据覆盖范围；预览使用实际保存的关键帧。</li>
          <li>视频卡片四段进度条增加「入库 / 转写 / 画面分析 / 检索索引」图例与状态说明；首页显示任务的具体阶段与等待的任务类型。</li>
          <li>上传结果明确提示处理需要手动启动；命中去重时提示复用已有结果。</li>
        </ul>
      </section>

      <section className="docs-log">
        <header>
          <time>2026-09-24</time>
          <h3>问答与个性化</h3>
        </header>
        <ul>
          <li><a href="/docs/features#qa">问答</a>：侧栏新增独立入口，单视频 / 视频库 / 知识库三种范围独立保存会话并持续显示当前范围。</li>
          <li>新增按视频内容的推荐问题，点击直接发问；不额外调用模型。</li>
          <li>AI 回答统一 Markdown 渲染：支持标题、列表、引用、代码与表格，引用标记可继续跳转证据。</li>
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
          <h3>配置体验</h3>
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
          <h3>向量维度兼容</h3>
        </header>
        <ul>
          <li>向量列改为未定长并自动迁移旧列；索引与检索按模型和维度匹配，支持不同配置使用不同 Embedding 维度（如 1024）。</li>
        </ul>
      </section>

      <section className="docs-log">
        <header>
          <time>2026-09-13</time>
          <h3>稳定性</h3>
        </header>
        <ul>
          <li>媒体播放统一走同源接口，修复部分环境下的播放鉴权问题。</li>
          <li>演示账号的配置列表补充向量维度与视觉模型名称，服务地址和密钥仍隐藏。</li>
        </ul>
      </section>

      <section className="docs-log">
        <header>
          <time>2026-09-12</time>
          <h3>Agent 工作区与知识库问答</h3>
        </header>
        <ul>
          <li>知识库跨视频 Agent 工作区：自主调用工具、流式展示执行过程，支持多轮追问与跨视频比较。</li>
          <li>Agent 执行预算（工具调用、时长、Token、视觉帧）可按配置档调整；执行记录持久化，断流后可恢复查看。</li>
          <li>引入用户授权的长期记忆；答案反馈可转成回归用例。</li>
        </ul>
      </section>

      <div className="docs-callout info">
        <b>记录范围</b>
        本日志面向使用者，记录功能与体验变化；底层重构、内部指标与文档整理等不影响使用的变更不逐一列出。
      </div>
    </article>
  )
}
