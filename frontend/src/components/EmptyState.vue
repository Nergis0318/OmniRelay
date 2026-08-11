<template>
  <div class="empty-state" :class="{ small }" role="status" aria-live="polite">
    <v-icon :size="iconSize" color="#4a4844" aria-hidden="true">{{ icon }}</v-icon>
    <p>{{ text }}</p>
    <slot />
  </div>
</template>

<script setup lang="ts">
withDefaults(
  defineProps<{
    icon: string;
    text: string;
    iconSize?: number | string;
    small?: boolean;
  }>(),
  { iconSize: 32, small: false },
);
</script>
