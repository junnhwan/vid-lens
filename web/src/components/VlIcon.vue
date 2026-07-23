<template>
  <component
    :is="iconComponent"
    class="vl-icon"
    :class="[`size-${size}`, { spin }]"
    :size="pixelSize"
    :stroke-width="strokeWidth"
    :aria-hidden="ariaHidden"
    v-bind="ariaLabel ? { 'aria-label': ariaLabel, role: 'img' } : {}"
  />
</template>

<script setup>
import { computed } from 'vue'
import {
  AlertTriangle,
  ArrowLeft,
  ArrowUp,
  Ban,
  Bot,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  ChevronUp,
  CircleAlert,
  Clapperboard,
  Clock,
  Copy,
  Download,
  FileText,
  HelpCircle,
  Inbox,
  Library,
  Loader2,
  Menu,
  MessageSquare,
  Palette,
  Pause,
  Plus,
  RefreshCw,
  RotateCw,
  Search,
  Settings,
  Sparkles,
  Trash2,
  Upload,
  WifiOff,
  X,
  XCircle,
} from 'lucide-vue-next'
import { resolveIconKey } from '../icons.js'

const REGISTRY = Object.freeze({
  'alert-triangle': AlertTriangle,
  'arrow-left': ArrowLeft,
  'arrow-up': ArrowUp,
  ban: Ban,
  bot: Bot,
  check: Check,
  'check-circle-2': CheckCircle2,
  'chevron-down': ChevronDown,
  'chevron-right': ChevronRight,
  'chevron-up': ChevronUp,
  'circle-alert': CircleAlert,
  clapperboard: Clapperboard,
  clock: Clock,
  copy: Copy,
  download: Download,
  'file-text': FileText,
  'help-circle': HelpCircle,
  inbox: Inbox,
  library: Library,
  'loader-2': Loader2,
  menu: Menu,
  'message-square': MessageSquare,
  palette: Palette,
  pause: Pause,
  plus: Plus,
  'refresh-cw': RefreshCw,
  'rotate-cw': RotateCw,
  search: Search,
  settings: Settings,
  sparkles: Sparkles,
  'trash-2': Trash2,
  upload: Upload,
  'wifi-off': WifiOff,
  x: X,
  'x-circle': XCircle,
})

const props = defineProps({
  /** Lucide key or legacy emoji (normalized via resolveIconKey) */
  name: { type: String, default: 'alert-triangle' },
  size: { type: String, default: 'md' }, // sm | md | lg | xl
  strokeWidth: { type: [Number, String], default: 1.75 },
  spin: { type: Boolean, default: false },
  ariaLabel: { type: String, default: '' },
  ariaHidden: { type: Boolean, default: true },
})

const pixelSize = computed(() => {
  const map = { sm: 14, md: 18, lg: 22, xl: 28 }
  return map[props.size] || 18
})

const iconComponent = computed(() => {
  const key = resolveIconKey(props.name)
  return REGISTRY[key] || AlertTriangle
})
</script>

<style scoped>
.vl-icon {
  display: inline-block;
  flex-shrink: 0;
  vertical-align: middle;
  color: currentColor;
}

.vl-icon.spin {
  animation: vl-spin 0.9s linear infinite;
}

@media (prefers-reduced-motion: reduce) {
  .vl-icon.spin {
    animation: none;
  }
}
</style>
