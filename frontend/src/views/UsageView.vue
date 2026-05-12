<template>
  <div>
    <div class="d-flex align-center mb-6">
      <h1 class="text-h4">Usage Logs</h1>
      <v-spacer />
      <v-btn variant="tonal" prepend-icon="mdi-refresh" @click="loadLogs">Refresh</v-btn>
    </div>

    <v-card class="mb-4">
      <v-card-text>
        <v-row>
          <v-col cols="12" sm="4">
            <v-text-field v-model="filters.model" label="Model" density="compact" hide-details clearable />
          </v-col>
          <v-col cols="12" sm="3">
            <v-text-field v-model="filters.from" label="From" type="date" density="compact" hide-details clearable />
          </v-col>
          <v-col cols="12" sm="3">
            <v-text-field v-model="filters.to" label="To" type="date" density="compact" hide-details clearable />
          </v-col>
          <v-col cols="12" sm="2">
            <v-btn block color="primary" @click="loadLogs" :loading="store.loading">Filter</v-btn>
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>

    <v-card>
      <v-data-table :headers="headers" :items="store.logs" :loading="store.loading" hover>
        <template #item.created_at="{ item }">
          {{ new Date(item.created_at).toLocaleString() }}
        </template>
        <template #item.cost="{ item }">
          <v-chip size="small" variant="tonal" color="success">
            ${{ item.cost.toFixed(6) }}
          </v-chip>
        </template>
        <template #item.is_error="{ item }">
          <v-chip v-if="item.is_error" size="small" color="error" variant="tonal">Error</v-chip>
          <v-chip v-else size="small" color="success" variant="tonal">OK</v-chip>
        </template>
        <template #item.latency_ms="{ item }">
          {{ item.latency_ms }}ms
        </template>
      </v-data-table>

      <div class="text-center pa-4 text-caption text-grey">
        Showing {{ store.logs.length }} of {{ store.total }} records
      </div>
    </v-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useUsageStore } from '../stores/usage'

const store = useUsageStore()

const filters = ref({
  model: '',
  from: '',
  to: '',
})

const headers = [
  { title: 'Time', key: 'created_at' },
  { title: 'Model', key: 'model' },
  { title: 'Req Tokens', key: 'request_tokens' },
  { title: 'Resp Tokens', key: 'response_tokens' },
  { title: 'Total', key: 'total_tokens' },
  { title: 'Latency', key: 'latency_ms' },
  { title: 'Cost', key: 'cost' },
  { title: 'Status', key: 'is_error' },
]

async function loadLogs() {
  const params: Record<string, any> = { limit: 50 }
  if (filters.value.model) params.model = filters.value.model
  if (filters.value.from) params.from = filters.value.from
  if (filters.value.to) params.to = filters.value.to
  await store.fetchLogs(params)
}

onMounted(() => loadLogs())
</script>
