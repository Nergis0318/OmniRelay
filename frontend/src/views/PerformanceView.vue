<template>
  <div class="page">
    <PageHeader :title="$t('performance.title')" :subtitle="$t('performance.subtitle')">
      <button class="btn-tonal" :disabled="store.loading" @click="load">
        <v-icon size="15">mdi-refresh</v-icon>
        {{ $t("common.refresh") }}
      </button>
    </PageHeader>

    <!-- Filter bar -->
    <div class="filter-bar">
      <div class="filter-col">
        <label class="field-label">{{ $t("performance.provider") }}</label>
        <select v-model="filters.provider_id" class="field-select">
          <option value="">{{ $t("performance.allProviders") }}</option>
          <option v-for="p in providersStore.providers" :key="p.id" :value="p.id">
            {{ p.name }}
          </option>
        </select>
      </div>
      <div class="filter-col">
        <label class="field-label">{{ $t("performance.from") }}</label>
        <input v-model="filters.from" type="date" class="field-input field-input--date" />
      </div>
      <div class="filter-col">
        <label class="field-label">{{ $t("performance.to") }}</label>
        <input v-model="filters.to" type="date" class="field-input field-input--date" />
      </div>
      <div class="filter-col filter-col--narrow">
        <label class="field-label">{{ $t("performance.granularity") }}</label>
        <select v-model="filters.granularity" class="field-select">
          <option value="">{{ $t("performance.auto") }}</option>
          <option value="minute">Minute</option>
          <option value="hour">Hour</option>
          <option value="day">Day</option>
        </select>
      </div>
      <div class="filter-col filter-col--narrow">
        <label class="field-label">&nbsp;</label>
        <div class="preset-row">
          <button
            v-for="p in presets"
            :key="p.key"
            class="preset-chip"
            :class="{ 'preset-chip--active': preset === p.key }"
            @click="applyPreset(p.key)"
          >
            {{ p.label }}
          </button>
        </div>
      </div>
      <button class="btn-primary filter-submit" :disabled="store.loading" @click="load">
        <span v-if="!store.loading">{{ $t("common.apply") }}</span>
        <span v-else class="btn-spinner" />
      </button>
    </div>

    <p v-if="store.error" class="alert alert--error alert--page">{{ store.error }}</p>

    <template v-if="store.data">
      <!-- Summary cards -->
      <div class="stats-grid">
        <StatCard :label="$t('performance.rpm')" :sub="`${summary.total_requests.toLocaleString()} req`">
          {{ fmtNum(summary.rpm) }}
        </StatCard>
        <StatCard :label="$t('performance.tpm')" value-class="stat-value--accent" sub="&nbsp;">
          {{ fmtNum(summary.tpm) }}
        </StatCard>
        <StatCard :label="$t('performance.avgLatency')">
          {{ fmtMs(summary.avg_latency_ms) }}
          <template #sub>{{ $t("performance.p50") }} {{ fmtMs(summary.p50_ms) }}</template>
        </StatCard>
        <StatCard :label="$t('performance.ttft')" :hint="$t('performance.ttftHint')" :value-class="summary.avg_ttft_ms === null ? 'stat-value--dim' : undefined">
          {{ summary.avg_ttft_ms === null ? "-" : fmtMs(summary.avg_ttft_ms) }}
          <template #sub>
            {{
              summary.avg_ttft_ms === null
                ? $t("performance.noStreaming")
                : summary.ttft_count.toLocaleString() + " streams"
            }}
          </template>
        </StatCard>
        <StatCard :label="`${$t('performance.p95')} / ${$t('performance.p99')}`" :sub="`${$t('performance.p99')} ${fmtMs(summary.p99_ms)}`">
          {{ fmtMs(summary.p95_ms) }}
        </StatCard>
        <StatCard :label="$t('performance.errorRate')" :value-class="summary.error_rate > 0.05 ? 'stat-value--error' : undefined" sub="&nbsp;">
          {{ (summary.error_rate * 100).toFixed(2) }}%
        </StatCard>
        <StatCard :label="$t('performance.cacheHitRate')" value-class="stat-value--cache" sub="&nbsp;">
          {{ (summary.cache_hit_rate * 100).toFixed(1) }}%
        </StatCard>
      </div>

      <!-- Timeseries charts -->
      <div class="charts-row">
        <div class="table-card chart-section">
          <h2 class="chart-heading">{{ $t("performance.throughput") }}</h2>
          <div class="chart-area">
            <Line v-if="store.data.timeseries.length" :data="throughputChart" :options="throughputOptions" />
            <EmptyState v-else icon="mdi-chart-line-variant" :text="$t('performance.noData')" />
          </div>
        </div>
        <div class="table-card chart-section">
          <h2 class="chart-heading">{{ $t("performance.latency") }}</h2>
          <div class="chart-area">
            <Line v-if="store.data.timeseries.length" :data="latencyChart" :options="latencyOptions" />
            <EmptyState v-else icon="mdi-chart-line-variant" :text="$t('performance.noData')" />
          </div>
        </div>
      </div>

      <!-- Breakdowns -->
      <div class="breakdown-row">
        <div class="table-card breakdown-card">
          <h2 class="chart-heading">{{ $t("performance.byProvider") }}</h2>
          <div class="table-scroll">
            <table v-if="store.data.by_provider.length" class="perf-table">
              <thead>
                <tr>
                  <th>{{ $t("performance.provider") }}</th>
                  <th class="num">{{ $t("performance.requests") }}</th>
                  <th class="num">{{ $t("performance.tokens") }}</th>
                  <th class="num">DAVG</th>
                  <th class="num">{{ $t("performance.cost") }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="r in store.data.by_provider" :key="r.provider_id ?? 'none'">
                  <td>{{ r.provider_name || "-" }}</td>
                  <td class="num mono-val">{{ r.requests.toLocaleString() }}</td>
                  <td class="num mono-val">{{ r.tokens.toLocaleString() }}</td>
                  <td class="num mono-val">{{ fmtMs(r.avg_latency_ms) }}</td>
                  <td class="num cost-val">${{ r.cost.toFixed(4) }}</td>
                </tr>
              </tbody>
            </table>
            <EmptyState v-else icon="mdi-chart-line-variant" :text="$t('performance.noData')" small />
          </div>
        </div>

        <div class="table-card breakdown-card">
          <h2 class="chart-heading">{{ $t("performance.byModel") }}</h2>
          <div class="table-scroll">
            <table v-if="store.data.by_model.length" class="perf-table">
              <thead>
                <tr>
                  <th>{{ $t("performance.model") }}</th>
                  <th class="num">{{ $t("performance.requests") }}</th>
                  <th class="num">{{ $t("performance.tokens") }}</th>
                  <th class="num">DAVG</th>
                  <th class="num">{{ $t("performance.cost") }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="r in store.data.by_model" :key="r.model">
                  <td><MonoTag>{{ r.model }}</MonoTag></td>
                  <td class="num mono-val">{{ r.requests.toLocaleString() }}</td>
                  <td class="num mono-val">{{ r.tokens.toLocaleString() }}</td>
                  <td class="num mono-val">{{ fmtMs(r.avg_latency_ms) }}</td>
                  <td class="num cost-val">${{ r.cost.toFixed(4) }}</td>
                </tr>
              </tbody>
            </table>
            <EmptyState v-else icon="mdi-chart-line-variant" :text="$t('performance.noData')" small />
          </div>
        </div>
      </div>

      <div class="table-card breakdown-card">
        <h2 class="chart-heading">{{ $t("performance.topModels") }}</h2>
        <div class="table-scroll">
          <table v-if="store.data.top_models_by_cost.length" class="perf-table">
            <thead>
              <tr>
                <th>{{ $t("performance.model") }}</th>
                <th class="num">{{ $t("performance.requests") }}</th>
                <th class="num">{{ $t("performance.tokens") }}</th>
                <th class="num">{{ $t("performance.cost") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in store.data.top_models_by_cost" :key="r.model">
                <td><MonoTag>{{ r.model }}</MonoTag></td>
                <td class="num mono-val">{{ r.requests.toLocaleString() }}</td>
                <td class="num mono-val">{{ r.tokens.toLocaleString() }}</td>
                <td class="num cost-val">${{ r.cost.toFixed(4) }}</td>
              </tr>
            </tbody>
          </table>
          <EmptyState v-else icon="mdi-chart-line-variant" :text="$t('performance.noData')" small />
        </div>
      </div>
    </template>

    <EmptyState v-else-if="!store.loading && !store.error" icon="mdi-chart-timeline-variant" :text="$t('performance.noData')" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { Line } from "vue-chartjs";
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Filler,
  Tooltip,
  Legend,
} from "chart.js";
import { usePerformanceStore } from "../stores/performance";
import { useProvidersStore } from "../stores/providers";
import PageHeader from "../components/PageHeader.vue";
import StatCard from "../components/StatCard.vue";
import MonoTag from "../components/MonoTag.vue";
import EmptyState from "../components/EmptyState.vue";

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Filler,
  Tooltip,
  Legend,
);

const { t } = useI18n();
const store = usePerformanceStore();
const providersStore = useProvidersStore();

const filters = reactive({
  provider_id: "" as string | number,
  from: "",
  to: "",
  granularity: "",
});

const preset = ref("");
let applyingPreset = false;
const presets = [
  { key: "1h", label: "1H" },
  { key: "24h", label: "24H" },
  { key: "7d", label: "7D" },
  { key: "30d", label: "30D" },
];

const summary = computed(
  () =>
    store.data?.summary ?? {
      total_requests: 0,
      rpm: 0,
      tpm: 0,
      avg_latency_ms: 0,
      p50_ms: 0,
      p95_ms: 0,
      p99_ms: 0,
      avg_ttft_ms: null,
      ttft_count: 0,
      error_rate: 0,
      cache_hit_rate: 0,
    },
);

function fmtNum(v: number): string {
  if (v >= 1000000) return (v / 1000000).toFixed(2) + "M";
  if (v >= 1000) return (v / 1000).toFixed(1) + "k";
  return v.toFixed(v < 10 && v % 1 !== 0 ? 1 : 0);
}

function fmtMs(v: number): string {
  if (v >= 60000) return (v / 60000).toFixed(1) + "m";
  if (v >= 1000) return (v / 1000).toFixed(2) + "s";
  return Math.round(v) + "ms";
}

function fmtBucket(b: string): string {
  if (b.length === 10) return b.slice(5);
  return b.slice(5, 16);
}

function applyPreset(key: string) {
  applyingPreset = true;
  preset.value = key;
  const now = new Date();
  const ms: Record<string, number> = {
    "1h": 60 * 60 * 1000,
    "24h": 24 * 60 * 60 * 1000,
    "7d": 7 * 24 * 60 * 60 * 1000,
    "30d": 30 * 24 * 60 * 60 * 1000,
  };
  const fromDate = new Date(now.getTime() - ms[key]);
  const iso = (d: Date) => d.toISOString().slice(0, 10);
  filters.from = iso(fromDate);
  filters.to = iso(now);
  filters.granularity = key === "1h" ? "minute" : key === "30d" ? "day" : "hour";
  load();
}

function load() {
  const params: Record<string, any> = {};
  if (filters.provider_id !== "") params.provider_id = filters.provider_id;
  if (filters.from) params.from = filters.from;
  if (filters.to) params.to = filters.to + " 23:59:59";
  if (filters.granularity) params.granularity = filters.granularity;
  store.fetchPerformance(params);
}

// Manual filter edits clear the preset highlight
watch(filters, () => {
  if (applyingPreset) {
    applyingPreset = false;
    return;
  }
  preset.value = "";
});

const bucketLabels = computed(() =>
  (store.data?.timeseries ?? []).map((b) => fmtBucket(b.bucket)),
);

const throughputChart = computed(() => ({
  labels: bucketLabels.value,
  datasets: [
    {
      label: t("performance.rpm"),
      data: store.data?.timeseries.map((b) => b.rpm) ?? [],
      borderColor: "#2ec4b6",
      backgroundColor: "rgba(46, 196, 182, 0.06)",
      fill: true,
      tension: 0.35,
      pointRadius: 2,
      pointHoverRadius: 4,
      borderWidth: 1.5,
      yAxisID: "y",
    },
    {
      label: t("performance.tpm"),
      data: store.data?.timeseries.map((b) => b.tpm) ?? [],
      borderColor: "#7b61ff",
      backgroundColor: "rgba(123, 97, 255, 0.04)",
      fill: true,
      tension: 0.35,
      pointRadius: 2,
      pointHoverRadius: 4,
      borderWidth: 1.5,
      yAxisID: "y1",
    },
  ],
}));

const latencyChart = computed(() => ({
  labels: bucketLabels.value,
  datasets: [
    {
      label: "DAVG",
      data: store.data?.timeseries.map((b) => b.avg_latency_ms) ?? [],
      borderColor: "#e8a020",
      backgroundColor: "rgba(232, 160, 32, 0.06)",
      fill: true,
      tension: 0.35,
      pointRadius: 2,
      pointHoverRadius: 4,
      borderWidth: 1.5,
    },
    {
      label: t("performance.ttft"),
      data: store.data?.timeseries.map((b) => b.avg_ttft_ms) ?? [],
      borderColor: "#2ec4b6",
      backgroundColor: "transparent",
      borderDash: [5, 4],
      fill: false,
      tension: 0.35,
      pointRadius: 2,
      pointHoverRadius: 4,
      borderWidth: 1.5,
      spanGaps: true,
    },
  ],
}));

const baseChartOptions = {
  responsive: true,
  maintainAspectRatio: false,
  interaction: { intersect: false, mode: "index" as const },
  plugins: {
    legend: {
      labels: {
        color: "#7c7a75",
        font: { family: '"DM Sans", sans-serif', size: 11 },
        usePointStyle: true,
        pointStyleWidth: 8,
        boxHeight: 6,
      },
    },
    tooltip: {
      backgroundColor: "#1a1a1f",
      borderColor: "rgba(232,160,32,0.25)",
      borderWidth: 1,
      titleColor: "#e8e6e1",
      bodyColor: "#7c7a75",
      titleFont: { family: "DM Sans", size: 12 },
      bodyFont: { family: "JetBrains Mono", size: 11 },
      padding: 10,
    },
  },
  scales: {
    x: {
      grid: { color: "rgba(255,255,255,0.04)" },
      ticks: { color: "#4a4844", font: { family: "JetBrains Mono", size: 10 }, maxRotation: 0, autoSkip: true },
    },
  },
};

const throughputOptions = {
  ...baseChartOptions,
  scales: {
    ...baseChartOptions.scales,
    y: {
      position: "left" as const,
      grid: { color: "rgba(255,255,255,0.04)" },
      ticks: { color: "#2ec4b6", font: { family: "JetBrains Mono", size: 10 } },
    },
    y1: {
      position: "right" as const,
      grid: { drawOnChartArea: false },
      ticks: { color: "#7b61ff", font: { family: "JetBrains Mono", size: 10 } },
    },
  },
};

const latencyOptions = {
  ...baseChartOptions,
  scales: {
    ...baseChartOptions.scales,
    y: {
      grid: { color: "rgba(255,255,255,0.04)" },
      ticks: {
        color: "#4a4844",
        font: { family: "JetBrains Mono", size: 10 },
        callback: (v: number | string) => fmtMs(Number(v)),
      },
    },
  },
};

onMounted(() => {
  providersStore.fetch();
  load();
});
</script>

<style scoped>
@import "../styles/page-shared.css";

/* ── Stats grid ── */
.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
}
@media (max-width: 1100px) {
  .stats-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}
@media (max-width: 560px) {
  .stats-grid {
    grid-template-columns: 1fr;
  }
}

.stat-value--accent { color: #7b61ff; }
.stat-value--cache { color: #2ec4b6; }
.stat-value--error { color: #ff5757; }
.stat-value--dim { color: #4a4844; }

/* ── Presets ── */
.filter-col--narrow {
  flex: 0 1 auto;
  min-width: 0;
}
.preset-row {
  display: flex;
  gap: 4px;
}
.preset-chip {
  padding: 9px 10px;
  border-radius: 8px;
  border: 1px solid rgba(255, 255, 255, 0.08);
  background: transparent;
  color: #7c7a75;
  font-family: "JetBrains Mono", monospace;
  font-size: 0.72rem;
  cursor: pointer;
  transition: all 0.15s;
}
.preset-chip:hover {
  border-color: rgba(232, 160, 32, 0.3);
  color: #e8a020;
}
.preset-chip--active {
  background: rgba(232, 160, 32, 0.12);
  border-color: rgba(232, 160, 32, 0.35);
  color: #e8a020;
}

/* ── Charts ── */
.charts-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
@media (max-width: 900px) {
  .charts-row {
    grid-template-columns: 1fr;
  }
}
.chart-section {
  padding: 18px 20px;
}
.chart-heading {
  font-family: "Fraunces", Georgia, serif;
  font-size: 0.95rem;
  font-weight: 600;
  color: #e8e6e1;
  margin: 0 0 14px;
}
.chart-area {
  height: 240px;
  position: relative;
}

/* ── Breakdown tables ── */
.breakdown-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
@media (max-width: 900px) {
  .breakdown-row {
    grid-template-columns: 1fr;
  }
}
.breakdown-card {
  padding: 18px 20px;
}
.table-scroll {
  overflow-x: auto;
}
.perf-table {
  width: 100%;
  border-collapse: collapse;
  font-family: "DM Sans", sans-serif;
  font-size: 0.82rem;
}
.perf-table th {
  text-align: left;
  color: #4a4844;
  font-weight: 500;
  font-size: 0.68rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  padding: 6px 10px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  white-space: nowrap;
}
.perf-table td {
  padding: 8px 10px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.04);
  color: #e8e6e1;
  white-space: nowrap;
}
.perf-table tr:last-child td {
  border-bottom: none;
}
.perf-table .num {
  text-align: right;
}
.mono-val {
  font-family: "JetBrains Mono", monospace;
  font-size: 0.78rem;
}
.cost-val {
  font-family: "JetBrains Mono", monospace;
  font-size: 0.78rem;
  color: #2ec4b6;
}

.field-select {
  appearance: none;
  background-image: url("data:image/svg+xml;charset=utf-8,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='6' viewBox='0 0 10 6'%3E%3Cpath d='M1 1l4 4 4-4' stroke='%237c7a75' stroke-width='1.4' fill='none' stroke-linecap='round'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 12px center;
  padding-right: 30px;
}
</style>
