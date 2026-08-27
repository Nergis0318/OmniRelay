import { defineStore } from "pinia";
import { ref } from "vue";
import api from "../api/client";

export interface PassthroughLog {
  id: number;
  host: string;
  path: string;
  method: string;
  model: string;
  status_code: number;
  is_error: boolean;
  error_message?: string;
  dns_ms: number | null;
  connect_ms: number | null;
  tls_ms: number | null;
  ttfb_ms: number | null;
  ttft_ms: number | null;
  total_ms: number;
  request_bytes: number;
  response_bytes: number;
  input_tokens: number | null;
  output_tokens: number | null;
  cache_write_5m_tokens: number | null;
  cache_write_1h_tokens: number | null;
  cache_read_tokens: number | null;
  started_at: string;
  created_at?: string;
}

export interface PassthroughSummary {
  total_requests: number;
  error_rate: number;
  requests_per_sec: number;
  avg_total_ms: number | null;
  p50_total_ms: number | null;
  p95_total_ms: number | null;
  p99_total_ms: number | null;
  avg_ttfb_ms: number | null;
  avg_ttft_ms: number | null;
  avg_dns_ms: number | null;
  avg_connect_ms: number | null;
  avg_tls_ms: number | null;
  avg_response_bytes: number;
  total_input_tokens: number | null;
  total_output_tokens: number | null;
  total_cache_write_5m_tokens: number | null;
  total_cache_write_1h_tokens: number | null;
  total_cache_read_tokens: number | null;
}

export interface PassthroughBucket {
  bucket: string;
  request_count: number;
  error_count: number;
  avg_total_ms: number | null;
  max_total_ms: number;
  avg_ttfb_ms: number | null;
  avg_response_bytes: number;
  input_tokens: number | null;
  output_tokens: number | null;
  cache_write_5m_tokens: number | null;
  cache_write_1h_tokens: number | null;
  cache_read_tokens: number | null;
}

export interface PassthroughHostStats {
  host: string;
  requests: number;
  errors: number;
  avg_total_ms: number | null;
  avg_ttfb_ms: number | null;
  avg_ttft_ms: number | null;
  avg_response_bytes: number;
  output_tokens: number | null;
}

export interface PassthroughPerformance {
  summary: PassthroughSummary;
  timeseries: PassthroughBucket[];
  by_host: PassthroughHostStats[];
}

export const usePassthroughStore = defineStore("passthrough", () => {
  const perf = ref<PassthroughPerformance | null>(null);
  const logs = ref<PassthroughLog[]>([]);
  const logTotal = ref(0);
  const loading = ref(false);
  const logsLoading = ref(false);
  const error = ref<string | null>(null);

  async function fetchPerformance(params?: Record<string, unknown>) {
    loading.value = true;
    error.value = null;
    try {
      const { data } = await api.get("/passthrough/performance", { params });
      perf.value = data as PassthroughPerformance;
    } catch (err: any) {
      error.value = err?.response?.data?.error || err?.message || "Failed to load relay metrics";
    } finally {
      loading.value = false;
    }
  }

  async function fetchLogs(params?: Record<string, unknown>) {
    logsLoading.value = true;
    try {
      const { data } = await api.get("/passthrough/logs", { params });
      logs.value = (data.data ?? []) as PassthroughLog[];
      logTotal.value = data.total ?? 0;
    } catch (err: any) {
      error.value = err?.response?.data?.error || err?.message || "Failed to load relay log";
    } finally {
      logsLoading.value = false;
    }
  }

  function clearError() {
    error.value = null;
  }

  return {
    perf,
    logs,
    logTotal,
    loading,
    logsLoading,
    error,
    fetchPerformance,
    fetchLogs,
    clearError,
  };
});
