<template>
  <AuthShell :title="$t('auth.registerTitle')" :subtitle="$t('auth.registerSubtitle')">
    <form class="auth-form" @submit.prevent="handleRegister">
      <div class="field-group">
        <label class="field-label">{{ $t("auth.username") }}</label>
        <div class="field-wrap">
          <svg
            class="field-icon"
            width="16"
            height="16"
            viewBox="0 0 16 16"
            fill="none"
          >
            <circle
              cx="8"
              cy="5.5"
              r="2.5"
              stroke="currentColor"
              stroke-width="1.25"
            />
            <path
              d="M2.5 13c0-2.485 2.462-4.5 5.5-4.5s5.5 2.015 5.5 4.5"
              stroke="currentColor"
              stroke-width="1.25"
              stroke-linecap="round"
            />
          </svg>
          <input
            v-model="username"
            type="text"
            class="field-input"
            placeholder="admin"
            autocomplete="username"
            required
          />
        </div>
      </div>

      <div class="field-group">
        <label class="field-label">{{ $t("auth.email") }}</label>
        <div class="field-wrap">
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
        <div
          class="field-wrap"
          :class="{
            'field-wrap--mismatch':
              password && confirmPassword && password !== confirmPassword,
          }"
        >
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
            autocomplete="new-password"
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
        <span class="field-hint">{{
          $t("auth.passwordRequirements")
        }}</span>
      </div>

      <div class="field-group">
        <label class="field-label">{{ $t("auth.confirmPassword") }}</label>
        <div
          class="field-wrap"
          :class="{
            'field-wrap--mismatch':
              password && confirmPassword && password !== confirmPassword,
          }"
        >
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
            v-model="confirmPassword"
            :type="showPw ? 'text' : 'password'"
            class="field-input"
            placeholder="••••••••"
            autocomplete="new-password"
            required
          />
        </div>
        <span
          v-if="password && confirmPassword && password !== confirmPassword"
          class="field-hint-error"
          >{{ $t("auth.passwordsDontMatch") }}</span
        >
      </div>

      <div v-if="error" class="auth-error">
        <v-icon size="14">mdi-alert-circle-outline</v-icon>
        {{ error }}
      </div>

      <button
        type="submit"
        class="auth-submit"
        :class="{
          'auth-submit--loading': loading,
          'auth-submit--disabled': password !== confirmPassword,
        }"
        :disabled="password !== confirmPassword"
      >
        <span v-if="!loading">{{ $t("auth.createAccount") }}</span>
        <span v-else class="submit-spinner" />
      </button>
    </form>

    <template #footer>
      {{ $t("auth.haveAccount") }}
      <router-link to="/login">{{ $t("auth.signInLink") }}</router-link>
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
const username = ref("");
const email = ref("");
const password = ref("");
const confirmPassword = ref("");
const error = ref("");
const loading = ref(false);
const showPw = ref(false);

async function handleRegister() {
  if (password.value !== confirmPassword.value) return;
  loading.value = true;
  error.value = "";
  try {
    await auth.register(username.value, email.value, password.value);
    router.push("/");
  } catch (e: any) {
    error.value = e.response?.data?.error || t("auth.registrationFailed");
  } finally {
    loading.value = false;
  }
}
</script>
