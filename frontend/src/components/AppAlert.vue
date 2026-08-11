<template>
  <div class="alert" :class="[`alert--${variant}`, { 'alert--page': page }]">
    <v-icon size="14">{{ icon }}</v-icon>
    <slot />
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";

const props = withDefaults(
  defineProps<{
    variant?: "success" | "error" | "warning";
    page?: boolean;
  }>(),
  { variant: "error", page: false },
);

const icon = computed(() =>
  props.variant === "success" ? "mdi-check-circle-outline" : "mdi-alert-circle-outline",
);
</script>
