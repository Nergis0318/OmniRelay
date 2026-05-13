<template>
    <div class="page">
        <div class="page-header">
            <div>
                <h1 class="page-title">Usage Logs</h1>
                <p class="page-sub">Request history and token accounting</p>
            </div>
            <button class="btn-tonal" @click="loadLogs">
                <v-icon size="15">mdi-refresh</v-icon>
                Refresh
            </button>
        </div>

        <!-- Filter bar -->
        <div class="filter-bar">
            <div class="filter-col">
                <label class="field-label">Model</label>
                <input v-model="filters.model" class="field-input" placeholder="Filter by model..." />
            </div>
            <div class="filter-col">
                <label class="field-label">From</label>
                <input v-model="filters.from" type="date" class="field-input field-input--date" />
            </div>
            <div class="filter-col">
                <label class="field-label">To</label>
                <input v-model="filters.to" type="date" class="field-input field-input--date" />
            </div>
            <button class="btn-primary filter-submit" @click="loadLogs" :disabled="store.loading">
                <span v-if="!store.loading">Apply</span>
                <span v-else class="btn-spinner" />
            </button>
        </div>

        <div class="table-card">
            <v-data-table
                :headers="headers"
                :items="store.logs"
                :loading="store.loading"
                density="comfortable"
                hide-default-footer
                :items-per-page="-1"
            >
                <template #item.created_at="{ item }">
                    <span class="dim-text">{{ new Date(item.created_at).toLocaleString() }}</span>
                </template>
                <template #item.model="{ item }">
                    <code class="mono-tag">{{ item.model }}</code>
                </template>
                <template #item.cost="{ item }">
                    <span class="cost-val">${{ item.cost.toFixed(6) }}</span>
                </template>
                <template #item.is_error="{ item }">
                    <span class="status-chip" :class="item.is_error ? 'status-chip--off' : 'status-chip--on'">
                        {{ item.is_error ? "Error" : "OK" }}
                    </span>
                </template>
                <template #item.latency_ms="{ item }">
                    <span class="latency-val" :class="item.latency_ms > 3000 ? 'latency-val--slow' : ''">
                        {{ item.latency_ms }}ms
                    </span>
                </template>
                <template #item.request_tokens="{ item }">
                    <span class="mono-val">{{ item.request_tokens.toLocaleString() }}</span>
                </template>
                <template #item.response_tokens="{ item }">
                    <span class="mono-val">{{ item.response_tokens.toLocaleString() }}</span>
                </template>
                <template #item.total_tokens="{ item }">
                    <span class="mono-val mono-val--accent">{{ item.total_tokens.toLocaleString() }}</span>
                </template>
                <template #no-data>
                    <div class="empty-state">
                        <v-icon size="32" color="#4a4844">mdi-chart-line-variant</v-icon>
                        <p>No usage records found.</p>
                    </div>
                </template>
            </v-data-table>

            <div class="table-footer">
                <span class="mono-val">{{ store.logs.length }}</span>
                <span class="dim-text"> of </span>
                <span class="mono-val">{{ store.total }}</span>
                <span class="dim-text"> records</span>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from "vue";
import { useUsageStore } from "../stores/usage";

const store = useUsageStore();

const filters = ref({ model: "", from: "", to: "" });

const headers = [
    { title: "Time", key: "created_at", minWidth: "150" },
    { title: "Model", key: "model" },
    { title: "Req", key: "request_tokens" },
    { title: "Resp", key: "response_tokens" },
    { title: "Total", key: "total_tokens" },
    { title: "Latency", key: "latency_ms" },
    { title: "Cost", key: "cost" },
    { title: "Status", key: "is_error" },
];

async function loadLogs() {
    const params: Record<string, any> = { limit: 50 };
    if (filters.value.model) params.model = filters.value.model;
    if (filters.value.from) params.from = filters.value.from;
    if (filters.value.to) params.to = filters.value.to;
    await store.fetchLogs(params);
}

onMounted(() => loadLogs());
</script>

<style scoped>
@import '../styles/page-shared.css';

.dim-text {
    font-family: 'DM Sans', sans-serif;
    font-size: 0.82rem;
    color: #7c7a75;
}
.mono-val {
    font-family: 'JetBrains Mono', monospace;
    font-size: 0.82rem;
    color: #e8e6e1;
}
.mono-val--accent { color: #e8a020; }

.cost-val {
    font-family: 'JetBrains Mono', monospace;
    font-size: 0.78rem;
    color: #2ec4b6;
}
.latency-val {
    font-family: 'JetBrains Mono', monospace;
    font-size: 0.78rem;
    color: #7c7a75;
}
.latency-val--slow { color: #ff5757; }

.filter-submit { align-self: flex-end; }

.field-input--date {
    color-scheme: dark;
}

.table-footer {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 4px;
    padding: 10px 16px;
    border-top: 1px solid rgba(255,255,255,0.05);
    font-family: 'DM Sans', sans-serif;
    font-size: 0.8rem;
    color: #4a4844;
}
</style>
