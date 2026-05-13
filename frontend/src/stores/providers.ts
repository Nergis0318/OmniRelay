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

  async function fetch() {
    loading.value = true;
    try {
      const { data } = await api.get("/providers");
      providers.value = data.providers;
    } finally {
      loading.value = false;
    }
  }

  async function create(payload: CreateProviderPayload) {
    const { data } = await api.post("/providers", payload);
    await fetch();
    return data.provider;
  }

  async function update(id: number, payload: any) {
    await api.put(`/providers/${id}`, payload);
    await fetch();
  }

  async function remove(id: number) {
    await api.delete(`/providers/${id}`);
    await fetch();
  }

  async function syncModels(id: number) {
    return await api.post(`/providers/${id}/sync`);
  }

  return { providers, loading, fetch, create, update, remove, syncModels };
});
