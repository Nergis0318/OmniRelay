<template>
  <v-dialog v-model="open" :max-width="maxWidth" :fullscreen="fullscreen">
    <div class="dialog-card">
      <div class="dialog-header">
        <h2 class="dialog-title" :style="titleColor ? { color: titleColor } : undefined">
          {{ title }}
        </h2>
        <button class="dialog-close" @click="close">
          <v-icon size="18">mdi-close</v-icon>
        </button>
      </div>

      <div class="dialog-body">
        <slot />
        <div v-if="error" class="alert alert--error">
          <v-icon size="14">mdi-alert-circle-outline</v-icon>
          {{ error }}
        </div>
      </div>

      <div v-if="!closeOnly" class="dialog-footer">
        <button class="btn-ghost" @click="close">
          {{ cancelText }}
        </button>
        <button class="btn-primary" :class="{ 'btn-danger': danger }" @click="emit('save')" :disabled="saving">
          <span v-if="!saving">{{ saveText }}</span>
          <span v-else class="btn-spinner" />
        </button>
      </div>
      <div v-else class="dialog-footer">
        <button class="btn-primary" @click="close">
          {{ saveText }}
        </button>
      </div>
    </div>
  </v-dialog>
</template>

<script setup lang="ts">
import { computed } from "vue";

const props = defineProps<{
  modelValue: boolean;
  title: string;
  titleColor?: string;
  maxWidth?: number;
  fullscreen?: boolean;
  error?: string | null;
  saving?: boolean;
  saveText: string;
  cancelText: string;
  danger?: boolean;
  closeOnly?: boolean;
}>();

const emit = defineEmits<{
  (e: "update:modelValue", v: boolean): void;
  (e: "save"): void;
}>();

const open = computed({
  get: () => props.modelValue,
  set: (v: boolean) => emit("update:modelValue", v),
});

function close() {
  emit("update:modelValue", false);
}
</script>

<style scoped>
@import "../styles/page-shared.css";
</style>
