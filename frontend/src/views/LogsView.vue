<template>
  <div class="page">
    <PageHeader :title="$t('logs.title')" :subtitle="$t('logs.subtitle')">
      <button class="btn-tonal" @click="loadLogs">
        <v-icon size="15">mdi-refresh</v-icon>
        {{ $t("common.refresh") }}
      </button>
    </PageHeader>

    <!-- Filter bar -->
    <div class="filter-bar">
      <div class="filter-col">
        <label class="field-label">{{ $t("logs.model") }}</label>
        <input
          v-model="filters.model"
          class="field-input"
          :placeholder="$t('logs.modelFilter')"
        />
      </div>
      <div class="filter-col">
        <label class="field-label">{{ $t("logs.provider") }}</label>
        <input
          v-model="filters.provider"
          class="field-input"
          :placeholder="$t('logs.providerFilter')"
        />
      </div>
      <div class="filter-col">
        <label class="field-label">{{ $t("logs.from") }}</label>
        <input
          v-model="filters.from"
          type="date"
          class="field-input field-input--date"
        />
      </div>
      <div class="filter-col">
        <label class="field-label">{{ $t("logs.to") }}</label>
        <input
          v-model="filters.to"
          type="date"
          class="field-input field-input--date"
        />
      </div>
      <button
        class="btn-primary filter-submit"
        @click="loadLogs"
        :disabled="store.loading"
      >
        <span v-if="!store.loading">{{ $t("common.apply") }}</span>
        <span v-else class="btn-spinner" />
      </button>
    </div>

    <div class="table-card">
      <v-data-table
        :headers="headers"
        :items="store.logs"
        :loading="store.loading"
        density="compact"
        hide-default-footer
        :items-per-page="-1"
        fixed-header
      >
        <template #item.started_at="{ item }">
          <span class="dim-text">{{
            item.started_at ? formatTime(item.started_at) : "-"
          }}</span>
        </template>
        <template #item.completed_at="{ item }">
          <span class="dim-text">{{
            item.completed_at ? formatTime(item.completed_at) : "-"
          }}</span>
        </template>
        <template #item.duration_sec="{ item }">
          <span class="mono-val">
            {{ (item.latency_ms / 1000).toFixed(2) }}s
          </span>
        </template>
        <template #item.provider_name="{ item }">
          <span class="dim-text">{{ item.provider_name || "-" }}</span>
        </template>
        <template #item.model="{ item }">
          <MonoTag>{{ getModelName(item.model) }}</MonoTag>
        </template>
        <template #item.request_tokens="{ item }">
          <span class="mono-val">{{
            item.request_tokens.toLocaleString()
          }}</span>
        </template>
        <template #item.response_tokens="{ item }">
          <span class="mono-val">{{
            item.response_tokens.toLocaleString()
          }}</span>
        </template>
        <template #item.cache_write_5m_tokens="{ item }">
          <span class="mono-val">{{
            item.cache_write_5m_tokens.toLocaleString()
          }}</span>
        </template>
        <template #item.cache_write_1h_tokens="{ item }">
          <span class="mono-val">{{
            item.cache_write_1h_tokens.toLocaleString()
          }}</span>
        </template>
        <template #item.cache_read_tokens="{ item }">
          <span class="mono-val">{{
            item.cache_read_tokens.toLocaleString()
          }}</span>
        </template>
        <template #item.total_tokens="{ item }">
          <span class="mono-val mono-val--accent">{{
            item.total_tokens.toLocaleString()
          }}</span>
        </template>
        <template #item.cost="{ item }">
          <span class="cost-val">${{ item.cost.toFixed(6) }}</span>
        </template>
        <template #item.is_error="{ item }">
          <StatusChip :variant="item.is_error ? 'off' : 'on'">
            {{ item.is_error ? $t("logs.error") : $t("logs.ok") }}
          </StatusChip>
        </template>
        <template #no-data>
          <EmptyState icon="mdi-text-box-search-outline" :text="$t('logs.noRecords')" />
        </template>
      </v-data-table>

      <!-- Mobile cards -->
      <div class="mobile-cards">
        <MobileDataCard
          v-for="log in store.logs"
          :key="log.id"
          :items="[
            {
              label: $t('logs.startedAt'),
              value: log.started_at ? formatTime(log.started_at) : '-',
            },
            {
              label: $t('logs.completedAt'),
              value: log.completed_at ? formatTime(log.completed_at) : '-',
            },
            {
              label: $t('logs.duration'),
              value: (log.latency_ms / 1000).toFixed(2) + 's',
            },
            { label: $t('logs.provider'), value: log.provider_name || '-' },
            { label: $t('logs.model'), value: getModelName(log.model) },
            {
              label: $t('logs.totalTokens'),
              value: log.total_tokens.toLocaleString(),
            },
            { label: $t('logs.totalCost'), value: '$' + log.cost.toFixed(6) },
            {
              label: $t('logs.status'),
              value: log.is_error ? $t('logs.error') : $t('logs.ok'),
            },
          ]"
        />
        <EmptyState v-if="!store.logs.length" icon="mdi-text-box-search-outline" :text="$t('logs.noRecords')" />
      </div>

      <div class="table-footer">
        <span class="mono-val">{{ store.logs.length }}</span>
        <span class="dim-text"> {{ $t("logs.of") }} </span>
        <span class="mono-val">{{ store.total }}</span>
        <span class="dim-text"> {{ $t("logs.records") }}</span>
        <div class="pagination-btns">
          <button
            class="row-btn pagination-btn"
            :disabled="offset <= 0"
            @click="prevPage"
          >
            <v-icon size="16">mdi-chevron-left</v-icon>
          </button>
          <button
            class="row-btn pagination-btn"
            :disabled="offset + limit >= store.total"
            @click="nextPage"
          >
            <v-icon size="16">mdi-chevron-right</v-icon>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from "vue";
import { useI18n } from "vue-i18n";
import { useUsageStore } from "../stores/usage";
import { useMobile } from "../composables/useMobile";
import PageHeader from "../components/PageHeader.vue";
import MonoTag from "../components/MonoTag.vue";
import StatusChip from "../components/StatusChip.vue";
import EmptyState from "../components/EmptyState.vue";
import MobileDataCard from "../components/MobileDataCard.vue";

const { t } = useI18n();
const store = useUsageStore();
const { isMobile } = useMobile();

const limit = 50;
const offset = ref(0);

const filters = ref({ model: "", provider: "", from: "", to: "" });

const headers = computed(() => [
  { title: t("logs.startedAt"), key: "started_at", minWidth: "140" },
  { title: t("logs.completedAt"), key: "completed_at", minWidth: "140" },
  { title: t("logs.duration"), key: "duration_sec", minWidth: "90" },
  { title: t("logs.provider"), key: "provider_name", minWidth: "100" },
  { title: t("logs.model"), key: "model", minWidth: "140" },
  { title: t("logs.inputTokens"), key: "request_tokens", minWidth: "90" },
  { title: t("logs.outputTokens"), key: "response_tokens", minWidth: "90" },
  {
    title: t("logs.cacheWrite5m"),
    key: "cache_write_5m_tokens",
    minWidth: "90",
  },
  {
    title: t("logs.cacheWrite1h"),
    key: "cache_write_1h_tokens",
    minWidth: "90",
  },
  { title: t("logs.cacheRead"), key: "cache_read_tokens", minWidth: "90" },
  { title: t("logs.totalTokens"), key: "total_tokens", minWidth: "90" },
  { title: t("logs.totalCost"), key: "cost", minWidth: "100" },
  { title: t("logs.status"), key: "is_error", minWidth: "80" },
]);

function formatTime(ts: string): string {
  return new Date(ts).toLocaleString();
}

function getModelName(model: string): string {
  const idx = model.indexOf("/");
  return idx >= 0 ? model.substring(idx + 1) : model;
}

async function loadLogs() {
  const params: Record<string, any> = { limit, offset: offset.value };
  if (filters.value.model) params.model = filters.value.model;
  if (filters.value.from) params.from = filters.value.from;
  if (filters.value.to) params.to = filters.value.to;
  await store.fetchLogs(params);
}

function nextPage() {
  offset.value += limit;
  loadLogs();
}

function prevPage() {
  offset.value = Math.max(0, offset.value - limit);
  loadLogs();
}

onMounted(() => {
  loadLogs();
  store.connect();
});

onUnmounted(() => {
  store.disconnect();
});
</script>

<style scoped>
@import "../styles/page-shared.css";

.dim-text {
  font-family: "DM Sans", sans-serif;
  font-size: 0.78rem;
  color: #7c7a75;
}
.mono-val {
  font-family: "JetBrains Mono", monospace;
  font-size: 0.78rem;
  color: #e8e6e1;
}
.mono-val--accent {
  color: #e8a020;
}
.mono-val--slow {
  color: #ff5757;
}

.cost-val {
  font-family: "JetBrains Mono", monospace;
  font-size: 0.75rem;
  color: #2ec4b6;
}

.filter-submit {
  align-self: flex-end;
}

.field-input--date {
  color-scheme: dark;
}

.table-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
  padding: 10px 16px;
  border-top: 1px solid rgba(255, 255, 255, 0.05);
  font-family: "DM Sans", sans-serif;
  font-size: 0.8rem;
  color: #4a4844;
}

.pagination-btns {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-left: 12px;
}

.pagination-btn {
  width: 26px;
  height: 26px;
}

.pagination-btn:disabled {
  opacity: 0.3;
  cursor: not-allowed;
}

.mobile-cards {
  display: none;
}

@media (max-width: 768px) {
  .v-data-table {
    display: none;
  }
  .mobile-cards {
    display: block;
  }
  .table-footer {
    justify-content: center;
  }
}
</style>
