import { defineStore } from "pinia";
import { ref } from "vue";
import api from "../api/client";

export interface User {
  id: number;
  username: string;
  email: string;
  is_admin: boolean;
  created_at: string;
}

export const useUsersStore = defineStore("users", () => {
  const users = ref<User[]>([]);
  const loading = ref(false);
  const error = ref<string | null>(null);

  async function fetch() {
    loading.value = true;
    error.value = null;
    try {
      const { data } = await api.get("/users");
      users.value = data.users;
    } catch (err: any) {
      error.value =
        err?.response?.data?.error || err?.message || "Failed to load users";
    } finally {
      loading.value = false;
    }
  }

  async function remove(id: number) {
    await api.delete(`/users/${id}`);
    users.value = users.value.filter((u) => u.id !== id);
  }

  async function setRole(id: number, isAdmin: boolean) {
    await api.put(`/users/${id}/role`, { is_admin: isAdmin });
    const u = users.value.find((u) => u.id === id);
    if (u) u.is_admin = isAdmin;
  }

  async function resetPassword(id: number): Promise<string> {
    const { data } = await api.post(`/users/${id}/reset-password`);
    return data.code;
  }

  async function getProviders(id: number): Promise<number[]> {
    const { data } = await api.get(`/users/${id}/providers`);
    return data.provider_ids;
  }

  async function setProviders(id: number, providerIds: number[]) {
    await api.put(`/users/${id}/providers`, { provider_ids: providerIds });
  }

  function clearError() {
    error.value = null;
  }

  return { users, loading, error, fetch, remove, setRole, resetPassword, getProviders, setProviders, clearError };
});
