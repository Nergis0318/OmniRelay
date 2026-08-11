<template>
  <div class="page">
    <PageHeader :title="$t('usage.title')" :subtitle="$t('usage.subtitle')">
      <button class="btn-tonal" @click="store.fetchStats()">
        <v-icon size="15">mdi-refresh</v-icon>
        {{ $t("common.refresh") }}
      </button>
    </PageHeader>

    <!-- Stats Cards -->
    <div class="stats-grid">
      <StatCard :label="$t('dashboard.totalRequests')">
        {{ stats?.total_requests?.toLocaleString() ?? "-" }}
      </StatCard>
      <StatCard :label="$t('dashboard.totalTokens')" value-class="stat-value--accent">
        {{ stats?.total_tokens?.toLocaleString() ?? "-" }}
      </StatCard>
      <StatCard :label="$t('dashboard.totalCost')" value-class="stat-value--cost">
        ${{ stats?.total_cost?.toFixed(4) ?? "-" }}
      </StatCard>
      <StatCard :label="$t('dashboard.avgLatency')">
        {{ stats ? (stats.avg_latency_ms / 1000).toFixed(2) + "s" : "-" }}
      </StatCard>
      <StatCard :label="$t('usage.cacheWrite5m')">
        {{ stats?.total_cache_write_5m?.toLocaleString() ?? "-" }}
      </StatCard>
      <StatCard :label="$t('usage.cacheWrite1h')">
        {{ stats?.total_cache_write_1h?.toLocaleString() ?? "-" }}
      </StatCard>
      <StatCard :label="$t('usage.cacheRead')" value-class="stat-value--cache-read">
        {{ stats?.total_cache_read?.toLocaleString() ?? "-" }}
      </StatCard>
    </div>

    <!-- 30-day Chart -->
    <div class="table-card chart-section">
      <h2 class="chart-heading">{{ $t("dashboard.usage30Days") }}</h2>
      <div class="chart-area">
        <template v-if="stats?.daily_usage?.length">
          <Line :data="chartData" :options="chartOptions" />
        </template>
        <EmptyState v-else icon="mdi-chart-bar" :text="$t('dashboard.noUsageData')" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted } from "vue";
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
import { useUsageStore } from "../stores/usage";
import PageHeader from "../components/PageHeader.vue";
import StatCard from "../components/StatCard.vue";
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
const store = useUsageStore();
const stats = computed(() => store.stats);

const chartData = computed(() => ({
  labels: stats.value?.daily_usage.map((d) => d.date.slice(5)) ?? [],
  datasets: [
    {
      label: t("dashboard.tokens"),
      data: stats.value?.daily_usage.map((d) => d.total_tokens) ?? [],
      backgroundColor: "rgba(232, 160, 32, 0.1)",
      borderColor: "#e8a020",
      borderWidth: 2,
      fill: true,
      tension: 0.3,
      pointRadius: 3,
      pointHoverRadius: 5,
      yAxisID: "y",
    },
    {
      label: t("dashboard.cost"),
      data: stats.value?.daily_usage.map((d) => d.total_cost) ?? [],
      backgroundColor: "rgba(46, 196, 182, 0.1)",
      borderColor: "#2ec4b6",
      borderWidth: 2,
      fill: true,
      tension: 0.3,
      pointRadius: 3,
      pointHoverRadius: 5,
      yAxisID: "y1",
    },
  ],
}));

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { mode: "index" as const, intersect: false },
  plugins: {
    legend: {
      labels: {
        color: "#7c7a75",
        font: { family: '"DM Sans", sans-serif', size: 12 },
        usePointStyle: true,
        pointStyleWidth: 8,
      },
    },
    tooltip: {
      backgroundColor: "#131316",
      titleColor: "#e8e6e1",
      bodyColor: "#e8e6e1",
      borderColor: "rgba(232,160,32,0.2)",
      borderWidth: 1,
      padding: 12,
    },
  },
  scales: {
    x: {
      ticks: { color: "#4a4844", font: { size: 10 } },
      grid: { color: "rgba(255,255,255,0.04)" },
    },
    y: {
      type: "linear" as const,
      display: true,
      position: "left" as const,
      title: { display: true, text: t("dashboard.tokens"), color: "#e8a020" },
      ticks: { color: "#e8a020", font: { size: 10 } },
      grid: { color: "rgba(255,255,255,0.04)" },
    },
    y1: {
      type: "linear" as const,
      display: true,
      position: "right" as const,
      title: { display: true, text: t("dashboard.costAxis"), color: "#2ec4b6" },
      ticks: { color: "#2ec4b6", font: { size: 10 } },
      grid: { drawOnChartArea: false },
    },
  },
}));

onMounted(() => {
  store.fetchStats();
  store.connect();
});

onUnmounted(() => {
  store.disconnect();
});
</script>

<style scoped>
@import "../styles/page-shared.css";

.stats-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 12px;
}

.stat-value--accent {
  color: #e8a020;
}

.stat-value--cost {
  color: #2ec4b6;
}

.stat-value--cache-read {
  color: #2ec4b6;
}

.chart-section {
  padding: 20px;
}

.chart-heading {
  font-family: "Fraunces", Georgia, serif;
  font-size: 1.05rem;
  font-weight: 600;
  font-style: italic;
  color: #e8e6e1;
  letter-spacing: -0.015em;
  margin: 0 0 16px;
}

.chart-area {
  width: 100%;
  height: 320px;
}

@media (max-width: 768px) {
  .stats-grid {
    grid-template-columns: repeat(2, 1fr);
    gap: 8px;
  }
  .chart-area {
    height: 240px;
  }
  .chart-section {
    padding: 14px;
  }
}
@media (max-width: 480px) {
  .stats-grid {
    grid-template-columns: 1fr;
  }
  .chart-area {
    height: 200px;
  }
}
</style>
