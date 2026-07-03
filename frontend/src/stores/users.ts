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

  function clearError() {
    error.value = null;
  }

  return { users, loading, error, fetch, clearError };
});
