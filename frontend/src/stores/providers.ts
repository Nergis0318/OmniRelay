import { defineStore } from "pinia";
import { ref } from "vue";
import api from "../api/client";

interface ProviderAPIKey {
  id: number;
  key_prefix: string;
  is_active: boolean;
  created_at: string;
}

interface Provider {
  id: number;
  provider_key: string;
  name: string;
  api_base_url: string;
  provider_type: string;
  is_active: boolean;
  show_in_model_list: boolean;
  source_models: string[];
  endpoints?: { api_type: string; base_url: string }[];
  api_keys?: ProviderAPIKey[];
  created_at: string;
}

interface SourceModel {
  model_id: string;
  display_name: string;
}

interface SourceModelGroup {
  provider_key: string;
  provider_type: string;
  name: string;
  models: SourceModel[];
}

interface CreateProviderPayload {
  provider_key: string;
  name: string;
  api_base_url?: string;
  api_key?: string;
  provider_type: string;
  source_models?: string[];
  endpoints?: { api_type: string; base_url: string }[];
  show_in_model_list?: boolean;
}

export const useProvidersStore = defineStore("providers", () => {
  const providers = ref<Provider[]>([]);
  const loading = ref(false);
  const error = ref<string | null>(null);
  const sourceModels = ref<SourceModelGroup[]>([]);

  async function fetch() {
    loading.value = true;
    error.value = null;
    try {
      const { data } = await api.get("/providers");
      providers.value = data.providers;
    } catch (err: any) {
      error.value =
        err?.response?.data?.error ||
        err?.message ||
        "Failed to load providers";
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
      error.value =
        err?.response?.data?.error ||
        err?.message ||
        "Failed to create provider";
      throw err;
    }
  }

  async function update(id: number, payload: any) {
    error.value = null;
    try {
      const { api_key: _apiKey, ...rest } = payload;
      await api.put(`/providers/${id}`, rest);
      await fetch();
    } catch (err: any) {
      error.value =
        err?.response?.data?.error ||
        err?.message ||
        "Failed to update provider";
      throw err;
    }
  }

  async function remove(id: number) {
    error.value = null;
    try {
      await api.delete(`/providers/${id}`);
      await fetch();
    } catch (err: any) {
      error.value =
        err?.response?.data?.error ||
        err?.message ||
        "Failed to delete provider";
      throw err;
    }
  }

  async function syncModels(id: number) {
    error.value = null;
    try {
      return await api.post(`/providers/${id}/sync`);
    } catch (err: any) {
      error.value =
        err?.response?.data?.error || err?.message || "Failed to sync models";
      throw err;
    }
  }

  async function testProvider(id: number) {
    const { data } = await api.post(`/providers/${id}/test`);
    return data as { ok: boolean; latency_ms: number; error?: string };
  }

  async function fetchSourceModels() {
    try {
      const { data } = await api.get("/models/source-list");
      sourceModels.value = data.providers;
    } catch (_err: any) {
      sourceModels.value = [];
    }
  }

  async function addKey(providerId: number, api_key: string) {
    error.value = null;
    try {
      await api.post(`/providers/${providerId}/keys`, { api_key });
      await fetch();
    } catch (err: any) {
      error.value =
        err?.response?.data?.error || err?.message || "Failed to add key";
      throw err;
    }
  }

  async function setKeyActive(
    providerId: number,
    keyId: number,
    is_active: boolean,
  ) {
    error.value = null;
    try {
      await api.patch(`/providers/${providerId}/keys/${keyId}`, { is_active });
      await fetch();
    } catch (err: any) {
      error.value =
        err?.response?.data?.error ||
        err?.message ||
        "Failed to update key";
      throw err;
    }
  }

  async function removeKey(providerId: number, keyId: number) {
    error.value = null;
    try {
      await api.delete(`/providers/${providerId}/keys/${keyId}`);
      await fetch();
    } catch (err: any) {
      error.value =
        err?.response?.data?.error || err?.message || "Failed to delete key";
      throw err;
    }
  }

  function clearError() {
    error.value = null;
  }

  return {
    providers,
    loading,
    error,
    sourceModels,
    fetch,
    fetchSourceModels,
    create,
    update,
    remove,
    addKey,
    setKeyActive,
    removeKey,
    syncModels,
    testProvider,
    clearError,
  };
});
