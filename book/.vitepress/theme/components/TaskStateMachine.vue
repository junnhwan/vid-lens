<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'

interface State {
  id: number
  name: string
  code: string
  stage: string
  x: number
  y: number
  color: string
  colorDark: string
  category: 'normal' | 'fail' | 'dead' | 'success'
}

interface Transition {
  from: number
  to: number
  label: string
  color: string
  colorDark: string
  category: 'normal' | 'fail' | 'dead'
  // SVG path data
  path: string
  // Arrow tip position & rotation
  tipX: number
  tipY: number
  tipAngle: number
  // Label position
  labelX: number
  labelY: number
}

const isDark = ref(false)
const selectedState = ref<number | null>(null)

// ── States ──
const states: State[] = [
  { id: 0, name: 'Pending',   code: '0', stage: 'Job 创建后',     x: 80,  y: 100, color: '#3b82f6', colorDark: '#60a5fa', category: 'normal' },
  { id: 1, name: 'Queued',    code: '1', stage: 'Kafka Topic',     x: 280, y: 100, color: '#3b82f6', colorDark: '#60a5fa', category: 'normal' },
  { id: 2, name: 'Running',   code: '2', stage: 'Worker 消费处理', x: 480, y: 100, color: '#3b82f6', colorDark: '#60a5fa', category: 'normal' },
  { id: 3, name: 'Completed', code: '3', stage: '输出落盘',        x: 480, y: 280, color: '#22c55e', colorDark: '#4ade80', category: 'success' },
  { id: 4, name: 'Failed',    code: '4', stage: '错误记录 & 重试', x: 280, y: 280, color: '#f59e0b', colorDark: '#fbbf24', category: 'fail' },
  { id: 5, name: 'Dead',      code: '5', stage: '死信队列 (DLQ)',  x: 80,  y: 280, color: '#ef4444', colorDark: '#f87171', category: 'dead' },
]

// ── Transitions ──
const transitions: Transition[] = [
  // Pending -> Queued (straight right)
  {
    from: 0, to: 1, label: 'Kafka 投递', category: 'normal',
    color: '#3b82f6', colorDark: '#60a5fa',
    path: 'M 125,100 L 235,100',
    tipX: 235, tipY: 100, tipAngle: 0,
    labelX: 180, labelY: 88,
  },
  // Queued -> Running (straight right)
  {
    from: 1, to: 2, label: '开始处理', category: 'normal',
    color: '#3b82f6', colorDark: '#60a5fa',
    path: 'M 325,100 L 435,100',
    tipX: 435, tipY: 100, tipAngle: 0,
    labelX: 380, labelY: 88,
  },
  // Running -> Completed (straight down)
  {
    from: 2, to: 3, label: '处理完成', category: 'normal',
    color: '#22c55e', colorDark: '#4ade80',
    path: 'M 480,145 L 480,235',
    tipX: 480, tipY: 235, tipAngle: 90,
    labelX: 492, labelY: 195,
  },
  // Running -> Failed (diagonal down-left)
  {
    from: 2, to: 4, label: '处理失败', category: 'fail',
    color: '#f59e0b', colorDark: '#fbbf24',
    path: 'M 435,145 L 325,235',
    tipX: 325, tipY: 235, tipAngle: 135,
    labelX: 355, labelY: 170,
  },
  // Failed -> Queued (vertical up, retry loop)
  {
    from: 4, to: 1, label: '重试', category: 'fail',
    color: '#f59e0b', colorDark: '#fbbf24',
    path: 'M 310,235 C 370,190 370,155 310,145',
    tipX: 310, tipY: 145, tipAngle: -90,
    labelX: 375, labelY: 180,
  },
  // Failed -> Dead (straight left)
  {
    from: 4, to: 5, label: '重试耗尽', category: 'dead',
    color: '#ef4444', colorDark: '#f87171',
    path: 'M 235,280 L 125,280',
    tipX: 125, tipY: 280, tipAngle: 180,
    labelX: 180, labelY: 268,
  },
  // Pending -> Dead (straight down, edge case)
  {
    from: 0, to: 5, label: '超时 / 手动终止', category: 'dead',
    color: '#ef4444', colorDark: '#f87171',
    path: 'M 80,145 L 80,235',
    tipX: 80, tipY: 235, tipAngle: 90,
    labelX: 28, labelY: 195,
  },
]

const NODE_RADIUS = 45

function selectState(id: number) {
  selectedState.value = selectedState.value === id ? null : id
}

function isTransitionHighlighted(t: Transition): boolean {
  if (selectedState.value === null) return false
  return t.from === selectedState.value || t.to === selectedState.value
}

function isStateHighlighted(s: State): boolean {
  if (selectedState.value === null) return true
  if (s.id === selectedState.value) return true
  return transitions.some(
    t => (t.from === selectedState.value && t.to === s.id) ||
         (t.to === selectedState.value && t.from === s.id)
  )
}

function isTransitionDimmed(t: Transition): boolean {
  if (selectedState.value === null) return false
  return !isTransitionHighlighted(t)
}

const incomingTransitions = computed(() => {
  if (selectedState.value === null) return []
  return transitions.filter(t => t.to === selectedState.value)
})

const outgoingTransitions = computed(() => {
  if (selectedState.value === null) return []
  return transitions.filter(t => t.from === selectedState.value)
})

const selectedStateInfo = computed(() => {
  if (selectedState.value === null) return null
  return states.find(s => s.id === selectedState.value)!
})

// Dark mode detection
let observer: MutationObserver | null = null
onMounted(() => {
  isDark.value = document.documentElement.classList.contains('dark')
  observer = new MutationObserver(() => {
    isDark.value = document.documentElement.classList.contains('dark')
  })
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
})
onUnmounted(() => {
  observer?.disconnect()
})

function colorOf(s: State) {
  return isDark.value ? s.colorDark : s.color
}
function transColorOf(t: Transition) {
  return isDark.value ? t.colorDark : t.color
}

function polarToCartesian(cx: number, cy: number, r: number, angleDeg: number) {
  const rad = ((angleDeg - 90) * Math.PI) / 180
  return { x: cx + r * Math.cos(rad), y: cy + r * Math.sin(rad) }
}

function describeArc(cx: number, cy: number, r: number, startAngle: number, endAngle: number): string {
  const start = polarToCartesian(cx, cy, r, endAngle)
  const end = polarToCartesian(cx, cy, r, startAngle)
  const largeArc = endAngle - startAngle <= 180 ? '0' : '1'
  return `M ${start.x} ${start.y} A ${r} ${r} 0 ${largeArc} 0 ${end.x} ${end.y}`
}

function arrowMarker(t: { category: string; color: string; colorDark: string }, dim: boolean) {
  const prefix = dim ? '-dim' : ''
  switch (t.category) {
    case 'fail':  return `url(#ah-amber${prefix})`
    case 'dead':  return `url(#ah-red${prefix})`
    case 'normal':
    default:
      if (t.color === '#22c55e' || t.colorDark === '#4ade80') return `url(#ah-green${prefix})`
      return `url(#ah-blue${prefix})`
  }
}
</script>

<template>
  <div class="tsm-wrapper">
    <!-- Header -->
    <div class="tsm-header">
      <div class="tsm-title">Task State Machine</div>
      <div class="tsm-subtitle">点击任意状态节点，查看入边和出边转换</div>
    </div>

    <!-- Legend -->
    <div class="tsm-legend">
      <span class="tsm-legend-item">
        <span class="tsm-dot" style="background:#3b82f6"></span> 正常流转
      </span>
      <span class="tsm-legend-item">
        <span class="tsm-dot" style="background:#f59e0b"></span> 失败路径
      </span>
      <span class="tsm-legend-item">
        <span class="tsm-dot" style="background:#22c55e"></span> 成功终态
      </span>
      <span class="tsm-legend-item">
        <span class="tsm-dot" style="background:#ef4444"></span> 死信终态
      </span>
    </div>

    <!-- SVG Diagram -->
    <div class="tsm-diagram">
      <svg viewBox="0 0 560 380" xmlns="http://www.w3.org/2000/svg">
        <!-- Defs: arrowheads + glow filter -->
        <defs>
          <!-- Blue arrowhead -->
          <marker id="ah-blue" markerWidth="10" markerHeight="8" refX="9" refY="4" orient="auto">
            <path d="M 0,0 L 10,4 L 0,8 Z" :fill="isDark ? '#60a5fa' : '#3b82f6'" />
          </marker>
          <marker id="ah-blue-dim" markerWidth="10" markerHeight="8" refX="9" refY="4" orient="auto">
            <path d="M 0,0 L 10,4 L 0,8 Z" :fill="isDark ? '#1e3a5f' : '#bfdbfe'" />
          </marker>
          <!-- Amber arrowhead -->
          <marker id="ah-amber" markerWidth="10" markerHeight="8" refX="9" refY="4" orient="auto">
            <path d="M 0,0 L 10,4 L 0,8 Z" :fill="isDark ? '#fbbf24' : '#f59e0b'" />
          </marker>
          <marker id="ah-amber-dim" markerWidth="10" markerHeight="8" refX="9" refY="4" orient="auto">
            <path d="M 0,0 L 10,4 L 0,8 Z" :fill="isDark ? '#4a3a10' : '#fde68a'" />
          </marker>
          <!-- Green arrowhead -->
          <marker id="ah-green" markerWidth="10" markerHeight="8" refX="9" refY="4" orient="auto">
            <path d="M 0,0 L 10,4 L 0,8 Z" :fill="isDark ? '#4ade80' : '#22c55e'" />
          </marker>
          <marker id="ah-green-dim" markerWidth="10" markerHeight="8" refX="9" refY="4" orient="auto">
            <path d="M 0,0 L 10,4 L 0,8 Z" :fill="isDark ? '#0d3320' : '#bbf7d0'" />
          </marker>
          <!-- Red arrowhead -->
          <marker id="ah-red" markerWidth="10" markerHeight="8" refX="9" refY="4" orient="auto">
            <path d="M 0,0 L 10,4 L 0,8 Z" :fill="isDark ? '#f87171' : '#ef4444'" />
          </marker>
          <marker id="ah-red-dim" markerWidth="10" markerHeight="8" refX="9" refY="4" orient="auto">
            <path d="M 0,0 L 10,4 L 0,8 Z" :fill="isDark ? '#4a1515' : '#fecaca'" />
          </marker>
          <!-- Glow filter for selected -->
          <filter id="glow-blue" x="-40%" y="-40%" width="180%" height="180%">
            <feGaussianBlur stdDeviation="6" result="blur" />
            <feMerge><feMergeNode in="blur" /><feMergeNode in="SourceGraphic" /></feMerge>
          </filter>
          <filter id="glow-green" x="-40%" y="-40%" width="180%" height="180%">
            <feGaussianBlur stdDeviation="6" result="blur" />
            <feMerge><feMergeNode in="blur" /><feMergeNode in="SourceGraphic" /></feMerge>
          </filter>
          <filter id="glow-amber" x="-40%" y="-40%" width="180%" height="180%">
            <feGaussianBlur stdDeviation="6" result="blur" />
            <feMerge><feMergeNode in="blur" /><feMergeNode in="SourceGraphic" /></feMerge>
          </filter>
          <filter id="glow-red" x="-40%" y="-40%" width="180%" height="180%">
            <feGaussianBlur stdDeviation="6" result="blur" />
            <feMerge><feMergeNode in="blur" /><feMergeNode in="SourceGraphic" /></feMerge>
          </filter>
        </defs>

        <!-- Transitions (edges) -->
        <g v-for="(t, i) in transitions" :key="'edge-' + i">
          <!-- Glow layer when highlighted -->
          <path
            v-if="isTransitionHighlighted(t)"
            :d="t.path"
            fill="none"
            :stroke="transColorOf(t)"
            stroke-width="6"
            :filter="t.category === 'dead' ? 'url(#glow-red)' : t.category === 'fail' ? 'url(#glow-amber)' : t.to === 3 ? 'url(#glow-green)' : 'url(#glow-blue)'"
            stroke-linecap="round"
            class="tsm-edge-glow"
          />
          <!-- Main path -->
          <path
            :d="t.path"
            fill="none"
            :stroke="isTransitionDimmed(t) ? (isDark ? '#2a2a2a' : '#e5e7eb') : transColorOf(t)"
            :stroke-width="isTransitionHighlighted(t) ? 3 : 2"
            :stroke-dasharray="t.category === 'dead' ? '6,4' : t.category === 'fail' ? '8,4' : 'none'"
            :marker-end="arrowMarker(t, isTransitionDimmed(t))"
            :stroke-linecap="'round'"
            class="tsm-edge"
            :class="{ 'tsm-edge-active': isTransitionHighlighted(t), 'tsm-edge-dim': isTransitionDimmed(t) }"
          />
          <!-- Edge label -->
          <text
            :x="t.labelX"
            :y="t.labelY"
            :fill="isTransitionDimmed(t) ? (isDark ? '#555' : '#bbb') : (isDark ? '#ccc' : '#555')"
            font-size="11"
            font-family="'JetBrains Mono', monospace"
            font-weight="500"
            :text-anchor="t.from === 4 && t.to === 1 ? 'middle' : 'middle'"
            class="tsm-edge-label"
            :class="{ 'tsm-edge-label-active': isTransitionHighlighted(t) }"
          >
            {{ t.label }}
          </text>
        </g>

        <!-- States (nodes) -->
        <g
          v-for="s in states"
          :key="'node-' + s.id"
          class="tsm-node"
          :class="{ 'tsm-node-selected': selectedState === s.id }"
          @click="selectState(s.id)"
          style="cursor: pointer"
        >
          <!-- Outer ring glow when selected -->
          <circle
            v-if="selectedState === s.id"
            :cx="s.x"
            :cy="s.y"
            :r="NODE_RADIUS + 8"
            :fill="'none'"
            :stroke="colorOf(s)"
            stroke-width="2"
            opacity="0.4"
            class="tsm-ring-pulse"
          />
          <!-- Node background -->
          <circle
            :cx="s.x"
            :cy="s.y"
            :r="NODE_RADIUS"
            :fill="isDark ? '#1a1a1a' : '#ffffff'"
            :stroke="isStateHighlighted(s) ? colorOf(s) : (isDark ? '#333' : '#ddd')"
            :stroke-width="selectedState === s.id ? 3 : 2"
            class="tsm-node-circle"
          />
          <!-- Category indicator arc -->
          <path
            :d="describeArc(s.x, s.y, NODE_RADIUS - 3, -60, 60)"
            fill="none"
            :stroke="colorOf(s)"
            stroke-width="3"
            stroke-linecap="round"
            :opacity="isStateHighlighted(s) ? 0.6 : 0.15"
          />
          <!-- State code badge -->
          <rect
            :x="s.x - 10"
            :y="s.y - 28"
            width="20"
            height="16"
            rx="4"
            :fill="isStateHighlighted(s) ? colorOf(s) : (isDark ? '#333' : '#eee')"
          />
          <text
            :x="s.x"
            :y="s.y - 17"
            text-anchor="middle"
            font-size="10"
            font-family="'JetBrains Mono', monospace"
            font-weight="700"
            :fill="isStateHighlighted(s) ? '#fff' : (isDark ? '#999' : '#888')"
          >
            {{ s.code }}
          </text>
          <!-- State name -->
          <text
            :x="s.x"
            :y="s.y + 4"
            text-anchor="middle"
            font-size="13"
            font-weight="700"
            font-family="'Space Grotesk', sans-serif"
            :fill="isStateHighlighted(s) ? colorOf(s) : (isDark ? '#888' : '#666')"
          >
            {{ s.name }}
          </text>
          <!-- Stage subtitle -->
          <text
            :x="s.x"
            :y="s.y + 20"
            text-anchor="middle"
            font-size="9"
            font-family="'JetBrains Mono', monospace"
            :fill="isDark ? '#666' : '#999'"
          >
            {{ s.stage }}
          </text>
        </g>
      </svg>
    </div>

    <!-- Detail Panel -->
    <Transition name="tsm-panel">
      <div v-if="selectedStateInfo" class="tsm-detail">
        <div class="tsm-detail-header">
          <span
            class="tsm-detail-badge"
            :style="{ background: colorOf(selectedStateInfo), color: '#fff' }"
          >
            {{ selectedStateInfo.code }}
          </span>
          <span class="tsm-detail-name">{{ selectedStateInfo.name }}</span>
          <span class="tsm-detail-stage">{{ selectedStateInfo.stage }}</span>
          <button class="tsm-close" @click="selectedState = null" aria-label="Close">&times;</button>
        </div>

        <div class="tsm-detail-body">
          <!-- Incoming -->
          <div class="tsm-detail-section">
            <div class="tsm-detail-section-title tsm-incoming-title">
              <span class="tsm-arrow-icon">&#x2192;</span> 入边 (Ingoing)
            </div>
            <div v-if="incomingTransitions.length === 0" class="tsm-empty">无入边 -- 初始状态</div>
            <div v-for="t in incomingTransitions" :key="'in-' + t.from + '-' + t.to" class="tsm-trans-item">
              <span class="tsm-trans-node" :style="{ borderColor: colorOf(states[t.from]) }">
                {{ states[t.from].name }}
              </span>
              <span class="tsm-trans-arrow" :style="{ color: transColorOf(t) }">&#x2192;</span>
              <span class="tsm-trans-label">{{ t.label }}</span>
            </div>
          </div>
          <!-- Outgoing -->
          <div class="tsm-detail-section">
            <div class="tsm-detail-section-title tsm-outgoing-title">
              <span class="tsm-arrow-icon">&#x2192;</span> 出边 (Outgoing)
            </div>
            <div v-if="outgoingTransitions.length === 0" class="tsm-empty">无出边 -- 终态</div>
            <div v-for="t in outgoingTransitions" :key="'out-' + t.from + '-' + t.to" class="tsm-trans-item">
              <span class="tsm-trans-label">{{ t.label }}</span>
              <span class="tsm-trans-arrow" :style="{ color: transColorOf(t) }">&#x2192;</span>
              <span class="tsm-trans-node" :style="{ borderColor: colorOf(states[t.to]) }">
                {{ states[t.to].name }}
              </span>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.tsm-wrapper {
  margin: 24px 0;
  border: 1px solid var(--vp-c-divider);
  border-radius: 12px;
  overflow: hidden;
  background: var(--vp-c-bg);
}

/* ── Header ── */
.tsm-header {
  padding: 20px 24px 12px;
  border-bottom: 1px solid var(--vp-c-divider);
  background: var(--vp-c-bg-soft);
}
.tsm-title {
  font-family: 'Space Grotesk', sans-serif;
  font-size: 18px;
  font-weight: 700;
  color: var(--vp-c-text-1);
  letter-spacing: -0.02em;
}
.tsm-subtitle {
  font-size: 13px;
  color: var(--vp-c-text-3);
  margin-top: 4px;
}

/* ── Legend ── */
.tsm-legend {
  display: flex;
  gap: 16px;
  padding: 10px 24px;
  border-bottom: 1px solid var(--vp-c-divider);
  background: var(--vp-c-bg-soft);
  flex-wrap: wrap;
}
.tsm-legend-item {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--vp-c-text-2);
  font-family: 'JetBrains Mono', monospace;
}
.tsm-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

/* ── SVG Diagram ── */
.tsm-diagram {
  padding: 16px 24px;
  display: flex;
  justify-content: center;
}
.tsm-diagram svg {
  width: 100%;
  max-width: 560px;
  height: auto;
}

/* ── Node animations ── */
.tsm-node:hover .tsm-node-circle {
  filter: brightness(1.05);
}
.tsm-node-selected .tsm-node-circle {
  transition: stroke-width 0.2s ease;
}
.tsm-ring-pulse {
  animation: ring-pulse 2s ease-in-out infinite;
}
@keyframes ring-pulse {
  0%, 100% { opacity: 0.4; }
  50% { opacity: 0.15; }
}

/* ── Edge animations ── */
.tsm-edge {
  transition: stroke 0.3s ease, stroke-width 0.3s ease, opacity 0.3s ease;
}
.tsm-edge-active {
  animation: dash-flow 1.2s linear infinite;
}
.tsm-edge-glow {
  transition: opacity 0.3s ease;
}
.tsm-edge-label {
  transition: fill 0.3s ease, font-size 0.2s ease;
}
.tsm-edge-label-active {
  font-weight: 700 !important;
}

@keyframes dash-flow {
  0% { stroke-dashoffset: 0; }
  100% { stroke-dashoffset: -24; }
}

/* ── Detail Panel ── */
.tsm-detail {
  border-top: 1px solid var(--vp-c-divider);
  background: var(--vp-c-bg-soft);
}
.tsm-detail-header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 14px 24px;
  border-bottom: 1px solid var(--vp-c-divider);
}
.tsm-detail-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 6px;
  font-family: 'JetBrains Mono', monospace;
  font-size: 13px;
  font-weight: 700;
}
.tsm-detail-name {
  font-family: 'Space Grotesk', sans-serif;
  font-size: 16px;
  font-weight: 700;
  color: var(--vp-c-text-1);
}
.tsm-detail-stage {
  font-size: 12px;
  color: var(--vp-c-text-3);
  font-family: 'JetBrains Mono', monospace;
}
.tsm-close {
  margin-left: auto;
  background: none;
  border: 1px solid var(--vp-c-divider);
  border-radius: 6px;
  width: 28px;
  height: 28px;
  font-size: 16px;
  color: var(--vp-c-text-2);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.15s ease;
}
.tsm-close:hover {
  background: var(--vp-c-bg-mute);
}

.tsm-detail-body {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0;
}
@media (max-width: 500px) {
  .tsm-detail-body {
    grid-template-columns: 1fr;
  }
}

.tsm-detail-section {
  padding: 14px 24px;
}
.tsm-detail-section:first-child {
  border-right: 1px solid var(--vp-c-divider);
}
@media (max-width: 500px) {
  .tsm-detail-section:first-child {
    border-right: none;
    border-bottom: 1px solid var(--vp-c-divider);
  }
}

.tsm-detail-section-title {
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  margin-bottom: 10px;
  font-family: 'JetBrains Mono', monospace;
}
.tsm-incoming-title {
  color: #3b82f6;
}
.tsm-outgoing-title {
  color: #8b5cf6;
}

.tsm-arrow-icon {
  font-size: 14px;
}

.tsm-empty {
  font-size: 12px;
  color: var(--vp-c-text-3);
  font-style: italic;
}

.tsm-trans-item {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
  font-size: 13px;
}
.tsm-trans-node {
  display: inline-block;
  padding: 2px 8px;
  border: 1.5px solid;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 600;
  font-family: 'JetBrains Mono', monospace;
  color: var(--vp-c-text-1);
  background: var(--vp-c-bg);
}
.tsm-trans-arrow {
  font-size: 14px;
  font-weight: 700;
}
.tsm-trans-label {
  font-size: 12px;
  color: var(--vp-c-text-2);
  font-family: 'JetBrains Mono', monospace;
}

/* ── Panel transition ── */
.tsm-panel-enter-active,
.tsm-panel-leave-active {
  transition: max-height 0.3s ease, opacity 0.3s ease;
  overflow: hidden;
}
.tsm-panel-enter-from,
.tsm-panel-leave-to {
  max-height: 0;
  opacity: 0;
}
.tsm-panel-enter-to,
.tsm-panel-leave-from {
  max-height: 300px;
  opacity: 1;
}
</style>
