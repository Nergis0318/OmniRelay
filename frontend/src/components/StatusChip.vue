<template>
  <span class="status-chip" :class="`status-chip--${variant}`">
    <slot />
  </span>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ variant?: "on" | "off" | "warning" }>(), {
  variant: "on",
});
</script>
