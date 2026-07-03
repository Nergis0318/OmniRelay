<template>
  <div class="dash">
    <!-- Page header -->
    <div class="dash-header">
      <div>
        <h1 class="dash-title">{{ $t("dashboard.title") }}</h1>
        <p class="dash-sub">{{ $t("dashboard.subtitle") }}</p>
      </div>
      <div class="dash-time">{{ currentTime }}</div>
    </div>

    <!-- Stat cards -->
    <div class="stat-grid">
      <div class="stat-card" style="--accent: #e8a020">
        <div class="stat-card__top">
          <span class="stat-card__label">{{ $t("dashboard.todayCost") }}</span>
        </div>
        <div class="stat-card__value">${{ todayCost.toFixed(4) }}</div>
        <div class="stat-card__sub">
          {{ $t("dashboard.total") }}: ${{
            (stats?.total_cost ?? 0).toFixed(4)
          }}
        </div>
        <div class="stat-card__bar">
          <div
            class="stat-card__bar-fill"
            :style="{ background: '#e8a020', width: todayCostBarPct + '%' }"
          />
        </div>
      </div>

      <div class="stat-card" style="--accent: #2ec4b6">
        <div class="stat-card__top">
          <span class="stat-card__label">{{
            $t("dashboard.todayRequests")
          }}</span>
        </div>
        <div class="stat-card__value">{{ todayRequests.toLocaleString() }}</div>
        <div class="stat-card__sub">
          {{ $t("dashboard.total") }}:
          {{ (stats?.total_requests ?? 0).toLocaleString() }}
        </div>
        <div class="stat-card__bar">
          <div
            class="stat-card__bar-fill"
            :style="{ background: '#2ec4b6', width: todayRequestsBarPct + '%' }"
          />
        </div>
      </div>

      <div class="stat-card" style="--accent: #7b61ff">
        <div class="stat-card__top">
          <span class="stat-card__label">{{
            $t("dashboard.todayTokens")
          }}</span>
        </div>
        <div class="stat-card__value">{{ todayTokens.toLocaleString() }}</div>
        <div class="stat-card__sub">
          {{ $t("dashboard.total") }}:
          {{ (stats?.total_tokens ?? 0).toLocaleString() }}
        </div>
        <div class="stat-card__bar">
          <div
            class="stat-card__bar-fill"
            :style="{ background: '#7b61ff', width: todayTokensBarPct + '%' }"
          />
        </div>
      </div>

      <div class="stat-card" style="--accent: #ff5757">
        <div class="stat-card__top">
          <span class="stat-card__label">{{
            $t("dashboard.performance")
          }}</span>
        </div>
        <div class="stat-card__value stat-card__value--dual">
          <span class="perf-rpm">RPM: {{ rpm.toFixed(0) }}</span>
          <span class="perf-tpm">TPM: {{ tpm.toFixed(0) }}</span>
        </div>
        <div class="stat-card__sub">&nbsp;</div>
        <div class="stat-card__bar">
          <div
            class="stat-card__bar-fill"
            :style="{ background: '#ff5757', width: perfBarPct + '%' }"
          />
        </div>
      </div>

      <div class="stat-card" style="--accent: #e8a020">
        <div class="stat-card__top">
          <span class="stat-card__label">{{ $t("dashboard.avgLatency") }}</span>
          <div
            class="stat-card__icon-wrap"
            :style="{ '--accent': latencySec > 2 ? '#ff5757' : '#e8a020' }"
          >
            <v-icon size="16">mdi-speedometer</v-icon>
          </div>
        </div>
        <div class="stat-card__value">{{ latencySec.toFixed(2) }} s</div>
        <div class="stat-card__sub">&nbsp;</div>
        <div class="stat-card__bar">
          <div
            class="stat-card__bar-fill"
            :style="{
              background: latencySec > 2 ? '#ff5757' : '#e8a020',
              width: latencyBarPct + '%',
            }"
          />
        </div>
      </div>
    </div>

    <!-- Charts row -->
    <div class="charts-row">
      <div class="chart-panel">
        <div class="panel-header">
          <span class="panel-title">{{ $t("dashboard.usage30Days") }}</span>
          <div class="legend">
            <span class="legend-dot" style="background: #e8a020" />
            <span class="legend-label">{{ $t("dashboard.tokens") }}</span>
            <span class="legend-dot" style="background: #2ec4b6" />
            <span class="legend-label">{{ $t("dashboard.cost") }}</span>
          </div>
        </div>
        <div class="chart-wrap">
          <Line v-if="chartData" :data="chartData" :options="chartOptions" />
          <div v-else class="chart-empty">
            <span class="chart-empty__icon">
              <v-icon size="28" color="#4a4844">mdi-chart-line-variant</v-icon>
            </span>
            <span>{{ $t("dashboard.noUsageData") }}</span>
          </div>
        </div>
      </div>

      <div class="info-panel">
        <div class="panel-header">
          <span class="panel-title">{{ $t("dashboard.system") }}</span>
          <span class="status-badge">
            <span class="status-dot" />
            {{ $t("dashboard.operational") }}
          </span>
        </div>
        <div class="info-rows">
          <div v-for="row in infoRows" :key="row.label" class="info-row">
            <div class="info-row__left">
              <v-icon size="14" class="info-row__icon">{{ row.icon }}</v-icon>
              <span class="info-row__label">{{ row.label }}</span>
            </div>
            <span class="info-row__value">{{ row.value }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { Line } from "vue-chartjs";
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler,
} from "chart.js";
import { useUsageStore } from "../stores/usage";

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler,
);

const { t } = useI18n();
const usageStore = useUsageStore();
const stats = computed(() => usageStore.stats);

const currentTime = ref("");
function tick() {
  currentTime.value = new Date().toLocaleTimeString("en-US", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}
tick();
setInterval(tick, 1000);

const todayCost = computed(() => stats.value?.today_cost ?? 0);
const todayRequests = computed(() => stats.value?.today_requests ?? 0);
const todayTokens = computed(() => stats.value?.today_tokens ?? 0);
const rpm = computed(() => stats.value?.rpm ?? 0);
const tpm = computed(() => stats.value?.tpm ?? 0);
const latency = computed(() => stats.value?.avg_latency_ms ?? 0);
const latencySec = computed(() => latency.value / 1000);

const todayCostBarPct = computed(() => {
  const total = stats.value?.total_cost ?? 0;
  return total > 0 ? Math.min(100, (todayCost.value / total) * 100) : 0;
});
const todayRequestsBarPct = computed(() => {
  const total = stats.value?.total_requests ?? 0;
  return total > 0 ? Math.min(100, (todayRequests.value / total) * 100) : 0;
});
const todayTokensBarPct = computed(() => {
  const total = stats.value?.total_tokens ?? 0;
  return total > 0 ? Math.min(100, (todayTokens.value / total) * 100) : 0;
});
const perfBarPct = computed(() => Math.min(100, (rpm.value / 1000) * 100));
const latencyBarPct = computed(() =>
  Math.min(100, (latency.value / 5000) * 100),
);

const infoRows = computed(() => [
  {
    label: t("dashboard.activeProviders"),
    value: String(stats.value?.providers_count ?? 0),
    icon: "mdi-server-outline",
  },
  {
    label: t("dashboard.registeredModels"),
    value: String(stats.value?.models_count ?? 0),
    icon: "mdi-cube-outline",
  },
  {
    label: t("dashboard.activeApiKeys"),
    value: String(stats.value?.active_keys ?? 0),
    icon: "mdi-key-outline",
  },
  {
    label: t("logs.totalRequests"),
    value: (stats.value?.total_requests ?? 0).toLocaleString(),
    icon: "mdi-send-outline",
  },
]);

const chartData = computed(() => {
  if (!stats.value?.daily_usage?.length) return null;
  return {
    labels: stats.value.daily_usage.map((d) => d.date.slice(5)),
    datasets: [
      {
        label: t("dashboard.tokens"),
        data: stats.value.daily_usage.map((d) => d.total_tokens),
        borderColor: "#e8a020",
        backgroundColor: "rgba(232, 160, 32, 0.06)",
        fill: true,
        tension: 0.4,
        pointRadius: 3,
        pointHoverRadius: 5,
        pointBackgroundColor: "#e8a020",
        borderWidth: 1.5,
        yAxisID: "y",
      },
      {
        label: t("dashboard.costAxis"),
        data: stats.value.daily_usage.map((d) => d.total_cost),
        borderColor: "#2ec4b6",
        backgroundColor: "rgba(46, 196, 182, 0.04)",
        fill: true,
        tension: 0.4,
        pointRadius: 3,
        pointHoverRadius: 5,
        pointBackgroundColor: "#2ec4b6",
        borderWidth: 1.5,
        yAxisID: "y1",
      },
    ],
  };
});

const chartOptions = {
  responsive: true,
  maintainAspectRatio: false,
  interaction: { intersect: false, mode: "index" as const },
  plugins: {
    legend: { display: false },
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
      grid: { color: "rgba(255,255,255,0.04)", drawBorder: false },
      ticks: { color: "#4a4844", font: { family: "JetBrains Mono", size: 10 } },
    },
    y: {
      type: "linear" as const,
      position: "left" as const,
      grid: { color: "rgba(255,255,255,0.04)", drawBorder: false },
      ticks: { color: "#4a4844", font: { family: "JetBrains Mono", size: 10 } },
    },
    y1: {
      type: "linear" as const,
      position: "right" as const,
      grid: { drawOnChartArea: false, drawBorder: false },
      ticks: { color: "#4a4844", font: { family: "JetBrains Mono", size: 10 } },
    },
  },
};

onMounted(() => {
  usageStore.fetchStats();
  usageStore.connect();
});

onUnmounted(() => {
  usageStore.disconnect();
});
</script>

<style scoped>
.dash {
  display: flex;
  flex-direction: column;
  gap: 28px;
}

/* ── Header ── */
.dash-header {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
}
.dash-title {
  font-family: "Fraunces", Georgia, serif;
  font-size: 1.7rem;
  font-weight: 600;
  font-style: italic;
  color: #e8e6e1;
  letter-spacing: -0.03em;
  line-height: 1;
  margin: 0 0 4px;
}
.dash-sub {
  font-family: "DM Sans", sans-serif;
  font-size: 0.8rem;
  color: #4a4844;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  margin: 0;
}
.dash-time {
  font-family: "JetBrains Mono", monospace;
  font-size: 0.8rem;
  color: #4a4844;
  letter-spacing: 0.05em;
}

/* ── Stat grid ── */
.stat-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 14px;
}
@media (max-width: 900px) {
  .stat-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}
@media (max-width: 560px) {
  .stat-grid {
    grid-template-columns: 1fr;
    gap: 10px;
  }
  .dash {
    gap: 18px;
  }
}

@media (max-width: 480px) {
  .dash-title {
    font-size: 1.35rem;
  }
  .stat-card {
    padding: 14px 16px 10px;
  }
  .stat-card__value {
    font-size: 1.2rem;
  }
  .chart-wrap {
    height: 220px;
  }
  .charts-row {
    gap: 10px;
  }
}

.stat-card {
  background: #131316;
  border: 1px solid rgba(255, 255, 255, 0.06);
  border-radius: 12px;
  padding: 18px 20px 14px;
  position: relative;
  overflow: hidden;
  transition: border-color 0.2s;
}
.stat-card::before {
  content: "";
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 2px;
  background: var(--accent, #e8a020);
  opacity: 0.7;
}
.stat-card:hover {
  border-color: rgba(255, 255, 255, 0.1);
}

.stat-card__top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}
.stat-card__label {
  font-family: "DM Sans", sans-serif;
  font-size: 0.75rem;
  font-weight: 500;
  color: #7c7a75;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
.stat-card__icon-wrap {
  width: 28px;
  height: 28px;
  border-radius: 7px;
  background: rgba(255, 255, 255, 0.04);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--accent, #e8a020);
}
.stat-card__value {
  font-family: "JetBrains Mono", monospace;
  font-size: 1.5rem;
  font-weight: 500;
  color: #e8e6e1;
  letter-spacing: -0.02em;
  line-height: 1;
  margin-bottom: 4px;
}
.stat-card__value--dual {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.perf-rpm,
.perf-tpm {
  font-family: "JetBrains Mono", monospace;
  font-size: 1rem;
  font-weight: 500;
  color: #e8e6e1;
}
.stat-card__sub {
  font-family: "DM Sans", sans-serif;
  font-size: 0.72rem;
  color: #4a4844;
  margin-bottom: 8px;
  min-height: 1em;
}
.stat-card__bar {
  height: 2px;
  background: rgba(255, 255, 255, 0.05);
  border-radius: 1px;
  overflow: hidden;
}
.stat-card__bar-fill {
  height: 100%;
  border-radius: 1px;
  transition: width 0.6s ease;
  opacity: 0.6;
}

/* ── Charts row ── */
.charts-row {
  display: grid;
  grid-template-columns: 1fr 300px;
  gap: 14px;
}
@media (max-width: 900px) {
  .charts-row {
    grid-template-columns: 1fr;
  }
}

.chart-panel,
.info-panel {
  background: #131316;
  border: 1px solid rgba(255, 255, 255, 0.06);
  border-radius: 12px;
  padding: 18px 20px;
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.panel-title {
  font-family: "Fraunces", Georgia, serif;
  font-size: 0.95rem;
  font-weight: 600;
  color: #e8e6e1;
  letter-spacing: -0.01em;
}

.legend {
  display: flex;
  align-items: center;
  gap: 10px;
}
.legend-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}
.legend-label {
  font-family: "DM Sans", sans-serif;
  font-size: 0.75rem;
  color: #7c7a75;
}

.chart-wrap {
  height: 240px;
  position: relative;
}
.chart-empty {
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  font-family: "DM Sans", sans-serif;
  font-size: 0.82rem;
  color: #4a4844;
}

.status-badge {
  display: flex;
  align-items: center;
  gap: 6px;
  font-family: "DM Sans", sans-serif;
  font-size: 0.72rem;
  color: #2ec4b6;
  background: rgba(46, 196, 182, 0.1);
  border: 1px solid rgba(46, 196, 182, 0.2);
  border-radius: 20px;
  padding: 3px 10px;
}
.status-dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: #2ec4b6;
  animation: pulse 2s ease infinite;
}
@keyframes pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.4;
  }
}

.info-rows {
  display: flex;
  flex-direction: column;
  gap: 0;
}
.info-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 11px 0;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}
.info-row:last-child {
  border-bottom: none;
}
.info-row__left {
  display: flex;
  align-items: center;
  gap: 8px;
}
.info-row__icon {
  color: #4a4844;
}
.info-row__label {
  font-family: "DM Sans", sans-serif;
  font-size: 0.83rem;
  color: #7c7a75;
}
.info-row__value {
  font-family: "JetBrains Mono", monospace;
  font-size: 0.85rem;
  font-weight: 500;
  color: #e8e6e1;
}
</style>
