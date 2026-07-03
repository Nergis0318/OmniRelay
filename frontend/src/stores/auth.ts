import { defineStore } from "pinia";
import { ref, computed } from "vue";
import api from "../api/client";

interface User {
  id: number;
  username: string;
  is_admin: boolean;
}

export const useAuthStore = defineStore("auth", () => {
  const token = ref(localStorage.getItem("token") || "");
  const user = ref<User | null>(null);
  const error = ref<string | null>(null);
  const loading = ref(false);

  const isLoggedIn = computed(() => !!token.value);

  function setSession(t: string, u: User) {
    token.value = t;
    user.value = u;
    localStorage.setItem("token", t);
  }

  function logout() {
    token.value = "";
    user.value = null;
    error.value = null;
    localStorage.removeItem("token");
  }

  async function login(email: string, password: string) {
    loading.value = true;
    error.value = null;
    try {
      const { data } = await api.post("/auth/login", { email, password });
      setSession(data.token, data.user);
    } catch (err: any) {
      error.value =
        err?.response?.data?.error || err?.message || "Login failed";
      throw err;
    } finally {
      loading.value = false;
    }
  }

  async function register(username: string, email: string, password: string) {
    loading.value = true;
    error.value = null;
    try {
      await api.post("/auth/register", { username, email, password });
      await login(email, password);
    } catch (err: any) {
      error.value =
        err?.response?.data?.error || err?.message || "Registration failed";
      throw err;
    } finally {
      loading.value = false;
    }
  }

  function clearError() {
    error.value = null;
  }

  return {
    token,
    user,
    error,
    loading,
    isLoggedIn,
    login,
    register,
    logout,
    clearError,
  };
});
