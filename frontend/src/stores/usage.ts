import { defineStore } from "pinia";
import { ref } from "vue";
import api from "../api/client";
import { createRealtimeConnection, StatsDelta } from "../api/ws";

interface UsageLog {
  id: number;
  api_key_id: number | null;
  provider_id: number | null;
  model: string;
  request_tokens: number;
  response_tokens: number;
  total_tokens: number;
  cache_write_5m_tokens: number;
  cache_write_1h_tokens: number;
  cache_read_tokens: number;
  latency_ms: number;
  cost: number;
  is_error: boolean;
  error_message: string;
  started_at: string | null;
  completed_at: string | null;
  created_at: string;
  provider_name: string;
}

interface DashboardStats {
  total_requests: number;
  total_tokens: number;
  total_cost: number;
  avg_latency_ms: number;
  active_keys: number;
  providers_count: number;
  models_count: number;
  total_cache_write_5m: number;
  total_cache_write_1h: number;
  total_cache_read: number;
  today_cost: number;
  today_requests: number;
  today_tokens: number;
  rpm: number;
  tpm: number;
  daily_usage: {
    date: string;
    total_tokens: number;
    total_cost: number;
    request_count: number;
  }[];
}

export const useUsageStore = defineStore("usage", () => {
  const stats = ref<DashboardStats | null>(null);
  const logs = ref<UsageLog[]>([]);
  const total = ref(0);
  const loading = ref(false);
  const error = ref<string | null>(null);

  let rtConn: ReturnType<typeof createRealtimeConnection> | null = null;

  function connect() {
    if (rtConn) return;
    rtConn = createRealtimeConnection();
    rtConn.onUsageLog((entry) => {
      logs.value.unshift(entry as UsageLog);
      if (logs.value.length > 50) logs.value.pop();
      total.value += 1;
    });
    rtConn.onStatsDelta((delta: StatsDelta) => {
      if (!stats.value) return;
      stats.value = {
        ...stats.value,
        total_requests: delta.total_requests,
        total_tokens: delta.total_tokens,
        total_cost: delta.total_cost,
        avg_latency_ms: delta.avg_latency_ms,
        today_cost: delta.today_cost,
        today_requests: delta.today_requests,
        today_tokens: delta.today_tokens,
        rpm: delta.rpm,
        tpm: delta.tpm,
      };
    });
  }

  function disconnect() {
    rtConn?.close();
    rtConn = null;
  }

  function isRealtimeConnected(): boolean {
    return rtConn?.isConnected() ?? false;
  }

  async function fetchStats() {
    loading.value = true;
    error.value = null;
    try {
      const { data } = await api.get("/stats");
      stats.value = data;
    } catch (err: any) {
      error.value = err?.response?.data?.error || err?.message || "Failed to load stats";
    } finally {
      loading.value = false;
    }
  }

  async function fetchLogs(params?: Record<string, any>) {
    loading.value = true;
    error.value = null;
    try {
      const { data } = await api.get("/usage", { params });
      logs.value = data.usage_logs;
      total.value = data.total;
    } catch (err: any) {
      error.value = err?.response?.data?.error || err?.message || "Failed to load logs";
    } finally {
      loading.value = false;
    }
  }

  function clearError() {
    error.value = null;
  }

  return { stats, logs, total, loading, error, fetchStats, fetchLogs, clearError, connect, disconnect, isRealtimeConnected };
});
