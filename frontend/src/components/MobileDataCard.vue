<template>
  <div class="mobile-card">
    <div v-for="(item, idx) in items" :key="idx" class="mobile-card__row">
      <span class="mobile-card__label">{{ item.label }}</span>
      <span class="mobile-card__value">{{ item.value }}</span>
    </div>
    <div v-if="$slots.actions" class="mobile-card__actions">
      <slot name="actions" />
    </div>
  </div>
</template>

<script setup lang="ts">
interface CardItem {
  label: string;
  value: string;
}

defineProps<{
  items: CardItem[];
}>();
</script>

<style scoped>
.mobile-card {
  background: var(--color-surface, #131316);
  border: 1px solid var(--clr-border, rgba(255, 255, 255, 0.06));
  border-radius: var(--radius-card, 12px);
  padding: 16px;
  margin-bottom: 12px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}
.mobile-card__row {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  padding: 8px 0;
  border-bottom: 1px solid rgba(255, 255, 255, 0.04);
}
.mobile-card__row:last-child {
  border-bottom: none;
}
.mobile-card__label {
  font-family: "DM Sans", sans-serif;
  font-size: 0.75rem;
  font-weight: 500;
  color: #7c7a75;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  flex-shrink: 0;
}
.mobile-card__value {
  font-family: "JetBrains Mono", monospace;
  font-size: 0.82rem;
  color: #e8e6e1;
  text-align: right;
  word-break: break-word;
}
.mobile-card__actions {
  display: flex;
  justify-content: flex-end;
  gap: 6px;
  padding-top: 8px;
  margin-top: 4px;
  border-top: 1px solid rgba(255, 255, 255, 0.04);
}
</style>
