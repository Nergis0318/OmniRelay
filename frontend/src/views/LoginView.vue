<template>
  <AuthShell :title="$t('auth.loginTitle')" :subtitle="$t('auth.loginSubtitle')">
    <form class="auth-form" @submit.prevent="handleLogin">
      <div class="field-group">
        <label class="field-label">{{ $t("auth.email") }}</label>
        <div class="field-wrap" :class="{ 'field-wrap--error': !!error }">
          <svg
            class="field-icon"
            width="16"
            height="16"
            viewBox="0 0 16 16"
            fill="none"
          >
            <rect
              x="1.5"
              y="3.5"
              width="13"
              height="9"
              rx="1.5"
              stroke="currentColor"
              stroke-width="1.25"
            />
            <path
              d="M1.5 3.5l6.5 4.5 6.5-4.5"
              stroke="currentColor"
              stroke-width="1.25"
              stroke-linecap="round"
            />
          </svg>
          <input
            v-model="email"
            type="email"
            class="field-input"
            placeholder="admin@example.com"
            autocomplete="email"
            required
          />
        </div>
      </div>

      <div class="field-group">
        <label class="field-label">{{ $t("auth.password") }}</label>
        <div class="field-wrap" :class="{ 'field-wrap--error': !!error }">
          <svg
            class="field-icon"
            width="16"
            height="16"
            viewBox="0 0 16 16"
            fill="none"
          >
            <rect
              x="3.5"
              y="7"
              width="9"
              height="6.5"
              rx="1.25"
              stroke="currentColor"
              stroke-width="1.25"
            />
            <path
              d="M5.5 7V5.5a2.5 2.5 0 015 0V7"
              stroke="currentColor"
              stroke-width="1.25"
            />
            <circle cx="8" cy="10.25" r="1" fill="currentColor" />
          </svg>
          <input
            v-model="password"
            :type="showPw ? 'text' : 'password'"
            class="field-input"
            placeholder="••••••••"
            autocomplete="current-password"
            required
          />
          <button
            type="button"
            class="pw-toggle"
            @click="showPw = !showPw"
            tabindex="-1"
          >
            <v-icon size="15">{{
              showPw ? "mdi-eye-off-outline" : "mdi-eye-outline"
            }}</v-icon>
          </button>
        </div>
      </div>

      <div v-if="error" class="auth-error">
        <v-icon size="14">mdi-alert-circle-outline</v-icon>
        {{ error }}
      </div>

      <button
        type="submit"
        class="auth-submit"
        :class="{ 'auth-submit--loading': loading }"
      >
        <span v-if="!loading">{{ $t("auth.signIn") }}</span>
        <span v-else class="submit-spinner" />
      </button>
    </form>

    <template #footer>
      {{ $t("auth.noAccount") }}
      <router-link to="/register">{{ $t("auth.createOne") }}</router-link>
    </template>
  </AuthShell>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { useAuthStore } from "../stores/auth";
import AuthShell from "../components/AuthShell.vue";

const { t } = useI18n();
const auth = useAuthStore();
const router = useRouter();
const email = ref("");
const password = ref("");
const error = ref("");
const loading = ref(false);
const showPw = ref(false);

async function handleLogin() {
  loading.value = true;
  error.value = "";
  try {
    await auth.login(email.value, password.value);
    router.push("/");
  } catch (e: any) {
    error.value = e.response?.data?.error || t("auth.invalidCredentials");
  } finally {
    loading.value = false;
  }
}
</script>
