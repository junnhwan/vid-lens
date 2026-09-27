// Development-only contract specimens. Never imported by production data adapters.
import { detailSchema, evidenceSchema, runSchema, type ProductTask, type StudyBlock } from '../lib/artifacts/schema.ts'

const timestamp = '2026-09-27T05:00:00Z'
export const sourceTitle = 'Go 服务的 Docker 容器化实战'
const block = (block_id: string, parent_id: string | null, type: StudyBlock['type'], title: string, content: string, evidence: string): StudyBlock => ({ block_id, parent_id, type, title, content, claim_origin: 'source', evidence_refs: [{ evidence_id: evidence, relation: 'supports' }] })
export const studyFixture = detailSchema.parse({
  id: 'preview-study', kind: 'study', title: '从代码到容器，理解 Go 服务的交付', head_version: 1, current_version_id: 'preview-version-1', latest_run: null, created_at: timestamp, updated_at: timestamp,
  version: { id: 'preview-version-1', artifact_id: 'preview-study', version: 1, base_version: 0, origin: 'generated', run_id: 'preview-run', manifest_id: 'preview-manifest', quality: 'needs_review', created_at: timestamp, source_status: 'current', was_candidate: false, adopted_from_version_id: null,
    body: { schema_version: 1, kind: 'study', title: '从代码到容器，理解 Go 服务的交付', warnings: [], blocks: [
      block('build', null, 'section', '构建与交付', '从一份可复现的镜像开始，把构建工具与运行环境分开。交付的是可以重新创建的环境，而不只是一个可执行文件。', 'preview-e1'),
      block('image', 'build', 'concept', '镜像是模板，容器是一次运行', '镜像保存应用代码与运行环境。容器是镜像启动后的实例，拥有自己的进程和文件系统视图。更新应用时，创建新镜像并替换容器，让每次交付都有可追溯的起点。', 'preview-e1'),
      block('stages', 'build', 'concept', '多阶段构建，只带走运行所需', '在 builder 阶段编译 Go 程序，再把可执行文件复制到运行阶段。最终镜像不需要保留完整的 Go 编译环境。', 'preview-e2'),
      block('config', null, 'section', '配置与边界', '配置随部署环境变化，程序构建尽可能保持一致。理解进程监听的端口与宿主机映射端口之间的关系。', 'preview-e3'),
      block('ports', 'config', 'example', '端口映射，让服务可以被访问', '应用在容器内监听服务端口，再由运行时把宿主机端口映射到容器。排查连接失败时，先分别确认监听地址、容器端口与宿主机映射。', 'preview-e3'),
      block('verify', null, 'section', '运行与验证', '运行成功只是起点。用可重复的检查确认应用行为，并保留可以定位问题的日志。', 'preview-e4'),
      block('logs', 'verify', 'concept', '用日志与请求，验证真实行为', '查看应用启动日志，再实际发起请求。不要仅凭容器进程仍在运行就判断整个服务可用。', 'preview-e4'),
    ] },
  },
})
export const evidenceFixtures = [
  { id: 'preview-e1', content: '镜像里装的是应用运行需要的内容，启动之后才是一个正在运行的容器。同一份镜像可以启动不同的容器实例。', start_ms: 246000, end_ms: 310000 },
  { id: 'preview-e2', content: '我们在 builder 这个阶段把 Go 程序编译出来。下面这个阶段只把编译好的文件拷贝过去，不需要把整个 Go 编译环境都带上。', start_ms: 522000, end_ms: 576000 },
  { id: 'preview-e3', content: '这里左边是宿主机的端口，右边是容器里的端口。要检查程序本身监听的位置，然后再去看端口映射。', start_ms: 740000, end_ms: 810000 },
  { id: 'preview-e4', content: '先看启动日志，然后用请求实际检查接口。进程还在并不代表请求能正常返回，我们要验证真实的行为。', start_ms: 1110000, end_ms: 1180000 },
].map((item, i) => evidenceSchema.parse({ ...item, manifest_id: 'preview-manifest', source_id: 42, source_title: sourceTitle, source_identity: `preview-transcript-${i}`, modality: 'transcript', content_hash: `preview-hash-${i}`, time_range_status: 'coarse' }))
export const runFixture = runSchema.parse({ id: 'preview-run', artifact_id: 'preview-study', source_task_id: 42, parent_run_id: null, status: 'running', stage: 'generating', cancel_requested: false, can_cancel: true, can_retry: false, can_resume: true, result: null, error_code: null, created_at: timestamp, started_at: timestamp, finished_at: null, last_seq: 3, usage: { llm_calls: 1, prompt_tokens: 0, completion_tokens: 0, token_source: 'unknown' } })
export const taskFixtures: ProductTask[] = [
  { id: 'artifact:preview-run', type: 'artifact_generation', resource_id: 'preview-run', title: studyFixture.title, status: 'running', stage: 'generating', can_cancel: true, can_retry: false, can_resume: true, created_at: timestamp, updated_at: timestamp, run: runFixture },
  { id: 'artifact:preview-failed', type: 'artifact_generation', resource_id: 'preview-failed', title: '整理这段课程的核心概念', status: 'failed', stage: 'failed', can_cancel: false, can_retry: true, can_resume: false, created_at: timestamp, updated_at: timestamp, run: { ...runFixture, id: 'preview-failed', status: 'failed', stage: 'failed', can_cancel: false, can_retry: true, can_resume: false, error_code: 'provider_error' } },
]
