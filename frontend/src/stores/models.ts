import { defineStore } from "pinia";
import { ref } from "vue";
import api from "../api/client";

interface Model {
  id: number;
  provider_id: number;
  model_id: string;
  display_name: string;
  provider_key: string;
  is_manual: boolean;
  input_price_per_1mtok: number;
  output_price_per_1mtok: number;
  cache_write_5m_price_per_1mtok: number;
  cache_write_1h_price_per_1mtok: number;
  cache_read_price_per_1mtok: number;
  context_window: number;
  created_at: string;
}

export const useModelsStore = defineStore("models", () => {
  const models = ref<Model[]>([]);
  const loading = ref(false);
  const error = ref<string | null>(null);

  async function fetch(providerKey?: string) {
    loading.value = true;
    error.value = null;
    try {
      const params = providerKey ? `?provider_key=${providerKey}` : "";
      const { data } = await api.get(`/models${params}`);
      models.value = data.models;
    } catch (err: any) {
      error.value = err?.response?.data?.error || err?.message || "Failed to load models";
    } finally {
      loading.value = false;
    }
  }

  async function create(payload: {
    model_id: string;
    display_name: string;
    provider_id: number;
    input_price_per_1mtok: number;
    output_price_per_1mtok: number;
    cache_write_5m_price_per_1mtok: number;
    cache_write_1h_price_per_1mtok: number;
    cache_read_price_per_1mtok: number;
    context_window: number;
  }) {
    error.value = null;
    try {
      await api.post("/models", payload);
      await fetch();
    } catch (err: any) {
      error.value = err?.response?.data?.error || err?.message || "Failed to create model";
      throw err;
    }
  }

  async function update(id: number, payload: any) {
    error.value = null;
    try {
      await api.put(`/models/${id}`, payload);
      await fetch();
    } catch (err: any) {
      error.value = err?.response?.data?.error || err?.message || "Failed to update model";
      throw err;
    }
  }

  async function remove(id: number) {
    error.value = null;
    try {
      await api.delete(`/models/${id}`);
      await fetch();
    } catch (err: any) {
      error.value = err?.response?.data?.error || err?.message || "Failed to delete model";
      throw err;
    }
  }

  function clearError() {
    error.value = null;
  }

  return { models, loading, error, fetch, create, update, remove, clearError };
});
