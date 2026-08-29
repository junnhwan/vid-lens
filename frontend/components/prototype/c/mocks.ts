// PROTOTYPE C+ — 共享 mock 数据（未登录 / API 失败时兜底）

import { TaskStatusEnum, type VideoTask, type KnowledgeBase, type AIProfile } from '@/lib/types'

export const MOCK_TASKS: VideoTask[] = [
  {
    id: 101, user_id: 1, file_md5: 'abc', filename: 'product-demo.mp4', title: '产品发布会全程实录',
    file_url: '', file_size: 524288000, status: TaskStatusEnum.Completed, stage: 'none',
    trace_id: '', source_type: 'upload', retry_count: 0, max_retries: 3,
    last_error_code: '', last_error_msg: '', last_job_type: 'rag_index', error_msg: '',
    created_at: new Date(Date.now() - 3600000).toISOString(),
    updated_at: new Date().toISOString(),
    has_transcription: true, has_summary: true,
    transcription: { id: 1, task_id: 101, file_md5: 'abc', content: '', words: 1240, created_at: '' },
    summary: { id: 1, task_id: 101, file_md5: 'abc', content: '', model_name: 'gpt-4o', created_at: '' },
  },
  {
    id: 102, user_id: 1, file_md5: 'def', filename: 'interview.mp4', title: '创始人访谈：从 0 到 1',
    file_url: '', file_size: 209715200, status: TaskStatusEnum.Running, stage: 'transcribing',
    trace_id: '', source_type: 'url', source_url: 'https://youtube.com/watch?v=xxx',
    retry_count: 0, max_retries: 3, last_error_code: '', last_error_msg: '', last_job_type: 'transcribe', error_msg: '',
    created_at: new Date(Date.now() - 7200000).toISOString(),
    updated_at: new Date().toISOString(),
    has_transcription: true, has_summary: true,
    transcription: { id: 2, task_id: 102, file_md5: 'def', content: '', words: 380, created_at: '' },
    summary: { id: 2, task_id: 102, file_md5: 'def', content: '', model_name: 'gpt-4o', created_at: '' },
  },
  {
    id: 103, user_id: 1, file_md5: 'ghi', filename: 'lecture-ai.mp4', title: '深度学习入门讲座第 3 讲',
    file_url: '', file_size: 1073741824, status: TaskStatusEnum.Pending, stage: 'uploaded',
    trace_id: '', source_type: 'chunked', retry_count: 0, max_retries: 3,
    last_error_code: '', last_error_msg: '', last_job_type: '', error_msg: '',
    created_at: new Date(Date.now() - 86400000).toISOString(),
    updated_at: new Date().toISOString(),
    has_transcription: false, has_summary: false,
  },
  {
    id: 104, user_id: 1, file_md5: 'jkl', filename: 'broken.mp4', title: '损坏的测试文件',
    file_url: '', file_size: 10485760, status: TaskStatusEnum.Failed, stage: 'transcribing',
    trace_id: '', source_type: 'upload', retry_count: 2, max_retries: 3,
    last_error_code: 'ASR_TIMEOUT', last_error_msg: 'ASR 服务超时，请检查 API 配置后重试', last_job_type: 'transcribe',
    error_msg: 'ASR 服务超时', created_at: new Date(Date.now() - 172800000).toISOString(),
    updated_at: new Date().toISOString(),
    has_transcription: false, has_summary: false,
  },
]

export const MOCK_KBS: KnowledgeBase[] = [
  {
    id: 1, name: '产品研究', description: '竞品分析与产品发布会合集，用于跨视频对比问答',
    member_count: 3, embedding_model: 'text-embedding-3-small',
    videos: [
      { task_id: 101, title: '产品发布会全程实录', status: 3, index_status: 'indexed', retrievable: true },
      { task_id: 102, title: '创始人访谈：从 0 到 1', status: 2, index_status: 'pending', retrievable: false },
      { task_id: 103, title: '深度学习入门讲座第 3 讲', status: 0, index_status: 'none', retrievable: false },
    ],
    created_at: new Date(Date.now() - 604800000).toISOString(),
    updated_at: new Date().toISOString(),
  },
  {
    id: 2, name: '技术分享', description: '内部技术分享与培训录像',
    member_count: 1, embedding_model: 'bge-m3',
    videos: [
      { task_id: 103, title: '深度学习入门讲座第 3 讲', status: 0, index_status: 'none', retrievable: false },
    ],
    created_at: new Date(Date.now() - 1209600000).toISOString(),
    updated_at: new Date().toISOString(),
  },
]

export const MOCK_PROFILES: AIProfile[] = [
  {
    id: 1, name: '默认配置', is_default: true,
    llm_provider: 'openai', llm_base_url: 'https://api.openai.com/v1', llm_api_key_masked: 'sk-••••abcd', llm_model: 'gpt-4o',
    asr_provider: 'openai', asr_base_url: 'https://api.openai.com/v1', asr_api_key_masked: 'sk-••••abcd', asr_model: 'whisper-1',
    embedding_provider: 'openai', embedding_endpoint: 'https://api.openai.com/v1', embedding_api_key_masked: 'sk-••••abcd',
    embedding_model: 'text-embedding-3-small', embedding_dim: 1536,
    vision_provider: '', vision_base_url: '', vision_api_key_masked: '', vision_model: '',
  },
  {
    id: 2, name: 'SiliconFlow', is_default: false,
    llm_provider: 'siliconflow', llm_base_url: 'https://api.siliconflow.cn/v1', llm_api_key_masked: 'sk-••••efgh', llm_model: 'Qwen/Qwen2.5-72B-Instruct',
    asr_provider: 'siliconflow', asr_base_url: 'https://api.siliconflow.cn/v1', asr_api_key_masked: 'sk-••••efgh', asr_model: 'FunAudioLLM/SenseVoiceSmall',
    embedding_provider: 'siliconflow', embedding_endpoint: 'https://api.siliconflow.cn/v1', embedding_api_key_masked: 'sk-••••efgh',
    embedding_model: 'BAAI/bge-m3', embedding_dim: 1024,
    vision_provider: '', vision_base_url: '', vision_api_key_masked: '', vision_model: '',
  },
]

export function taskTitle(t: VideoTask) {
  return t.title || t.filename || `任务 #${t.id}`
}
