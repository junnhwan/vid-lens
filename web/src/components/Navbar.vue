<template>
  <nav class="navbar" role="navigation" aria-label="主导航">
    <div class="nav-container">
      <div class="brand">
        <span class="mirror-icon">◇</span>
        <span class="brand-text">镜知 <span class="en">VidLens</span></span>
      </div>
      <button class="mobile-menu-btn" @click="$emit('toggleSidebar')" aria-label="切换侧边栏">☰</button>
      <div class="nav-right">
        <template v-if="user">
          <button class="btn-icon-text" @click="$emit('openConfig')" title="模型配置" aria-label="模型配置">
            <span class="icon">🤖</span>
          </button>
          <div class="user-badge">
            <span class="user-avatar">{{ user.nickname?.[0] || 'U' }}</span>
            <span class="user-name">{{ user.nickname || user.username }}</span>
          </div>
          <button class="btn-text" @click="$emit('logout')">退出</button>
        </template>
        <button v-else class="btn-amber" @click="$emit('openAuth')">登录 / 注册</button>
      </div>
    </div>
  </nav>
</template>

<script setup>
defineProps({
  user: Object
})

defineEmits(['logout', 'openAuth', 'openConfig', 'toggleSidebar'])
</script>

<style scoped>
/* 导航栏 */
.navbar {
  backdrop-filter: blur(24px) saturate(180%);
  background: rgba(10, 14, 26, 0.85);
  border-bottom: 1px solid rgba(212, 175, 55, 0.15);
  box-shadow: 0 4px 24px rgba(0, 0, 0, 0.4), inset 0 1px 0 rgba(255, 255, 255, 0.05);
  padding: 1.25rem 0;
  position: sticky;
  top: 0;
  z-index: 100;
}

.navbar::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 50%;
  transform: translateX(-50%);
  width: 60%;
  height: 1px;
  background: linear-gradient(90deg, transparent, rgba(212, 175, 55, 0.5), transparent);
}

.nav-container {
  max-width: 1600px;
  margin: 0 auto;
  padding: 0 3rem;
  display: flex;
  justify-content: space-between;
  align-items: center;
  position: relative;
  z-index: 2;
}

.brand {
  display: flex;
  align-items: center;
  gap: 1rem;
  font-size: 1.75rem;
  font-weight: 700;
  letter-spacing: 0.5px;
  position: relative;
}

.mirror-icon {
  font-size: 2.5rem;
  color: #d4af37;
  filter: drop-shadow(0 0 12px rgba(212, 175, 55, 0.7)) drop-shadow(0 0 4px rgba(41, 98, 255, 0.3));
  animation: iconPulse 3s ease-in-out infinite;
  transform-origin: center;
}

@keyframes iconPulse {
  0%, 100% { transform: scale(1) rotate(0deg); filter: drop-shadow(0 0 12px rgba(212, 175, 55, 0.7)) drop-shadow(0 0 4px rgba(41, 98, 255, 0.3)); }
  50% { transform: scale(1.05) rotate(5deg); filter: drop-shadow(0 0 18px rgba(212, 175, 55, 0.9)) drop-shadow(0 0 8px rgba(41, 98, 255, 0.5)); }
}

.brand-text {
  background: linear-gradient(135deg, #d4af37 0%, #f4e4a6 50%, #d4af37 100%);
  background-size: 200% auto;
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  animation: shimmer 4s linear infinite;
  position: relative;
  text-shadow: 0 0 20px rgba(212, 175, 55, 0.3);
}

@keyframes shimmer {
  to { background-position: 200% center; }
}

.brand-text .en {
  font-size: 0.65rem;
  opacity: 0.8;
  margin-left: 0.5rem;
  font-family: 'JetBrains Mono', monospace;
  letter-spacing: 1px;
  font-weight: 400;
}

/* 移动端菜单按钮 —— 默认隐藏 */
.mobile-menu-btn {
  display: none;
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.1), rgba(41, 98, 255, 0.08));
  border: 1px solid rgba(212, 175, 55, 0.3);
  font-size: 1.5rem;
  color: #d4af37;
  padding: 0.5rem 0.75rem;
  border-radius: 0.65rem;
  cursor: pointer;
  transition: all 0.3s;
}

.nav-right { display: flex; align-items: center; gap: 1.25rem; }

.btn-icon-text {
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.1), rgba(41, 98, 255, 0.08));
  border: 1px solid rgba(212, 175, 55, 0.3);
  padding: 0.65rem 1rem;
  border-radius: 0.75rem;
  cursor: pointer;
  transition: all 0.3s;
  display: flex;
  align-items: center;
  gap: 0.5rem;
  color: #d4af37;
  font-weight: 500;
  font-size: 0.9rem;
}

.btn-icon-text:hover {
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.2), rgba(41, 98, 255, 0.12));
  border-color: rgba(212, 175, 55, 0.5);
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(212, 175, 55, 0.2);
}

.btn-icon-text .icon {
  font-size: 1.2rem;
}

.user-badge {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.65rem 1.25rem;
  background: linear-gradient(135deg, rgba(212, 175, 55, 0.08), rgba(41, 98, 255, 0.08));
  backdrop-filter: blur(12px);
  border-radius: 2rem;
  border: 1px solid rgba(212, 175, 55, 0.25);
  box-shadow: 0 2px 12px rgba(212, 175, 55, 0.15), inset 0 1px 0 rgba(255, 255, 255, 0.1);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}
.user-badge:hover {
  border-color: rgba(212, 175, 55, 0.45);
  box-shadow: 0 4px 20px rgba(212, 175, 55, 0.25), inset 0 1px 0 rgba(255, 255, 255, 0.15);
  transform: translateY(-1px);
}
.user-avatar {
  width: 2.25rem;
  height: 2.25rem;
  border-radius: 50%;
  background: linear-gradient(135deg, #d4af37, #f4e4a6);
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  color: #0a0e1a;
  box-shadow: 0 0 12px rgba(212, 175, 55, 0.5), inset 0 1px 2px rgba(255, 255, 255, 0.3);
  font-size: 0.95rem;
}
.user-name { font-size: 0.95rem; font-weight: 500; color: #e8eef7; }
.btn-text {
  background: none;
  border: none;
  color: #8b95a8;
  cursor: pointer;
  font-size: 0.9rem;
  font-weight: 500;
  transition: all 0.3s;
  padding: 0.5rem 0.75rem;
  border-radius: 0.5rem;
}
.btn-text:hover {
  color: #d4af37;
  background: rgba(212, 175, 55, 0.08);
}
.btn-amber {
  background: linear-gradient(135deg, #d4af37 0%, #f4e4a6 50%, #d4af37 100%);
  background-size: 200% auto;
  color: #0a0e1a;
  border: none;
  padding: 0.75rem 1.75rem;
  border-radius: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.4s cubic-bezier(0.4, 0, 0.2, 1);
  box-shadow: 0 4px 16px rgba(212, 175, 55, 0.3), inset 0 1px 0 rgba(255, 255, 255, 0.3);
  font-size: 0.95rem;
  letter-spacing: 0.5px;
  position: relative;
  overflow: hidden;
}
.btn-amber::before {
  content: '';
  position: absolute;
  inset: 0;
  background: linear-gradient(135deg, transparent, rgba(255, 255, 255, 0.2), transparent);
  transform: translateX(-100%);
  transition: transform 0.6s;
}
.btn-amber:hover::before {
  transform: translateX(100%);
}
.btn-amber:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 24px rgba(212, 175, 55, 0.5), inset 0 1px 0 rgba(255, 255, 255, 0.4);
  background-position: 200% center;
}

/* 响应式：平板及以下 */
@media (max-width: 900px) {
  .nav-container {
    padding: 0 1.5rem;
  }
  .mobile-menu-btn {
    display: block;
  }
  .user-name {
    display: none;
  }
}

@media (max-width: 600px) {
  .brand {
    font-size: 1.35rem;
  }
  .mirror-icon {
    font-size: 1.8rem;
  }
  .nav-container {
    padding: 0 1rem;
  }
}
</style>
