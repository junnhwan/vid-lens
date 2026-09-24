export default function DocsQuickstartPage() {
  return (
    <article className="docs-article">
      <h1>快速开始</h1>
      <p className="docs-lede">从启动服务到完成第一次视频问答，大约需要十分钟。</p>

      <h2>映知是什么</h2>
      <p>
        映知 VidLens 是一个自托管的视频知识库与问答平台。上传视频或导入链接后，系统在后台完成语音转写、可选的画面识别与语义索引；之后可以对单个视频、整个视频库或自建知识库提问，回答中的每条引用都能跳回原视频对应的时间点核对。
      </p>

      <h2>环境准备</h2>
      <ul>
        <li>Go 1.24+、Node.js 20+、Docker Compose</li>
        <li>FFmpeg 与 yt-dlp（处理音视频与链接导入）</li>
        <li>可用的模型服务：对话、语音识别、向量（Embedding）为必需，视觉为可选；详见 <a href="/docs/config">配置与常见问题</a></li>
      </ul>

      <h2>启动服务</h2>
      <p>在仓库根目录启动基础设施（PostgreSQL + pgvector、Redis、RabbitMQ、MinIO）和后端：</p>
      <pre><code>{`cp .env.example .env   # 首次配置;已有 .env 时跳过,按环境编辑后继续
docker compose up -d
go run ./cmd/server`}</code></pre>
      <p>另开一个终端启动前端：</p>
      <pre><code>{`cd frontend
npm ci
npm run dev`}</code></pre>
      <p>
        启动后访问前端地址（开发模式默认为 <code>http://127.0.0.1:5173</code>）；后端健康检查为{' '}
        <code>http://127.0.0.1:8080/healthz</code>。Windows 环境也可以使用 <code>make start</code> /{' '}
        <code>make status</code>。
      </p>

      <h2>第一次使用</h2>
      <ol>
        <li>
          <strong>注册并登录。</strong>打开前端后进入登录页，注册账号；部署方提供的演示账号为只读，适合先看界面。
        </li>
        <li>
          <strong>配置模型服务。</strong>进入「设置 → AI 服务」，新建配置并填写对话、语音识别、向量三项能力的地址、密钥与模型；视觉能力按需开启。填写规则与示例见{' '}
          <a href="/docs/config">配置与常见问题</a>。
        </li>
        <li>
          <strong>上传视频。</strong>在工作台或视频库点「上传」，选择本地文件或粘贴视频链接。上传只负责把视频入库，<strong>不会自动开始处理</strong>。
        </li>
        <li>
          <strong>开始转写。</strong>打开视频详情，点「开始转写」。长视频会分片处理，页面显示分片进度与已转出的文字。
        </li>
        <li>
          <strong>生成摘要、建立索引。</strong>转写完成后可以生成全片摘要；建立索引后，视频内容才能被检索和问答引用。
        </li>
        <li>
          <strong>开始问答。</strong>从视频详情或侧栏「问答」进入，选择单视频、视频库或知识库范围提问；点击回答中的引用可跳回原视频画面。
        </li>
      </ol>

      <div className="docs-callout">
        处理的每一步都需要调用你在「设置 → AI 服务」中配置的模型服务，并按服务商规则产生用量与费用。页面上会在相应操作前说明是否消耗额度。
      </div>

      <h2>接下来</h2>
      <ul>
        <li><a href="/docs/features">功能说明</a>：工作台、转写、画面证据、索引、摘要、问答、知识库等能力的完整介绍</li>
        <li><a href="/docs/config">配置与常见问题</a>：模型地址怎么填、索引有什么用、失败如何排查</li>
        <li><a href="/docs/changelog">更新日志</a>：近期功能与体验变化</li>
      </ul>
    </article>
  )
}
