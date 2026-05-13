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

  const isLoggedIn = computed(() => !!token.value);

  function setSession(t: string, u: User) {
    token.value = t;
    user.value = u;
    localStorage.setItem("token", t);
  }

  function logout() {
    token.value = "";
    user.value = null;
    localStorage.removeItem("token");
  }

  async function login(username: string, password: string) {
    const { data } = await api.post("/auth/login", { username, password });
    setSession(data.token, data.user);
  }

  async function register(username: string, password: string) {
    await api.post("/auth/register", { username, password });
    await login(username, password);
  }

  return { token, user, isLoggedIn, login, register, logout };
});
