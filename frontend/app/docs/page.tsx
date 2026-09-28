import { CodeBlock } from './CodeBlock'

export default function DocsQuickstartPage() {
  return (
    <article className="docs-article">
      <h1>快速开始</h1>
      <p className="docs-lede">从一段视频开始，整理学习笔记、核对来源，再通过问答继续理解。</p>

      <h2>映知是什么</h2>
      <p>
        映知 VidLens 是一个自托管的视频学习与问答工作台。上传视频或导入链接后，先发起转写，再按需要生成摘要、学习笔记或建立检索索引。笔记、思维导图和知识画布共用一份内容，可以手动编辑或让 Agent 修改，并沿引用回到视频核对；也可以对单个视频、整个视频库或自建知识库提问。
      </p>
      <p>如果已经有可访问的站点，直接从下方「第一次使用」开始；环境准备与启动命令供自行部署时使用。</p>

      <h2>环境准备</h2>
      <ul>
        <li>Go 1.24+、Node.js 20+、Docker Compose</li>
        <li>FFmpeg；需要导入视频链接时安装 yt-dlp，需要画面文字识别时安装 Tesseract OCR 及对应语言包</li>
        <li>可用的模型服务：对话、语音识别、向量（Embedding）为必需，视觉为可选；详见 <a href="/docs/config">配置与常见问题</a></li>
      </ul>

      <h2>启动服务</h2>
      <p>
        首次使用时，将仓库根目录的 <code>.env.example</code> 复制为 <code>.env</code>；已有配置则保留原文件。
        按实际服务填写模型地址、密钥与维度，设置稳定的 <code>VIDLENS_API_KEY_SECRET</code>，并核对仓库中的{' '}
        <code>config.yaml</code> 与本机依赖地址。示例模型地址不能直接调用。
      </p>
      <p>在仓库根目录启动基础设施（PostgreSQL + pgvector、Redis、RabbitMQ、MinIO），待它们就绪后启动后端：</p>
      <CodeBlock lang="bash">{`docker compose up -d
go run ./cmd/server`}</CodeBlock>
      <p>另开一个终端启动 Vite / React 前端：</p>
      <CodeBlock lang="bash">{`cd frontend
npm ci
npm run dev -- --port 5173`}</CodeBlock>
      <p>
        启动后访问前端地址（开发模式默认为 <code>http://127.0.0.1:5173</code>）；后端健康检查为{' '}
        <code>http://127.0.0.1:8080/healthz</code>，依赖就绪检查为 <code>/readyz</code>。
        若端口被占用或受系统保留，换一个可用端口，例如 <code>npm run dev -- --port 5273</code>。
      </p>
      <p>
        开发前端默认将 API 请求代理到 <code>http://127.0.0.1:8080</code>；后端地址有变化时，在启动前端的终端设置{' '}
        <code>VIDLENS_API_BASE</code>。生产前端使用 <code>npm run build</code> 后运行 <code>npm start</code>，默认端口为 3000，可用{' '}
        <code>PORT</code> 调整。
      </p>
      <p>
        Windows 已有本地数据环境可用 <code>make start</code> / <code>make status</code>；首次初始化请先用上述命令。
        启动脚本会检查配置、依赖和数据目录，目录未准备好时会停止。前端端口可通过 <code>VIDLENS_FRONTEND_PORT</code> 设置。
      </p>

      <h2>第一次使用</h2>
      <ol>
        <li>
          <strong>注册并登录。</strong>打开前端后进入登录页，注册账号；也可用「演示账号」体验已有视频与问答，上传、成果生成和配置修改等操作受限。
        </li>
        <li>
          <strong>配置模型服务。</strong>进入「设置 → AI 服务」，新建配置并填写对话、语音识别、向量三项能力的地址、密钥与模型，保存并设为默认；视觉能力按需配置。可逐项探测连接，填写规则与示例见{' '}
          <a href="/docs/config">配置与常见问题</a>。
        </li>
        <li>
          <strong>导入视频。</strong>在工作台或视频库选择本地文件或粘贴视频链接。本地文件上传、合并完成前保持页面打开；链接下载任务受理后可离页。入库后需要手动发起转写；若复用了已有处理结果，可直接使用已就绪内容。
        </li>
        <li>
          <strong>完成转写。</strong>打开视频详情，点「先完成转写」。长视频会分片处理，页面显示分片进度与已转出的文字，也会显示画面处理状态；等待处理结束后再生成学习笔记。
        </li>
        <li>
          <strong>生成第一份笔记。</strong>在视频详情点「新建学习笔记」，或从侧栏「成果」选择视频，填写学习目标并开始后台生成。任务中心显示进度，离开页面不会取消。学习笔记只需已有转写，<strong>不要求先建立检索索引</strong>；视频摘要也可单独生成。
        </li>
        <li>
          <strong>阅读、核对与修改。</strong>打开成果，在学习笔记、思维导图和知识画布间切换；点引用查看文字或画面依据并回看视频。需要纠错时可编辑保存，也可让 Agent 修改全文或某个段落，再查看新版本差异。
        </li>
        <li>
          <strong>继续提问和积累。</strong>需要按内容检索时，在视频详情建立索引，再从「问答」选择范围。也可从笔记段落追问，将同视频的完整回答预览后收录到笔记。下次可从首页「回到学习位置」继续，笔记支持导出 Markdown。
        </li>
      </ol>

      <div className="docs-callout">
        <b>关于用量</b>
        转写、摘要、索引、问答、笔记生成与 AI 修订会调用对应模型服务，费用以服务商规则为准。阅读、手动编辑、切换导图或画布、导出 Markdown 不额外调用模型。生成结果及引用仍需人工核对。
      </div>

      <h2>接下来</h2>
      <ul>
        <li><a href="/docs/features#study-notes">学习笔记与成果</a>：从生成、来源核对到编辑、版本与导出</li>
        <li><a href="/docs/features">功能说明</a>：视频处理、摘要修订、Agent 问答、知识库与继续学习</li>
        <li><a href="/docs/config">配置与常见问题</a>：模型地址怎么填、索引有什么用、失败如何排查</li>
        <li><a href="/docs/changelog">更新日志</a>：近期功能与体验变化</li>
      </ul>
    </article>
  )
}
