import { defineStore } from "pinia";
import { ref } from "vue";
import api from "../api/client";

interface UsageLog {
  id: number;
  api_key_id: number | null;
  provider_id: number | null;
  model: string;
  request_tokens: number;
  response_tokens: number;
  total_tokens: number;
  latency_ms: number;
  cost: number;
  is_error: boolean;
  error_message: string;
  created_at: string;
}

interface DashboardStats {
  total_requests: number;
  total_tokens: number;
  total_cost: number;
  avg_latency_ms: number;
  active_keys: number;
  providers_count: number;
  models_count: number;
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

  async function fetchStats() {
    loading.value = true;
    try {
      const { data } = await api.get("/stats");
      stats.value = data;
    } finally {
      loading.value = false;
    }
  }

  async function fetchLogs(params?: Record<string, any>) {
    loading.value = true;
    try {
      const { data } = await api.get("/usage", { params });
      logs.value = data.usage_logs;
      total.value = data.total;
    } finally {
      loading.value = false;
    }
  }

  return { stats, logs, total, loading, fetchStats, fetchLogs };
});
