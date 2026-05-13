<template>
    <div class="dash">
        <!-- Page header -->
        <div class="dash-header">
            <div>
                <h1 class="dash-title">Dashboard</h1>
                <p class="dash-sub">Inference gateway overview</p>
            </div>
            <div class="dash-time">{{ currentTime }}</div>
        </div>

        <!-- Stat cards -->
        <div class="stat-grid">
            <div
                v-for="card in statCards"
                :key="card.title"
                class="stat-card"
                :style="{ '--accent': card.accent }"
            >
                <div class="stat-card__top">
                    <span class="stat-card__label">{{ card.title }}</span>
                    <div class="stat-card__icon-wrap" :style="{ '--accent': card.accent }">
                        <v-icon size="16">{{ card.icon }}</v-icon>
                    </div>
                </div>
                <div class="stat-card__value">{{ formatValue(card.value, card.format) }}</div>
                <div class="stat-card__bar">
                    <div class="stat-card__bar-fill" :style="{ background: card.accent, width: card.barPct + '%' }" />
                </div>
            </div>
        </div>

        <!-- Charts row -->
        <div class="charts-row">
            <div class="chart-panel">
                <div class="panel-header">
                    <span class="panel-title">Usage — 30 days</span>
                    <div class="legend">
                        <span class="legend-dot" style="background: #e8a020" />
                        <span class="legend-label">Tokens</span>
                        <span class="legend-dot" style="background: #2ec4b6" />
                        <span class="legend-label">Cost</span>
                    </div>
                </div>
                <div class="chart-wrap">
                    <Line v-if="chartData" :data="chartData" :options="chartOptions" />
                    <div v-else class="chart-empty">
                        <span class="chart-empty__icon">
                            <v-icon size="28" color="#4a4844">mdi-chart-line-variant</v-icon>
                        </span>
                        <span>No usage data yet</span>
                    </div>
                </div>
            </div>

            <div class="info-panel">
                <div class="panel-header">
                    <span class="panel-title">System</span>
                    <span class="status-badge">
                        <span class="status-dot" />
                        Operational
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
import { computed, onMounted, ref } from "vue";
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

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Title, Tooltip, Legend, Filler);

const usageStore = useUsageStore();
const stats = computed(() => usageStore.stats);

const currentTime = ref("");
function tick() {
    currentTime.value = new Date().toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}
tick();
setInterval(tick, 1000);

const statCards = computed(() => {
    const requests = stats.value?.total_requests ?? 0;
    const tokens = stats.value?.total_tokens ?? 0;
    const cost = stats.value?.total_cost ?? 0;
    const latency = stats.value?.avg_latency_ms ?? 0;
    return [
        {
            title: "Total Requests",
            value: requests,
            format: "number",
            icon: "mdi-send-outline",
            accent: "#e8a020",
            barPct: Math.min(100, (requests / Math.max(1, requests)) * 100),
        },
        {
            title: "Total Tokens",
            value: tokens,
            format: "number",
            icon: "mdi-code-tags",
            accent: "#2ec4b6",
            barPct: Math.min(100, (tokens / Math.max(1, tokens)) * 100),
        },
        {
            title: "Total Cost",
            value: cost,
            format: "cost",
            icon: "mdi-currency-usd",
            accent: "#7b61ff",
            barPct: Math.min(100, (cost / Math.max(0.01, cost)) * 100),
        },
        {
            title: "Avg Latency",
            value: latency,
            format: "latency",
            icon: "mdi-speedometer",
            accent: latency > 2000 ? "#ff5757" : "#e8a020",
            barPct: Math.min(100, (latency / 5000) * 100),
        },
    ];
});

const infoRows = computed(() => [
    { label: "Active Providers", value: String(stats.value?.providers_count ?? 0), icon: "mdi-server-outline" },
    { label: "Registered Models", value: String(stats.value?.models_count ?? 0), icon: "mdi-cube-outline" },
    { label: "Active API Keys", value: String(stats.value?.active_keys ?? 0), icon: "mdi-key-outline" },
    { label: "Total Requests", value: (stats.value?.total_requests ?? 0).toLocaleString(), icon: "mdi-send-outline" },
]);

const chartData = computed(() => {
    if (!stats.value?.daily_usage?.length) return null;
    return {
        labels: stats.value.daily_usage.map((d) => d.date.slice(5)),
        datasets: [
            {
                label: "Tokens",
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
                label: "Cost ($)",
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

function formatValue(value: number, format: string): string {
    switch (format) {
        case "cost": return `$${value.toFixed(4)}`;
        case "latency": return `${value.toFixed(0)} ms`;
        default: return value.toLocaleString();
    }
}

onMounted(() => usageStore.fetchStats());
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
    font-family: 'Fraunces', Georgia, serif;
    font-size: 1.7rem;
    font-weight: 600;
    font-style: italic;
    color: #e8e6e1;
    letter-spacing: -0.03em;
    line-height: 1;
    margin: 0 0 4px;
}
.dash-sub {
    font-family: 'DM Sans', sans-serif;
    font-size: 0.8rem;
    color: #4a4844;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    margin: 0;
}
.dash-time {
    font-family: 'JetBrains Mono', monospace;
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
@media (max-width: 900px) { .stat-grid { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 560px) { .stat-grid { grid-template-columns: 1fr; } }

.stat-card {
    background: #131316;
    border: 1px solid rgba(255,255,255,0.06);
    border-radius: 12px;
    padding: 18px 20px 14px;
    position: relative;
    overflow: hidden;
    transition: border-color 0.2s;
}
.stat-card::before {
    content: '';
    position: absolute;
    top: 0; left: 0; right: 0;
    height: 2px;
    background: var(--accent, #e8a020);
    opacity: 0.7;
}
.stat-card:hover {
    border-color: rgba(255,255,255,0.1);
}

.stat-card__top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 10px;
}
.stat-card__label {
    font-family: 'DM Sans', sans-serif;
    font-size: 0.75rem;
    font-weight: 500;
    color: #7c7a75;
    letter-spacing: 0.04em;
    text-transform: uppercase;
}
.stat-card__icon-wrap {
    width: 28px; height: 28px;
    border-radius: 7px;
    background: rgba(255,255,255,0.04);
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--accent, #e8a020);
}
.stat-card__value {
    font-family: 'JetBrains Mono', monospace;
    font-size: 1.5rem;
    font-weight: 500;
    color: #e8e6e1;
    letter-spacing: -0.02em;
    line-height: 1;
    margin-bottom: 12px;
}
.stat-card__bar {
    height: 2px;
    background: rgba(255,255,255,0.05);
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
@media (max-width: 900px) { .charts-row { grid-template-columns: 1fr; } }

.chart-panel,
.info-panel {
    background: #131316;
    border: 1px solid rgba(255,255,255,0.06);
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
    font-family: 'Fraunces', Georgia, serif;
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
    width: 6px; height: 6px;
    border-radius: 50%;
}
.legend-label {
    font-family: 'DM Sans', sans-serif;
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
    font-family: 'DM Sans', sans-serif;
    font-size: 0.82rem;
    color: #4a4844;
}

.status-badge {
    display: flex;
    align-items: center;
    gap: 6px;
    font-family: 'DM Sans', sans-serif;
    font-size: 0.72rem;
    color: #2ec4b6;
    background: rgba(46, 196, 182, 0.1);
    border: 1px solid rgba(46, 196, 182, 0.2);
    border-radius: 20px;
    padding: 3px 10px;
}
.status-dot {
    width: 5px; height: 5px;
    border-radius: 50%;
    background: #2ec4b6;
    animation: pulse 2s ease infinite;
}
@keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.4; }
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
    border-bottom: 1px solid rgba(255,255,255,0.05);
}
.info-row:last-child { border-bottom: none; }
.info-row__left {
    display: flex;
    align-items: center;
    gap: 8px;
}
.info-row__icon { color: #4a4844; }
.info-row__label {
    font-family: 'DM Sans', sans-serif;
    font-size: 0.83rem;
    color: #7c7a75;
}
.info-row__value {
    font-family: 'JetBrains Mono', monospace;
    font-size: 0.85rem;
    font-weight: 500;
    color: #e8e6e1;
}
</style>
