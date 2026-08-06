import { defineStore } from "pinia";
import { ref } from "vue";
import api from "../api/client";

export interface PerformanceSummary {
  total_requests: number;
  rpm: number;
  tpm: number;
  avg_latency_ms: number;
  p50_ms: number;
  p95_ms: number;
  p99_ms: number;
  avg_ttft_ms: number | null;
  ttft_count: number;
  error_rate: number;
  cache_hit_rate: number;
}

export interface PerformanceBucket {
  bucket: string;
  request_count: number;
  rpm: number;
  tpm: number;
  avg_latency_ms: number;
  avg_ttft_ms: number | null;
  error_count: number;
}

export interface PerformanceBreakdown {
  provider_id: number | null;
  provider_name: string;
  model: string;
  requests: number;
  tokens: number;
  avg_latency_ms: number;
  cost: number;
}

export interface PerformanceData {
  summary: PerformanceSummary;
  timeseries: PerformanceBucket[];
  by_provider: PerformanceBreakdown[];
  by_model: PerformanceBreakdown[];
  top_models_by_cost: PerformanceBreakdown[];
}

export const usePerformanceStore = defineStore("performance", () => {
  const data = ref<PerformanceData | null>(null);
  const loading = ref(false);
  const error = ref<string | null>(null);

  async function fetchPerformance(params?: Record<string, any>) {
    loading.value = true;
    error.value = null;
    try {
      const { data: res } = await api.get("/performance", { params });
      data.value = res as PerformanceData;
    } catch (err: any) {
      error.value = err?.response?.data?.error || err?.message || "Failed to load performance";
    } finally {
      loading.value = false;
    }
  }

  function clearError() {
    error.value = null;
  }

  return { data, loading, error, fetchPerformance, clearError };
});
