<template>
  <div class="immersive-canvas" aria-hidden="true">
    <div class="orb orb-a"></div>
    <div class="orb orb-b"></div>
    <div class="orb orb-c"></div>
    <div class="grid-veil"></div>
    <div class="scan-line"></div>
    <div class="vignette"></div>
    <div class="grain"></div>
  </div>
</template>

<script setup>
// Pure CSS atmosphere layer — no JS loops; respects reduced-motion via CSS.
</script>

<style scoped>
.immersive-canvas {
  position: fixed;
  inset: 0;
  z-index: 0;
  pointer-events: none;
  overflow: hidden;
  isolation: isolate;
}

.orb {
  position: absolute;
  border-radius: 50%;
  filter: blur(80px);
  opacity: var(--vl-orb-opacity, 0.45);
  will-change: transform;
}

.orb-a {
  width: min(52vw, 640px);
  height: min(52vw, 640px);
  top: -12%;
  left: -8%;
  background: radial-gradient(circle at 40% 40%, var(--vl-primary-glow), transparent 68%);
  animation: orb-drift-a 22s ease-in-out infinite alternate;
}

.orb-b {
  width: min(42vw, 520px);
  height: min(42vw, 520px);
  bottom: -10%;
  right: -6%;
  background: radial-gradient(circle at 50% 50%, var(--vl-info-dim), transparent 70%);
  animation: orb-drift-b 28s ease-in-out infinite alternate;
}

.orb-c {
  width: min(28vw, 360px);
  height: min(28vw, 360px);
  top: 42%;
  left: 48%;
  background: radial-gradient(circle at 50% 50%, var(--vl-accent-glow), transparent 72%);
  opacity: calc(var(--vl-orb-opacity, 0.45) * 0.65);
  animation: orb-drift-c 18s ease-in-out infinite alternate;
}

.grid-veil {
  position: absolute;
  inset: -20%;
  background-image:
    linear-gradient(var(--vl-grid-line) 1px, transparent 1px),
    linear-gradient(90deg, var(--vl-grid-line) 1px, transparent 1px);
  background-size: 64px 64px;
  mask-image: radial-gradient(ellipse 75% 65% at 50% 40%, #000 20%, transparent 75%);
  -webkit-mask-image: radial-gradient(ellipse 75% 65% at 50% 40%, #000 20%, transparent 75%);
  opacity: var(--vl-grid-opacity, 0.35);
  transform: perspective(900px) rotateX(58deg) translateY(-8%);
  transform-origin: center top;
}

.scan-line {
  position: absolute;
  left: 0;
  right: 0;
  height: 28%;
  top: -30%;
  background: linear-gradient(
    180deg,
    transparent,
    color-mix(in srgb, var(--vl-primary) 6%, transparent),
    transparent
  );
  animation: scan-sweep 14s linear infinite;
  opacity: var(--vl-scan-opacity, 0.4);
}

.vignette {
  position: absolute;
  inset: 0;
  background: radial-gradient(
    ellipse 80% 70% at 50% 45%,
    transparent 40%,
    var(--vl-vignette) 100%
  );
}

.grain {
  position: absolute;
  inset: 0;
  opacity: var(--vl-grain-opacity, 0.045);
  background-image: url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.85' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)' opacity='0.55'/%3E%3C/svg%3E");
  background-size: 180px 180px;
  mix-blend-mode: soft-light;
}

@keyframes orb-drift-a {
  from { transform: translate3d(0, 0, 0) scale(1); }
  to { transform: translate3d(6%, 8%, 0) scale(1.08); }
}

@keyframes orb-drift-b {
  from { transform: translate3d(0, 0, 0) scale(1); }
  to { transform: translate3d(-8%, -5%, 0) scale(1.12); }
}

@keyframes orb-drift-c {
  from { transform: translate3d(-4%, 2%, 0) scale(0.95); }
  to { transform: translate3d(5%, -6%, 0) scale(1.1); }
}

@keyframes scan-sweep {
  0% { transform: translateY(0); }
  100% { transform: translateY(420%); }
}

@media (prefers-reduced-motion: reduce) {
  .orb,
  .scan-line {
    animation: none !important;
  }
}
</style>
