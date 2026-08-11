<template>
  <div class="stat-card">
    <p class="stat-label">
      {{ label }}
      <span v-if="hint" class="stat-hint">{{ hint }}</span>
    </p>
    <p class="stat-value" :class="valueClass">
      <slot />
    </p>
    <p v-if="$slots.sub || sub" class="stat-sub">
      <slot name="sub">{{ sub }}</slot>
    </p>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  label: string;
  hint?: string;
  sub?: string;
  valueClass?: string;
}>();
</script>
