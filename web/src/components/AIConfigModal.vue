<template>
  <transition name="modal">
    <div v-if="show" class="modal-backdrop" @mousedown.self="handleBackdropMouseDown" role="dialog" aria-modal="true" aria-label="模型配置">
      <div class="modal-panel">
        <button class="modal-close" @click="$emit('close')" aria-label="关闭">×</button>
        <h2>模型配置</h2>

        <div v-if="!isEditing" class="config-list">
          <div v-if="profiles.length === 0" class="empty-state">
            <div class="empty-icon" aria-hidden="true">◇</div>
            <p>还没有配置模型服务</p>
            <button class="btn-amber" @click="startCreate">新建配置</button>
          </div>
          <div v-else>
            <div class="config-actions">
              <button class="btn-amber" @click="startCreate">+ 新建配置</button>
            </div>
            <div class="profile-grid">
              <div v-for="profile in profiles" :key="profile.id" class="profile-card" :class="{ default: profile.is_default }">
                <div class="profile-header">
                  <h4>{{ profile.name }}</h4>
                  <span v-if="profile.is_default" class="default-badge">默认</span>
                </div>
                <div class="profile-details">
                  <div class="detail-row"><span class="label">LLM:</span><span class="value">{{ profile.llm_provider }} / {{ profile.llm_model }}</span></div>
                  <div class="detail-row"><span class="label">ASR:</span><span class="value">{{ profile.asr_provider }} / {{ profile.asr_model }}</span></div>
                  <div class="detail-row"><span class="label">Embedding:</span><span class="value">{{ profile.embedding_provider }} / {{ profile.embedding_model }}</span></div>
                </div>
                <div class="profile-actions">
                  <button class="action-btn" @click="startEdit(profile)">编辑</button>
                  <button class="action-btn test" @click="handleTest(profile)">测试</button>
                  <button class="action-btn danger" @click="handleDelete(profile.id)">删除</button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div v-else class="config-form">
          <div class="form-section">
            <h3>基本信息</h3>
            <div class="form-group">
              <label>配置名称 *</label>
              <input v-model="formData.name" placeholder="例如：我的模型配置" class="form-input" />
            </div>
            <div class="form-group checkbox-group">
              <label><input type="checkbox" v-model="formData.is_default" />设为默认配置</label>
            </div>
          </div>
          <div class="form-section">
            <h3>🗣️ ASR 配置</h3>
            <div class="form-group"><label>Provider *</label><input v-model="formData.asr_provider" placeholder="mimo" class="form-input" /></div>
            <div class="form-group"><label>Base URL *</label><input v-model="formData.asr_base_url" placeholder="https://..." class="form-input" /></div>
            <div class="form-group"><label>API Key {{ isEditMode ? '(留空不改)' : '*' }}</label><input v-model="formData.asr_api_key" type="password" :placeholder="isEditMode ? '保持原有' : 'tp-xxx'" class="form-input" /><div v-if="isEditMode && editingProfile?.asr_api_key_masked" class="masked-key">当前: {{ editingProfile.asr_api_key_masked }}</div></div>
            <div class="form-group"><label>Model *</label><input v-model="formData.asr_model" placeholder="mimo-v2.5-asr" class="form-input" /></div>
          </div>
          <div class="form-section">
            <h3>💬 LLM 配置</h3>
            <div class="form-group"><label>Provider *</label><input v-model="formData.llm_provider" placeholder="openai_compatible" class="form-input" /></div>
            <div class="form-group"><label>Base URL *</label><input v-model="formData.llm_base_url" placeholder="https://..." class="form-input" /></div>
            <div class="form-group"><label>API Key {{ isEditMode ? '(留空不改)' : '*' }}</label><input v-model="formData.llm_api_key" type="password" :placeholder="isEditMode ? '保持原有' : 'sk-xxx'" class="form-input" /><div v-if="isEditMode && editingProfile?.llm_api_key_masked" class="masked-key">当前: {{ editingProfile.llm_api_key_masked }}</div></div>
            <div class="form-group"><label>Model *</label><input v-model="formData.llm_model" placeholder="deepseek-chat" class="form-input" /></div>
          </div>
          <div class="form-section">
            <h3>🔍 Embedding 配置</h3>
            <div class="form-group"><label>Provider *</label><input v-model="formData.embedding_provider" placeholder="openai_compatible" class="form-input" /></div>
            <div class="form-group"><label>Endpoint *</label><input v-model="formData.embedding_endpoint" placeholder="https://.../embeddings" class="form-input" /></div>
            <div class="form-group"><label>API Key {{ isEditMode ? '(留空不改)' : '*' }}</label><input v-model="formData.embedding_api_key" type="password" :placeholder="isEditMode ? '保持原有' : 'sk-xxx'" class="form-input" /><div v-if="isEditMode && editingProfile?.embedding_api_key_masked" class="masked-key">当前: {{ editingProfile.embedding_api_key_masked }}</div></div>
            <div class="form-group"><label>Model *</label><input v-model="formData.embedding_model" placeholder="text-embedding-3-small" class="form-input" /></div>
            <div class="form-group"><label>Dimension *</label><input v-model.number="formData.embedding_dim" type="number" placeholder="1536" class="form-input" /></div>
          </div>
          <div class="form-actions">
            <button class="btn-secondary" @click="cancelEdit" :disabled="loading">取消</button>
            <button class="btn-amber" @click="handleSubmit" :disabled="loading">{{ loading ? '保存中...' : '保存配置' }}</button>
          </div>
        </div>

        <div v-if="testResult" class="test-result" :class="testResult.success ? 'success' : 'error'">
          <h4>{{ testResult.success ? '✅ 测试成功' : '❌ 测试失败' }}</h4>
          <p>{{ testResult.message }}</p>
        </div>

      </div>
    </div>
  </transition>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import api from '../api'
import { normalizeListResponse } from '../apiEnvelope.js'

defineProps({ show: Boolean })
const emit = defineEmits(['close', 'updated', 'showConfirm'])

const profiles = ref([])
const isEditing = ref(false)
const isEditMode = ref(false)
const editingProfile = ref(null)
const loading = ref(false)
const testResult = ref(null)
const formData = ref({
  name: '', asr_provider: '', asr_base_url: '', asr_api_key: '', asr_model: '',
  llm_provider: '', llm_base_url: '', llm_api_key: '', llm_model: '',
  embedding_provider: '', embedding_endpoint: '', embedding_api_key: '', embedding_model: '', embedding_dim: null, is_default: false,
})

const loadProfiles = async () => {
  try {
    const res = await api.getAIProfiles()
    profiles.value = normalizeListResponse(res)
  } catch (err) {
    console.error('加载配置失败:', err)
  }
}

const startCreate = () => {
  isEditing.value = true
  isEditMode.value = false
  editingProfile.value = null
  formData.value = { name: '', asr_provider: '', asr_base_url: '', asr_api_key: '', asr_model: '', llm_provider: '', llm_base_url: '', llm_api_key: '', llm_model: '', embedding_provider: '', embedding_endpoint: '', embedding_api_key: '', embedding_model: '', embedding_dim: null, is_default: false }
  testResult.value = null
}

const startEdit = (profile) => {
  isEditing.value = true
  isEditMode.value = true
  editingProfile.value = profile
  formData.value = {
    name: profile.name, asr_provider: profile.asr_provider, asr_base_url: profile.asr_base_url, asr_api_key: '', asr_model: profile.asr_model,
    llm_provider: profile.llm_provider, llm_base_url: profile.llm_base_url, llm_api_key: '', llm_model: profile.llm_model,
    embedding_provider: profile.embedding_provider, embedding_endpoint: profile.embedding_endpoint, embedding_api_key: '', embedding_model: profile.embedding_model, embedding_dim: profile.embedding_dim, is_default: profile.is_default,
  }
  testResult.value = null
}

const cancelEdit = () => { isEditing.value = false; editingProfile.value = null }

const handleSubmit = async () => {
  if (!formData.value.name) {
    emit('showConfirm', { title: '提示', message: '请输入配置名称', confirmText: '知道了', showCancel: false, type: 'warning', icon: '📝' })
    return
  }
  loading.value = true
  try {
    const payload = { ...formData.value }
    if (isEditMode.value) {
      if (!payload.asr_api_key) delete payload.asr_api_key
      if (!payload.llm_api_key) delete payload.llm_api_key
      if (!payload.embedding_api_key) delete payload.embedding_api_key
    }
    if (isEditMode.value) { await api.updateAIProfile(editingProfile.value.id, payload) }
    else { await api.createAIProfile(payload) }
    await loadProfiles()
    cancelEdit()
    emit('updated')
  } catch (err) {
    emit('showConfirm', { title: '保存失败', message: err.message || '保存失败', confirmText: '知道了', showCancel: false, type: 'danger', icon: '❌' })
  } finally {
    loading.value = false
  }
}

const handleTest = async (profile) => {
  testResult.value = null
  loading.value = true
  try {
    const res = await api.testAIProfile({ id: profile.id })
    testResult.value = { success: true, message: res.message || '所有服务连接正常' }
  } catch (err) {
    testResult.value = { success: false, message: err.message || '测试失败' }
  } finally {
    loading.value = false
  }
}

const handleDelete = async (id) => {
  emit('showConfirm', {
    title: '确认删除',
    message: '此操作不可恢复，确定要删除这条模型配置吗？',
    confirmText: '删除',
    showCancel: true,
    type: 'danger',
    icon: '🗑️',
    onConfirm: async () => {
      try {
        await api.deleteAIProfile(id)
        await loadProfiles()
        emit('updated')
      } catch (err) {
        emit('showConfirm', { title: '删除失败', message: err.message || '删除失败', confirmText: '知道了', showCancel: false, type: 'danger', icon: '❌' })
      }
    }
  })
}

const handleBackdropMouseDown = (e) => {
  const startTarget = e.target
  const handleMouseUp = (upEvent) => {
    if (upEvent.target === startTarget && startTarget.classList.contains('modal-backdrop')) {
      emit('close')
    }
    document.removeEventListener('mouseup', handleMouseUp)
  }
  document.addEventListener('mouseup', handleMouseUp)
}

// ESC 关闭
const onKeyDown = (e) => {
  if (e.key === 'Escape') {
    emit('close')
  }
}
onMounted(() => document.addEventListener('keydown', onKeyDown))
onUnmounted(() => document.removeEventListener('keydown', onKeyDown))

defineExpose({ loadProfiles })
</script>

<style scoped>
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.72);
  backdrop-filter: blur(10px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 1.25rem;
  z-index: 1100;
  overflow-y: auto;
}

.modal-panel {
  width: min(900px, 100%);
  max-height: min(90vh, 100%);
  margin: auto;
  backdrop-filter: blur(20px) saturate(160%);
  background: var(--vl-panel, rgba(12, 16, 24, 0.96));
  border: 1px solid var(--vl-border-strong, rgba(148, 163, 184, 0.22));
  border-radius: var(--vl-radius-xl, 1.25rem);
  padding: 1.75rem 1.75rem 1.5rem;
  position: relative;
  box-shadow: var(--vl-shadow, 0 24px 64px rgba(0, 0, 0, 0.55));
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: rgba(45, 212, 191, 0.3) transparent;
}

.modal-panel::-webkit-scrollbar { width: 8px; }
.modal-panel::-webkit-scrollbar-thumb { background: rgba(45, 212, 191, 0.3); border-radius: 4px; }

.modal-close {
  position: absolute;
  top: 1.25rem;
  right: 1.25rem;
  background: rgba(239, 68, 68, 0.1);
  border: 1px solid rgba(239, 68, 68, 0.3);
  width: 2.5rem;
  height: 2.5rem;
  border-radius: 50%;
  color: var(--vl-danger);
  font-size: 1.5rem;
  cursor: pointer;
  transition: all 0.3s;
}

.modal-close:hover {
  background: rgba(239, 68, 68, 0.2);
  transform: rotate(90deg);
}

.modal-panel h2 {
  margin: 0 0 1.25rem;
  padding-right: 2.5rem;
  font-family: var(--vl-font-display, inherit);
  font-size: 1.25rem;
  font-weight: 700;
  letter-spacing: 0.02em;
  color: var(--vl-text, #e8eef6);
}

.empty-state { text-align: center; padding: 3rem; }
.empty-icon { font-size: 4rem; margin-bottom: 1rem; }
.empty-state p { color: var(--vl-text-secondary); margin-bottom: 2rem; }
.config-actions { margin-bottom: 1.5rem; }
.profile-grid { display: grid; gap: 1.25rem; }

.profile-card {
  background: linear-gradient(135deg, rgba(16, 22, 34, 0.6), rgba(17, 24, 39, 0.4));
  border: 1px solid rgba(139, 149, 168, 0.2);
  border-radius: 1rem;
  padding: 1.5rem;
  transition: all 0.3s;
}

.profile-card.default { border-color: rgba(45, 212, 191, 0.4); }
.profile-card:hover { border-color: rgba(45, 212, 191, 0.3); box-shadow: 0 4px 16px rgba(45, 212, 191, 0.15); }
.profile-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 1rem; }
.profile-header h4 { font-size: 1.15rem; font-weight: 600; color: var(--vl-text); }

.default-badge {
  background: linear-gradient(135deg, rgba(45, 212, 191, 0.2), rgba(96, 165, 250, 0.15));
  border: 1px solid rgba(45, 212, 191, 0.4);
  color: var(--vl-primary);
  padding: 0.25rem 0.75rem;
  border-radius: 0.5rem;
  font-size: 0.8rem;
  font-weight: 600;
}

.profile-details { margin-bottom: 1rem; }
.detail-row { display: flex; gap: 0.5rem; margin-bottom: 0.5rem; font-size: 0.9rem; }
.detail-row .label { color: var(--vl-text-secondary); min-width: 90px; }
.detail-row .value { color: #b8c5db; font-family: var(--vl-font-mono); }
.profile-actions { display: flex; gap: 0.75rem; }

.action-btn {
  flex: 1;
  background: linear-gradient(135deg, rgba(16, 22, 34, 0.5), rgba(17, 24, 39, 0.4));
  border: 1px solid rgba(139, 149, 168, 0.25);
  padding: 0.65rem 1rem;
  border-radius: 0.65rem;
  color: var(--vl-text-secondary);
  cursor: pointer;
  transition: all 0.3s;
  font-size: 0.9rem;
  font-weight: 500;
}

.action-btn:hover { border-color: rgba(45, 212, 191, 0.5); color: var(--vl-primary); }
.action-btn.test { color: var(--vl-info); border-color: rgba(96, 165, 250, 0.3); }
.action-btn.test:hover { border-color: rgba(96, 165, 250, 0.6); }
.action-btn.danger { color: var(--vl-danger); border-color: rgba(239, 68, 68, 0.3); }
.action-btn.danger:hover { border-color: rgba(239, 68, 68, 0.6); }

.config-form { display: flex; flex-direction: column; gap: 1.5rem; }
.form-section h3 { font-size: 1.1rem; font-weight: 600; color: var(--vl-primary); margin-bottom: 1rem; }
.form-group { margin-bottom: 1rem; }
.form-group label { display: block; color: var(--vl-text-secondary); font-size: 0.9rem; margin-bottom: 0.5rem; font-weight: 500; }
.form-group.checkbox-group label { display: inline; }

.form-input {
  width: 100%;
  box-sizing: border-box;
  background: rgba(7, 9, 15, 0.6);
  border: 1px solid rgba(139, 149, 168, 0.2);
  padding: 0.75rem 1rem;
  border-radius: 0.75rem;
  color: var(--vl-text);
  outline: none;
  transition: all 0.3s;
  font-size: 0.9rem;
}

.form-input:focus { border-color: var(--vl-primary); box-shadow: 0 0 0 3px rgba(45, 212, 191, 0.15); }
.masked-key { margin-top: 0.5rem; font-size: 0.85rem; color: var(--vl-text-muted); font-family: var(--vl-font-mono); }
.form-actions { display: flex; gap: 1rem; justify-content: flex-end; margin-top: 1rem; }

.btn-amber {
  background: var(--vl-primary);
  color: var(--vl-text-inverse);
  border: none;
  padding: 0.75rem 1.75rem;
  border-radius: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.3s;
  font-size: 0.95rem;
}

.btn-amber:hover { transform: translateY(-2px); box-shadow: 0 6px 24px rgba(45, 212, 191, 0.4); }
.btn-amber:disabled { opacity: 0.5; cursor: not-allowed; }

.btn-secondary {
  background: rgba(139, 149, 168, 0.1);
  border: 1px solid rgba(139, 149, 168, 0.3);
  color: var(--vl-text-secondary);
  padding: 0.75rem 1.75rem;
  border-radius: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.3s;
  font-size: 0.95rem;
}

.btn-secondary:hover { border-color: rgba(139, 149, 168, 0.5); }

.test-result { margin-top: 1.5rem; padding: 1.25rem; border-radius: 0.875rem; backdrop-filter: blur(8px); }
.test-result.success { background: linear-gradient(135deg, rgba(34, 197, 94, 0.15), rgba(22, 163, 74, 0.1)); border: 1px solid rgba(34, 197, 94, 0.3); color: var(--vl-success); }
.test-result.error { background: linear-gradient(135deg, rgba(239, 68, 68, 0.15), rgba(220, 38, 38, 0.1)); border: 1px solid rgba(239, 68, 68, 0.3); color: var(--vl-danger); }
.test-result h4 { margin-bottom: 0.5rem; font-size: 1rem; }
.test-result p { font-size: 0.9rem; line-height: 1.6; }

.modal-enter-active, .modal-leave-active { transition: all 0.3s; }
.modal-enter-from, .modal-leave-to { opacity: 0; }
.modal-enter-from .modal-panel, .modal-leave-to .modal-panel { transform: scale(0.9); }

/* 响应式 */
@media (max-width: 600px) {
  .modal-panel {
    width: 100%;
    max-width: 100%;
    max-height: 100vh;
    border-radius: 0;
    padding: 1.5rem;
  }
}
</style>
