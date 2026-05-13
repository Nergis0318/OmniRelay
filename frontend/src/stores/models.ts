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

  async function fetch(providerKey?: string) {
    loading.value = true;
    try {
      const params = providerKey ? `?provider_key=${providerKey}` : "";
      const { data } = await api.get(`/models${params}`);
      models.value = data.models;
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
    await api.post("/models", payload);
    await fetch();
  }

  async function update(id: number, payload: any) {
    await api.put(`/models/${id}`, payload);
    await fetch();
  }

  async function remove(id: number) {
    await api.delete(`/models/${id}`);
    await fetch();
  }

  return { models, loading, fetch, create, update, remove };
});
