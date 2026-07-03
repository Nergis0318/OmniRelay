import { defineStore } from "pinia";
import { ref } from "vue";
import api from "../api/client";

interface APIKey {
  id: number;
  key_prefix: string;
  name: string;
  is_active: boolean;
  rate_limit_rpm: number;
  total_token_limit: number;
  created_at: string;
  last_used_at: string | null;
}

export const useApiKeysStore = defineStore("apikeys", () => {
  const apiKeys = ref<APIKey[]>([]);
  const loading = ref(false);
  const error = ref<string | null>(null);

  async function fetch() {
    loading.value = true;
    error.value = null;
    try {
      const { data } = await api.get("/api-keys");
      apiKeys.value = data.api_keys;
    } catch (err: any) {
      error.value =
        err?.response?.data?.error || err?.message || "Failed to load API keys";
    } finally {
      loading.value = false;
    }
  }

  async function create(
    name: string,
    rateLimitRpm: number,
    totalTokenLimit: number = 0,
  ) {
    error.value = null;
    try {
      const { data } = await api.post("/api-keys", {
        name,
        rate_limit_rpm: rateLimitRpm,
        total_token_limit: totalTokenLimit,
      });
      await fetch();
      return data;
    } catch (err: any) {
      error.value =
        err?.response?.data?.error ||
        err?.message ||
        "Failed to create API key";
      throw err;
    }
  }

  async function remove(id: number) {
    error.value = null;
    try {
      await api.delete(`/api-keys/${id}`);
      await fetch();
    } catch (err: any) {
      error.value =
        err?.response?.data?.error ||
        err?.message ||
        "Failed to delete API key";
      throw err;
    }
  }

  function clearError() {
    error.value = null;
  }

  return { apiKeys, loading, error, fetch, create, remove, clearError };
});
