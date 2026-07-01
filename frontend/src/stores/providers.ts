import { defineStore } from "pinia";
import { ref } from "vue";
import api from "../api/client";

interface Provider {
  id: number;
  provider_key: string;
  name: string;
  api_base_url: string;
  provider_type: string;
  is_active: boolean;
  created_at: string;
}

interface CreateProviderPayload {
  provider_key: string;
  name: string;
  api_base_url: string;
  api_key: string;
  provider_type: string;
}

export const useProvidersStore = defineStore("providers", () => {
  const providers = ref<Provider[]>([]);
  const loading = ref(false);
  const error = ref<string | null>(null);

  async function fetch() {
    loading.value = true;
    error.value = null;
    try {
      const { data } = await api.get("/providers");
      providers.value = data.providers;
    } catch (err: any) {
      error.value = err?.response?.data?.error || err?.message || "Failed to load providers";
    } finally {
      loading.value = false;
    }
  }

  async function create(payload: CreateProviderPayload) {
    error.value = null;
    try {
      const { data } = await api.post("/providers", payload);
      await fetch();
      return data.provider;
    } catch (err: any) {
      error.value = err?.response?.data?.error || err?.message || "Failed to create provider";
      throw err;
    }
  }

  async function update(id: number, payload: any) {
    error.value = null;
    try {
      await api.put(`/providers/${id}`, payload);
      await fetch();
    } catch (err: any) {
      error.value = err?.response?.data?.error || err?.message || "Failed to update provider";
      throw err;
    }
  }

  async function remove(id: number) {
    error.value = null;
    try {
      await api.delete(`/providers/${id}`);
      await fetch();
    } catch (err: any) {
      error.value = err?.response?.data?.error || err?.message || "Failed to delete provider";
      throw err;
    }
  }

  async function syncModels(id: number) {
    error.value = null;
    try {
      return await api.post(`/providers/${id}/sync`);
    } catch (err: any) {
      error.value = err?.response?.data?.error || err?.message || "Failed to sync models";
      throw err;
    }
  }

  function clearError() {
    error.value = null;
  }

  return { providers, loading, error, fetch, create, update, remove, syncModels, clearError };
});
