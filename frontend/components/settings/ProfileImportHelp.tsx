import { PROFILE_IMPORT_TEMPLATE } from '@/lib/profileTransfer'
import { useToast } from '@/components/Toast'
import { Modal } from '@/components/ui/Modal'
import { Icon } from '@/components/ui/Icon'

export function ProfileImportHelp({ onClose }: { onClose: () => void }) {
  const toast = useToast()
  const download = () => {
    const url = URL.createObjectURL(new Blob([PROFILE_IMPORT_TEMPLATE], { type: 'application/json' }))
    const link = document.createElement('a')
    link.href = url; link.download = 'vidlens-ai-profile-template.json'; link.click()
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
  const copy = async () => {
    try { await navigator.clipboard.writeText(PROFILE_IMPORT_TEMPLATE); toast.success('模板已复制') }
    catch { toast.error('复制失败，可以下载模板') }
  }
  return <Modal title="JSON 配置格式" onClose={onClose} width={660} footer={<><button type="button" className="btn" onClick={onClose}>知道了</button><button type="button" className="btn btn-primary" onClick={download}><Icon name="file" size="sm" />下载模板</button></>}>
    <p className="profile-import-intro">可以先导出已有配置，再按同样格式填写；也可以使用下面的模板。替换示例地址、模型名和向量维度后，再导入 JSON 文件。</p>
    <div className="profile-import-note"><Icon name="info" size="sm" /><p>导入后先预览，再确认编辑。API Key 需要在编辑页单独填写，文件中的密钥会被忽略。不会覆盖现有配置。</p></div>
    <div className="profile-import-code"><div><span>vidlens-ai-profile · v1</span><button type="button" className="btn btn-sm btn-ghost" onClick={() => void copy()}>复制模板</button></div><pre><code>{PROFILE_IMPORT_TEMPLATE}</code></pre></div>
    <h4 className="profile-import-heading">填写规则</h4>
    <div className="profile-import-fields"><table><tbody>
      <tr><th>必填内容</th><td>配置名称、对话 / 语音 / 向量三组服务商、地址和模型，以及向量维度。</td></tr>
      <tr><th>服务商</th><td><code>openai</code> 表示 OpenAI 兼容接口；也可从已有配置导出实际服务商值。</td></tr>
      <tr><th>接口地址</th><td><code>llm_base_url</code>、<code>asr_base_url</code> 填基础地址，例如以 <code>/v1</code> 结尾；<code>embedding_endpoint</code> 必须以 <code>/embeddings</code> 结尾。</td></tr>
      <tr><th>向量维度</th><td><code>embedding_dim</code> 要与模型实际输出维度一致；模板中的 1024 只是示例。</td></tr>
      <tr><th>可选内容</th><td><code>llm_context_tokens</code> 是模型单次请求的上下文窗口，0 表示未指定；自填范围为 8192–1048576，请按模型服务实际容量填写。它与 Agent 多次调用的累计预算分别设置。视觉服务的 <code>vision_provider</code>、<code>vision_base_url</code>、<code>vision_model</code> 需一起填写，也可全部省略。</td></tr>
    </tbody></table></div>
    <p className="profile-import-footnote">文件最大 100 KB，也支持不带 format / version 包装的扁平配置 JSON。建议使用模板或导出文件。</p>
  </Modal>
}
