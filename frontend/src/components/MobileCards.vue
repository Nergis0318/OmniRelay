<template>
  <div class="mobile-cards">
    <MobileDataCard
      v-for="item in items"
      :key="String(item[keyField])"
      :items="fields(item)"
    >
      <template #actions>
        <slot name="actions" :item="item" />
      </template>
    </MobileDataCard>
    <div v-if="!items.length" class="empty-state">
      <v-icon size="32" color="#4a4844">{{ emptyIcon }}</v-icon>
      <p>{{ emptyText }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import MobileDataCard from "./MobileDataCard.vue";

defineProps<{
  items: any[];
  keyField: string;
  fields: (item: any) => Array<{ label: string; value: string }>;
  emptyIcon: string;
  emptyText: string;
}>();
</script>

<style scoped>
@import "../styles/page-shared.css";
</style>
