<template>
  <div>
    <h1 class="text-h4 mb-6">Dashboard</h1>

    <v-row>
      <v-col cols="12" sm="6" md="3" v-for="card in statCards" :key="card.title">
        <v-card>
          <v-card-item>
            <template #prepend>
              <v-icon :color="card.color" size="36">{{ card.icon }}</v-icon>
            </template>
            <v-card-title class="text-h6">{{ card.title }}</v-card-title>
            <v-card-subtitle>{{ formatValue(card.value, card.format) }}</v-card-subtitle>
          </v-card-item>
        </v-card>
      </v-col>
    </v-row>

    <v-row class="mt-4">
      <v-col cols="12" md="8">
        <v-card>
          <v-card-title>Usage Over Time (30 Days)</v-card-title>
          <v-card-text>
            <Line v-if="chartData" :data="chartData" :options="chartOptions" style="max-height: 300px" />
            <p v-else class="text-caption text-grey">No usage data yet</p>
          </v-card-text>
        </v-card>
      </v-col>
      <v-col cols="12" md="4">
        <v-card>
          <v-card-title>Quick Info</v-card-title>
          <v-card-text>
            <v-list density="compact">
              <v-list-item title="Active Providers" :subtitle="String(stats?.providers_count ?? 0)" prepend-icon="mdi-server" />
              <v-list-item title="Registered Models" :subtitle="String(stats?.models_count ?? 0)" prepend-icon="mdi-cube-outline" />
              <v-list-item title="Active API Keys" :subtitle="String(stats?.active_keys ?? 0)" prepend-icon="mdi-key-variant" />
              <v-list-item title="Total Requests" :subtitle="String(stats?.total_requests ?? 0)" prepend-icon="mdi-send" />
            </v-list>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { Line } from 'vue-chartjs'
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
} from 'chart.js'
import { useUsageStore } from '../stores/usage'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Title, Tooltip, Legend, Filler)

const usageStore = useUsageStore()

const stats = computed(() => usageStore.stats)

const statCards = computed(() => [
  {
    title: 'Total Tokens',
    value: stats.value?.total_tokens ?? 0,
    format: 'number',
    icon: 'mdi-code-tags',
    color: 'primary',
  },
  {
    title: 'Total Cost',
    value: stats.value?.total_cost ?? 0,
    format: 'cost',
    icon: 'mdi-currency-usd',
    color: 'success',
  },
  {
    title: 'Avg Latency',
    value: stats.value?.avg_latency_ms ?? 0,
    format: 'latency',
    icon: 'mdi-speedometer',
    color: 'info',
  },
  {
    title: 'Total Requests',
    value: stats.value?.total_requests ?? 0,
    format: 'number',
    icon: 'mdi-send',
    color: 'warning',
  },
])

const chartData = computed(() => {
  if (!stats.value?.daily_usage?.length) return null
  return {
    labels: stats.value.daily_usage.map((d) => d.date),
    datasets: [
      {
        label: 'Tokens',
        data: stats.value.daily_usage.map((d) => d.total_tokens),
        borderColor: '#6750A4',
        backgroundColor: 'rgba(103, 80, 164, 0.1)',
        fill: true,
        yAxisID: 'y',
      },
      {
        label: 'Cost ($)',
        data: stats.value.daily_usage.map((d) => d.total_cost),
        borderColor: '#4CAF50',
        backgroundColor: 'rgba(76, 175, 80, 0.05)',
        fill: true,
        yAxisID: 'y1',
      },
    ],
  }
})

const chartOptions = {
  responsive: true,
  interaction: { intersect: false, mode: 'index' as const },
  scales: {
    y: { type: 'linear' as const, position: 'left' as const, title: { display: true, text: 'Tokens' } },
    y1: { type: 'linear' as const, position: 'right' as const, grid: { drawOnChartArea: false }, title: { display: true, text: 'Cost ($)' } },
  },
}

function formatValue(value: number, format: string): string {
  switch (format) {
    case 'cost':
      return `$${value.toFixed(4)}`
    case 'latency':
      return `${value.toFixed(0)} ms`
    default:
      return value.toLocaleString()
  }
}

onMounted(() => {
  usageStore.fetchStats()
})
</script>
