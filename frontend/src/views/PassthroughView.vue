<template>
  <div class="page">
    <PageHeader :title="$t('passthrough.title')" :subtitle="$t('passthrough.subtitle')">
      <div class="head-actions">
        <button
          class="preset-chip live-chip"
          :class="{ 'preset-chip--active': live }"
          :aria-pressed="live ? 'true' : 'false'"
          @click="toggleLive"
        >
          <span class="live-dot" :class="{ 'live-dot--on': live }" aria-hidden="true" />
          {{ $t("passthrough.live") }}
        </button>
        <button class="btn-tonal" :disabled="store.loading" @click="load">
          <v-icon size="15">mdi-refresh</v-icon>
          {{ $t("common.refresh") }}
        </button>
      </div>
    </PageHeader>

    <!-- Filter bar -->
    <div class="filter-bar">
      <div class="filter-col">
        <label class="field-label" for="pt-host">{{ $t("passthrough.host") }}</label>
        <input
          id="pt-host"
          v-model="filters.host"
          class="field-input"
          list="pt-host-options"
          :placeholder="$t('passthrough.hostPlaceholder')"
          autocomplete="off"
        />
        <datalist id="pt-host-options">
          <option v-for="h in knownHosts" :key="h" :value="h" />
        </datalist>
      </div>
      <div class="filter-col">
        <label class="field-label" for="pt-from">{{ $t("passthrough.from") }}</label>
        <input id="pt-from" v-model="filters.from" type="date" class="field-input field-input--date" />
      </div>
      <div class="filter-col">
        <label class="field-label" for="pt-to">{{ $t("passthrough.to") }}</label>
        <input id="pt-to" v-model="filters.to" type="date" class="field-input field-input--date" />
      </div>
      <div class="filter-col filter-col--narrow">
        <label class="field-label" for="pt-gran">{{ $t("passthrough.granularity") }}</label>
        <select id="pt-gran" v-model="filters.granularity" class="field-select">
          <option value="">{{ $t("passthrough.auto") }}</option>
          <option value="minute">Minute</option>
          <option value="hour">Hour</option>
          <option value="day">Day</option>
        </select>
      </div>
      <div class="filter-col filter-col--narrow">
        <span class="field-label" aria-hidden="true">&nbsp;</span>
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

    <p v-if="store.error" class="alert alert--error alert--page" role="alert">
      {{ store.error }}
      <button class="alert-dismiss" @click="store.clearError()"><v-icon size="14">mdi-close</v-icon></button>
    </p>

    <!-- Nothing measured at all: teach the shape of a passthrough request -->
    <div v-if="isEmpty" class="table-card primer-card">
      <EmptyState icon="mdi-connection" :text="$t('passthrough.emptyTitle')">
        <p class="primer-body">{{ $t("passthrough.emptyBody") }}</p>
        <code class="primer-cmd">{{ exampleCommand }}</code>
        <button class="btn-tonal" @click="copy(exampleCommand)">
          <v-icon size="14">{{ copied ? "mdi-check" : "mdi-content-copy" }}</v-icon>
          {{ copied ? $t("passthrough.copied") : $t("passthrough.copyCommand") }}
        </button>
      </EmptyState>
    </div>

    <!-- Measured, but not under these filters -->
    <div v-else-if="filteredEmpty" class="table-card">
      <EmptyState icon="mdi-filter-remove-outline" :text="$t('passthrough.noData')">
        <button class="btn-tonal" @click="clearFilters">{{ $t("passthrough.clearFilters") }}</button>
      </EmptyState>
    </div>

    <template v-else-if="store.perf">
      <!-- Summary -->
      <div class="stats-grid">
        <StatCard :label="$t('passthrough.requests')" :sub="rateLabel">
          {{ summary.total_requests.toLocaleString() }}
        </StatCard>
        <StatCard
          :label="$t('passthrough.avgTotal')"
          :value-class="slowTotal ? 'stat-value--warn' : undefined"
          :sub="`P95 ${fmtMs(summary.p95_total_ms)} · P99 ${fmtMs(summary.p99_total_ms)}`"
        >
          {{ fmtMs(summary.avg_total_ms) }}
        </StatCard>
        <StatCard
          :label="$t('passthrough.ttfb')"
          :hint="$t('passthrough.ttfbHint')"
          :sub="`${$t('passthrough.prefill')} ${fmtMs(prefillMs)}`"
        >
          <template v-if="summary.avg_ttfb_ms === null">-</template>
          <template v-else>{{ fmtMs(summary.avg_ttfb_ms) }}</template>
        </StatCard>
        <StatCard
          :label="$t('passthrough.ttft')"
          :hint="$t('passthrough.streamOnly')"
          :value-class="summary.avg_ttft_ms === null ? 'stat-value--dim' : undefined"
          :sub="summary.avg_ttft_ms === null ? $t('passthrough.noStreaming') : `+${fmtMs(streamMs)} ${$t('passthrough.streaming')}`"
        >
          {{ summary.avg_ttft_ms === null ? "-" : fmtMs(summary.avg_ttft_ms) }}
        </StatCard>
        <StatCard
          :label="$t('passthrough.setup')"
          :hint="$t('passthrough.setupHint')"
          :sub="`DNS ${fmtMs(summary.avg_dns_ms)} · TCP ${fmtMs(summary.avg_connect_ms)} · TLS ${fmtMs(summary.avg_tls_ms)}`"
        >
          {{ fmtMs(setupMs) }}
        </StatCard>
        <StatCard
          :label="$t('passthrough.errorRate')"
          :value-class="summary.error_rate > 0.05 ? 'stat-value--error' : undefined"
          :sub="`${$t('passthrough.avgResponse')} ${fmtBytes(summary.avg_response_bytes)}`"
        >
          {{ (summary.error_rate * 100).toFixed(1) }}%
        </StatCard>
      </div>

      <!-- Latency ladder: every phase and tail percentile on one shared scale -->
      <div class="table-card ladder-card">
        <div class="card-head">
          <h2 class="chart-heading">{{ $t("passthrough.ladder") }}</h2>
          <span class="card-note mono-val">{{ scaleMaxLabel }}</span>
        </div>
        <div class="ladder" role="table" :aria-label="$t('passthrough.ladder')">
          <template v-for="group in ladderGroups" :key="group.key">
            <p class="ladder-group">{{ $t(`passthrough.${group.key}`) }}</p>
            <div
              v-for="row in group.rows"
              :key="row.label"
              class="ladder-row"
              role="row"
            >
              <span class="ladder-label" role="rowheader">{{ row.label }}</span>
              <span class="ladder-track">
                <span
                  class="ladder-fill"
                  :class="`ladder-fill--${row.tone}`"
                  :style="{ width: barWidth(row.value) }"
                />
              </span>
              <span class="ladder-value mono-val" :class="{ 'mono-val--dim': row.value === null }" role="cell">
                {{ row.value === null ? "-" : fmtMs(row.value) }}
              </span>
            </div>
          </template>
        </div>
        <p class="ladder-foot">{{ $t("passthrough.ladderNote") }}</p>
      </div>

      <div class="charts-row">
        <div class="table-card chart-section">
          <div class="card-head">
            <h2 class="chart-heading">{{ $t("passthrough.timeseries") }}</h2>
            <span class="card-note mono-val">{{ bucketCount }}</span>
          </div>
          <div class="chart-area">
            <Line v-if="store.perf.timeseries.length > 1" :data="seriesChart" :options="seriesOptions" />
            <EmptyState
              v-else
              icon="mdi-chart-line-variant"
              :text="$t('passthrough.notEnoughPoints')"
              small
            />
          </div>
        </div>

        <div class="table-card breakdown-card">
          <h2 class="chart-heading">{{ $t("passthrough.byHost") }}</h2>
          <div class="table-scroll">
            <table v-if="store.perf.by_host.length" class="perf-table">
              <thead>
                <tr>
                  <th>{{ $t("passthrough.host") }}</th>
                  <th class="num">{{ $t("passthrough.requests") }}</th>
                  <th class="num">{{ $t("passthrough.avgTotal") }}</th>
                  <th class="num">TTFB</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="h in store.perf.by_host"
                  :key="h.host"
                  class="host-row"
                  tabindex="0"
                  role="button"
                  :aria-label="`${h.host} — ${$t('passthrough.filterByHost')}`"
                  @click="filterByHost(h.host)"
                  @keydown.enter.prevent="filterByHost(h.host)"
                  @keydown.space.prevent="filterByHost(h.host)"
                >
                  <td><MonoTag>{{ h.host }}</MonoTag></td>
                  <td class="num mono-val">
                    {{ h.requests.toLocaleString() }}
                    <span v-if="h.errors" class="err-count mono-val">{{ h.errors }}{{ $t("passthrough.errors") }}</span>
                  </td>
                  <td class="num mono-val">{{ fmtMs(h.avg_total_ms) }}</td>
                  <td class="num mono-val">{{ fmtMs(h.avg_ttfb_ms) }}</td>
                </tr>
              </tbody>
            </table>
            <EmptyState v-else icon="mdi-server-network" :text="$t('passthrough.noData')" small />
          </div>
        </div>
      </div>

      <!-- Individual measurements -->
      <div class="table-card">
        <div class="card-head card-head--flush">
          <h2 class="chart-heading">{{ $t("passthrough.recent") }}</h2>
          <span class="card-note">{{ $t("passthrough.recentNote") }}</span>
        </div>

        <div class="table-scroll">
          <table v-if="store.logs.length" class="perf-table logs-table">
            <thead>
              <tr>
                <th>{{ $t("passthrough.time") }}</th>
                <th>{{ $t("passthrough.host") }}</th>
                <th>{{ $t("passthrough.path") }}</th>
                <th>{{ $t("passthrough.status") }}</th>
                <th class="num">DNS</th>
                <th class="num">TCP</th>
                <th class="num">TLS</th>
                <th class="num">TTFB</th>
                <th class="num">TTFT</th>
                <th class="num">{{ $t("passthrough.total") }}</th>
                <th class="num">{{ $t("passthrough.responseSize") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="log in store.logs" :key="log.id">
                <td class="dim-text">{{ formatTime(log.started_at) }}</td>
                <td><MonoTag>{{ log.host || "-" }}</MonoTag></td>
                <td class="path-cell">
                  <span class="method-chip type-chip">{{ log.method }}</span>
                  <span class="path-text mono-val">{{ log.path }}</span>
                </td>
                <td>
                  <StatusChip v-if="!log.is_error" variant="on">{{ log.status_code }}</StatusChip>
                  <StatusChip v-else variant="off" :title="log.error_message">{{ log.status_code }}</StatusChip>
                </td>
                <td class="num mono-val mono-val--dim">{{ log.dns_ms === null ? "-" : fmtMs(log.dns_ms) }}</td>
                <td class="num mono-val mono-val--dim">{{ log.connect_ms === null ? "-" : fmtMs(log.connect_ms) }}</td>
                <td class="num mono-val mono-val--dim">{{ log.tls_ms === null ? "-" : fmtMs(log.tls_ms) }}</td>
                <td class="num mono-val">{{ log.ttfb_ms === null ? "-" : fmtMs(log.ttfb_ms) }}</td>
                <td class="num mono-val">{{ log.ttft_ms === null ? "-" : fmtMs(log.ttft_ms) }}</td>
                <td class="num mono-val" :class="log.total_ms >= 5000 ? 'mono-val--slow' : 'mono-val--accent'">
                  {{ fmtMs(log.total_ms) }}
                </td>
                <td class="num mono-val mono-val--dim">{{ fmtBytes(log.response_bytes) }}</td>
              </tr>
            </tbody>
          </table>
          <EmptyState v-else-if="!store.logsLoading" icon="mdi-text-box-search-outline" :text="$t('passthrough.noRecords')" small />
        </div>

        <!-- Mobile: same records, thumb-reachable cards -->
        <div class="mobile-cards">
          <MobileDataCard
            v-for="log in store.logs"
            :key="'m' + log.id"
            :items="[
              { label: $t('passthrough.time'), value: formatTime(log.started_at) },
              { label: $t('passthrough.host'), value: log.host || '-' },
              { label: $t('passthrough.path'), value: `${log.method} ${log.path}` },
              { label: $t('passthrough.status'), value: String(log.status_code) },
              { label: $t('passthrough.setup'), value: fmtMs(sumPhases(log)) },
              { label: 'TTFB', value: log.ttfb_ms === null ? '-' : fmtMs(log.ttfb_ms) },
              { label: 'TTFT', value: log.ttft_ms === null ? '-' : fmtMs(log.ttft_ms) },
              { label: $t('passthrough.total'), value: fmtMs(log.total_ms) },
              { label: $t('passthrough.responseSize'), value: fmtBytes(log.response_bytes) },
            ]"
          />
          <EmptyState v-if="!store.logs.length" icon="mdi-text-box-search-outline" :text="$t('passthrough.noRecords')" small />
        </div>

        <div class="table-footer">
          <span class="mono-val">{{ offset + 1 }}</span>
          <span class="dim-text">–</span>
          <span class="mono-val">{{ Math.min(offset + pageSize, store.logTotal) }}</span>
          <span class="dim-text">{{ $t("passthrough.of") }}</span>
          <span class="mono-val">{{ store.logTotal }}</span>
          <div class="pagination-btns">
            <button class="row-btn pagination-btn" :disabled="offset <= 0" :aria-label="$t('passthrough.prevPage')" @click="prevPage">
              <v-icon size="16">mdi-chevron-left</v-icon>
            </button>
            <button
              class="row-btn pagination-btn"
              :disabled="offset + pageSize >= store.logTotal"
              :aria-label="$t('passthrough.nextPage')"
              @click="nextPage"
            >
              <v-icon size="16">mdi-chevron-right</v-icon>
            </button>
          </div>
        </div>
      </div>
    </template>

    <EmptyState v-else-if="!store.loading" icon="mdi-connection" :text="$t('passthrough.noData')" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
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
import { usePassthroughStore } from "../stores/passthrough";
import { useCopyToClipboard } from "../composables/useCopyToClipboard";
import { useMobile } from "../composables/useMobile";
import PageHeader from "../components/PageHeader.vue";
import StatCard from "../components/StatCard.vue";
import MonoTag from "../components/MonoTag.vue";
import StatusChip from "../components/StatusChip.vue";
import EmptyState from "../components/EmptyState.vue";
import MobileDataCard from "../components/MobileDataCard.vue";

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Filler, Tooltip, Legend);

const { t } = useI18n();
const store = usePassthroughStore();
const { copied, copy } = useCopyToClipboard();
const { isMobile } = useMobile();

// A phone-sized card list gets unwieldy fast, so paginate tighter there.
const pageSize = computed(() => (isMobile.value ? 10 : 25));
const offset = ref(0);

const filters = reactive({
  host: "",
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

const live = ref(false);
let liveTimer: ReturnType<typeof setInterval> | undefined;

const emptySummary = {
  total_requests: 0,
  error_rate: 0,
  requests_per_sec: 0,
  avg_total_ms: null,
  p50_total_ms: null,
  p95_total_ms: null,
  p99_total_ms: null,
  avg_ttfb_ms: null,
  avg_ttft_ms: null,
  avg_dns_ms: null,
  avg_connect_ms: null,
  avg_tls_ms: null,
  avg_response_bytes: 0,
};

const summary = computed(() => store.perf?.summary ?? emptySummary);
const hasFilters = computed(() => !!(filters.host || filters.from || filters.to));
const noResults = computed(() => (summary.value.total_requests ?? 0) === 0);
const isEmpty = computed(() => !store.loading && noResults.value && !hasFilters.value);
const filteredEmpty = computed(() => !store.loading && noResults.value && hasFilters.value);
const knownHosts = computed(() => (store.perf?.by_host ?? []).map((h) => h.host));
const bucketCount = computed(() => {
  const n = store.perf?.timeseries.length ?? 0;
  const gran = filters.granularity || t("passthrough.auto");
  return `${n} × ${gran} · UTC`;
});

// Sub-second rates read as noise, so drop to a per-minute unit below 1/s.
const rateLabel = computed(() => {
  const perSec = summary.value.requests_per_sec ?? 0;
  return perSec >= 1
    ? `${fmtRate(perSec)} ${t("passthrough.perSecond")}`
    : `${fmtRate(perSec * 60)} ${t("passthrough.perMinute")}`;
});

const exampleCommand = computed(
  () => `curl ${window.location.origin}/https://api.openai.com/v1/chat/completions \\\n  -H "Authorization: Bearer <upstream-key>" \\\n  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}'`,
);

function numOrNull(v: number | null | undefined): number | null {
  return v === null || v === undefined ? null : v;
}

const setupMs = computed(
  () =>
    (summary.value.avg_dns_ms ?? 0) +
    (summary.value.avg_connect_ms ?? 0) +
    (summary.value.avg_tls_ms ?? 0),
);
const prefillMs = computed<number | null>(() => {
  const ttfb = summary.value.avg_ttfb_ms;
  if (ttfb === null) return null;
  const wait = ttfb - setupMs.value;
  // Phase averages come from different row populations (a reused connection has
  // no DNS/TCP/TLS at all), so a non-positive remainder is not "instant" - it is
  // simply not derivable.
  return wait > 0 ? wait : null;
});
const streamMs = computed(() => {
  const { avg_ttft_ms, avg_ttfb_ms, avg_total_ms } = summary.value;
  if (avg_ttft_ms === null || avg_ttfb_ms === null || avg_total_ms === null) return 0;
  return Math.max(0, avg_total_ms - avg_ttft_ms);
});
const slowTotal = computed(() => (summary.value.avg_total_ms ?? 0) > 5000);

type Tone = "muted" | "amber" | "teal" | "violet" | "error";
interface LadderRow {
  label: string;
  value: number | null;
  tone: Tone;
}
interface LadderGroup {
  key: string;
  rows: LadderRow[];
}

const ladderGroups = computed<LadderGroup[]>(() => {
  const s = summary.value;
  return [
    {
      key: "groupPhases",
      rows: [
        { label: "DNS", value: numOrNull(s.avg_dns_ms), tone: "muted" },
        { label: "TCP", value: numOrNull(s.avg_connect_ms), tone: "muted" },
        { label: "TLS", value: numOrNull(s.avg_tls_ms), tone: "muted" },
        { label: "TTFB", value: numOrNull(s.avg_ttfb_ms), tone: "teal" },
        { label: "TTFT", value: numOrNull(s.avg_ttft_ms), tone: "violet" },
      ],
    },
    {
      key: "groupTotals",
      rows: [
        { label: t("passthrough.p50"), value: s.p50_total_ms, tone: "amber" },
        { label: t("passthrough.avg"), value: s.avg_total_ms, tone: "amber" },
        { label: t("passthrough.p95"), value: s.p95_total_ms, tone: "amber" },
        { label: t("passthrough.p99"), value: s.p99_total_ms, tone: "error" },
      ],
    },
  ];
});

// One shared scale so a phase bar and a percentile bar mean the same thing.
const scaleMax = computed(() => {
  const values = ladderGroups.value
    .flatMap((g) => g.rows.map((r) => r.value ?? 0));
  return Math.max(1, ...values);
});

const scaleMaxLabel = computed(() => `scale 0 – ${fmtMs(scaleMax.value)}`);

function barWidth(value: number | null): string {
  if (value === null) return "0%";
  return `${Math.min(100, (value / scaleMax.value) * 100)}%`;
}

function fmtMs(v: number | null | undefined): string {
  if (v === null || v === undefined) return "-";
  if (v >= 60000) return (v / 60000).toFixed(1) + "m";
  if (v >= 1000) return (v / 1000).toFixed(2) + "s";
  return Math.round(v) + "ms";
}

function fmtRate(v: number): string {
  if (v >= 100) return v.toFixed(0);
  if (v >= 1) return v.toFixed(1);
  return v.toFixed(2);
}

function fmtBytes(v: number | null | undefined): string {
  if (v === null || v === undefined) return "-";
  if (v >= 1048576) return (v / 1048576).toFixed(1) + " MiB";
  if (v >= 1024) return (v / 1024).toFixed(1) + " KiB";
  return Math.round(v) + " B";
}

function sumPhases(log: { dns_ms: number | null; connect_ms: number | null; tls_ms: number | null }): number {
  return (log.dns_ms ?? 0) + (log.connect_ms ?? 0) + (log.tls_ms ?? 0);
}

function formatTime(ts: string): string {
  if (!ts) return "-";
  return new Date(ts).toLocaleString();
}

function queryFilters(): Record<string, unknown> {
  const params: Record<string, unknown> = {};
  if (filters.host) params.host = filters.host;
  if (filters.from) params.from = filters.from;
  if (filters.to) params.to = `${filters.to} 23:59:59`;
  if (filters.granularity) params.granularity = filters.granularity;
  return params;
}

function load() {
  const params = queryFilters();
  offset.value = 0;
  store.fetchPerformance(params);
  store.fetchLogs({ ...params, limit: pageSize.value, offset: 0 });
}

function reloadLogs() {
  store.fetchLogs({ ...queryFilters(), limit: pageSize.value, offset: offset.value });
}

function applyPreset(key: string) {
  applyingPreset = true;
  preset.value = key;
  const ms: Record<string, number> = {
    "1h": 3600e3,
    "24h": 86400e3,
    "7d": 7 * 86400e3,
    "30d": 30 * 86400e3,
  };
  const now = new Date();
  const iso = (d: Date) => d.toISOString().slice(0, 10);
  filters.from = iso(new Date(now.getTime() - ms[key]));
  filters.to = iso(now);
  filters.granularity = key === "1h" ? "minute" : key === "30d" ? "day" : "hour";
  load();
}

function filterByHost(host: string) {
  filters.host = filters.host === host ? "" : host;
  load();
}

function clearFilters() {
  filters.host = "";
  filters.from = "";
  filters.to = "";
  filters.granularity = "";
  preset.value = "";
  load();
}

function nextPage() {
  offset.value += pageSize.value;
  reloadLogs();
}

function prevPage() {
  offset.value = Math.max(0, offset.value - pageSize.value);
  reloadLogs();
}

function toggleLive() {
  live.value = !live.value;
  if (live.value) {
    liveTimer = setInterval(() => {
      store.fetchPerformance(queryFilters());
      store.fetchLogs({ ...queryFilters(), limit: pageSize.value, offset: offset.value });
    }, 5000);
  } else if (liveTimer) {
    clearInterval(liveTimer);
    liveTimer = undefined;
  }
}

watch(filters, () => {
  if (applyingPreset) {
    applyingPreset = false;
    return;
  }
  preset.value = "";
});

const bucketLabels = computed(() =>
  (store.perf?.timeseries ?? []).map((b) => (b.bucket.length === 10 ? b.bucket.slice(5) : b.bucket.slice(5, 16))),
);

const seriesChart = computed(() => ({
  labels: bucketLabels.value,
  datasets: [
    {
      label: t("passthrough.avgTotal"),
      data: store.perf?.timeseries.map((b) => b.avg_total_ms) ?? [],
      borderColor: "#e8a020",
      backgroundColor: "rgba(232, 160, 32, 0.06)",
      fill: true,
      tension: 0.35,
      pointRadius: 2,
      pointHoverRadius: 4,
      borderWidth: 1.5,
      yAxisID: "y",
    },
    {
      label: "TTFB",
      data: store.perf?.timeseries.map((b) => b.avg_ttfb_ms) ?? [],
      borderColor: "#2ec4b6",
      backgroundColor: "transparent",
      borderDash: [5, 4],
      fill: false,
      tension: 0.35,
      pointRadius: 2,
      pointHoverRadius: 4,
      borderWidth: 1.5,
      spanGaps: true,
      yAxisID: "y",
    },
    {
      label: t("passthrough.requests"),
      data: store.perf?.timeseries.map((b) => b.request_count) ?? [],
      borderColor: "#7b61ff",
      backgroundColor: "transparent",
      fill: false,
      tension: 0.35,
      pointRadius: 0,
      borderWidth: 1.2,
      yAxisID: "y1",
    },
  ],
}));

const seriesOptions = {
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
    y: {
      grid: { color: "rgba(255,255,255,0.04)" },
      ticks: {
        color: "#4a4844",
        font: { family: "JetBrains Mono", size: 10 },
        callback: (v: number | string) => fmtMs(Number(v)),
      },
    },
    y1: {
      position: "right" as const,
      grid: { drawOnChartArea: false },
      ticks: { color: "#7b61ff", font: { family: "JetBrains Mono", size: 10 } },
    },
  },
};

onMounted(load);
onUnmounted(() => {
  if (liveTimer) clearInterval(liveTimer);
});
</script>

<style scoped>
@import "../styles/page-shared.css";

/* ── Header actions ── */
.head-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.live-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.live-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--clr-text-dim);
  flex-shrink: 0;
}
.live-dot--on {
  background: var(--color-success);
  animation: pulse 1.6s ease-in-out infinite;
}
@keyframes pulse {
  0%,
  100% {
    opacity: 1;
    box-shadow: 0 0 0 0 rgba(46, 196, 182, 0.35);
  }
  50% {
    opacity: 0.6;
    box-shadow: 0 0 0 4px rgba(46, 196, 182, 0);
  }
}
@media (prefers-reduced-motion: reduce) {
  .live-dot--on {
    animation: none;
  }
}

/* ── Filter bar ── */
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
  border-radius: var(--radius-md);
  border: 1px solid var(--clr-border-strong);
  background: transparent;
  color: var(--color-text-muted);
  font-family: var(--font-mono);
  font-size: var(--text-mono-sm);
  letter-spacing: var(--tracking-mono);
  cursor: pointer;
  transition: all var(--dur-fast) var(--ease-default);
}
.preset-chip:hover {
  border-color: var(--clr-border-accent-soft);
  color: var(--color-primary);
}
.preset-chip--active {
  background: rgba(232, 160, 32, 0.12);
  border-color: var(--clr-border-accent);
  color: var(--color-primary);
}
.field-input--date {
  color-scheme: dark;
}
.alert-dismiss {
  margin-inline-start: auto;
  background: none;
  border: none;
  color: inherit;
  cursor: pointer;
  display: flex;
  padding: 2px;
}

.preset-chip:focus-visible,
.host-row:focus-visible,
.alert-dismiss:focus-visible,
.live-chip:focus-visible {
  outline: none;
  box-shadow: var(--focus-ring-visible);
  border-radius: var(--radius-sm);
}

/* ── Stats ── */
.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--space-3);
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
.stat-value--error {
  color: var(--color-danger);
}
.stat-value--warn {
  color: var(--color-primary);
}
.stat-value--dim {
  color: var(--color-text-dim);
}

/* ── Shared card chrome ── */
.ladder-card,
.chart-section,
.breakdown-card {
  padding: var(--pad-card) var(--pad-card-y);
}
.card-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: 14px;
}
.card-head--flush {
  padding: var(--pad-card) var(--pad-card-y) 0;
  margin-bottom: 10px;
}
.card-head .chart-heading {
  margin: 0;
}
.card-note {
  font-size: var(--text-label);
  color: var(--color-text-dim);
  letter-spacing: var(--tracking-label);
  text-transform: uppercase;
}
.chart-heading {
  font-family: var(--font-display);
  font-size: var(--text-title);
  font-weight: 600;
  font-style: italic;
  color: var(--color-text);
  letter-spacing: var(--tracking-display);
  margin: 0 0 14px;
}

/* ── Latency ladder ── */
.ladder {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.ladder-group {
  font-family: var(--font-body);
  font-size: var(--text-label);
  letter-spacing: var(--tracking-label);
  text-transform: uppercase;
  color: var(--color-text-dim);
  margin: 10px 0 2px;
}
.ladder-group:first-child {
  margin-top: 0;
}
.ladder-row {
  display: grid;
  grid-template-columns: 64px 1fr 76px;
  align-items: center;
  gap: var(--space-3);
}
.ladder-label {
  font-family: var(--font-mono);
  font-size: var(--text-mono-sm);
  letter-spacing: var(--tracking-mono);
  color: var(--color-text-muted);
  white-space: nowrap;
}
.ladder-track {
  display: block;
  height: 8px;
  border-radius: var(--radius-pill);
  background: var(--clr-border-subtle);
  overflow: hidden;
}
.ladder-fill {
  display: block;
  height: 100%;
  min-width: 2px;
  border-radius: var(--radius-pill);
  transition: width var(--dur-bar) var(--ease-out);
}
.ladder-fill--muted {
  background: var(--clr-text-faint);
}
.ladder-fill--amber {
  background: var(--color-primary);
}
.ladder-fill--teal {
  background: var(--color-success);
}
.ladder-fill--violet {
  background: var(--clr-violet);
}
.ladder-fill--error {
  background: var(--color-danger);
}
.ladder-value {
  text-align: end;
  color: var(--color-text);
}
.ladder-foot {
  font-family: var(--font-body);
  font-size: var(--text-label);
  line-height: var(--lh-label);
  color: var(--color-text-dim);
  margin: 12px 0 0;
  max-width: var(--measure);
}
@media (max-width: 560px) {
  .ladder-row {
    grid-template-columns: 52px 1fr 64px;
    gap: var(--space-2);
  }
}

/* ── Charts + tables ── */
.charts-row {
  display: grid;
  grid-template-columns: 3fr 2fr;
  gap: var(--space-3);
}
@media (max-width: 900px) {
  .charts-row {
    grid-template-columns: 1fr;
  }
}
.chart-area {
  height: 240px;
  position: relative;
}
.table-scroll {
  overflow-x: auto;
}
.perf-table {
  width: 100%;
  border-collapse: collapse;
  font-family: var(--font-body);
  font-size: var(--text-body-sm);
}
.perf-table th {
  text-align: left;
  color: var(--color-text-dim);
  font-weight: 500;
  font-size: var(--text-label);
  text-transform: uppercase;
  letter-spacing: var(--tracking-label);
  padding: 8px 10px;
  border-bottom: 1px solid var(--clr-border);
  white-space: nowrap;
  background: var(--color-surface);
}
.perf-table td {
  padding: 9px 10px;
  border-bottom: 1px solid var(--clr-border-subtle);
  color: var(--color-text);
  white-space: nowrap;
}
.perf-table tbody tr:hover {
  background: var(--table-row-hover);
}
.perf-table tr:last-child td {
  border-bottom: none;
}
.perf-table .num {
  text-align: right;
}
.logs-table {
  min-width: 880px;
}
.host-row {
  cursor: pointer;
}
.err-count {
  margin-inline-start: 6px;
  color: var(--color-danger);
  font-size: var(--text-mono-sm);
}
.path-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  max-width: 320px;
}
.path-text {
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--color-text-muted);
}
.method-chip {
  font-size: var(--text-mono-sm);
  padding: 2px 6px;
}

.mono-val {
  font-family: var(--font-mono);
  font-size: var(--text-mono);
  font-variant-numeric: tabular-nums;
  letter-spacing: var(--tracking-mono);
}
.mono-val--dim {
  color: var(--color-text-dim);
}
.mono-val--accent {
  color: var(--color-primary);
}
.mono-val--slow {
  color: var(--color-danger);
}
.dim-text {
  font-family: var(--font-body);
  font-size: var(--text-body-sm);
  color: var(--color-text-muted);
}

/* ── Empty primer ── */
.primer-card {
  padding: var(--space-6) var(--space-5);
}
.primer-body {
  font-family: var(--font-body);
  font-size: var(--text-body);
  line-height: var(--lh-body);
  color: var(--color-text-muted);
  max-width: var(--measure);
  margin: 4px 0 0;
}
.primer-cmd {
  display: block;
  font-family: var(--font-mono);
  font-size: var(--text-mono-sm);
  line-height: var(--lh-mono);
  color: var(--color-text);
  background: var(--color-surface-raised);
  border: 1px solid var(--clr-border-strong);
  border-radius: var(--radius-md);
  padding: 12px 14px;
  margin: 6px 0 12px;
  max-width: 100%;
  overflow-x: auto;
  white-space: pre;
  text-align: start;
}

/* ── Footer / mobile ── */
.table-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-1);
  padding: 10px var(--pad-card-y);
  border-top: 1px solid var(--clr-border);
  font-family: var(--font-body);
  font-size: var(--text-body-sm);
  color: var(--color-text-dim);
}
.pagination-btns {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  margin-inline-start: var(--space-3);
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
  padding: 0 var(--space-3);
}
@media (max-width: 768px) {
  .logs-table {
    display: none;
  }
  .mobile-cards {
    display: block;
  }
  .table-footer {
    justify-content: center;
  }
  .head-actions {
    width: 100%;
  }
}
</style>
