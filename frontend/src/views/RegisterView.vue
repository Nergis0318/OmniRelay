<template>
  <div class="auth-shell">
    <div class="auth-grid" aria-hidden="true" />
    <div class="auth-glow" aria-hidden="true" />

    <div class="auth-card-wrap">
      <div class="auth-brand">
        <img class="auth-brand__logo" :src="logoUrl" alt="OmniRelay" />
        <span class="auth-brand__name">OmniRelay</span>
      </div>

      <div class="auth-card">
        <div class="auth-card__header">
          <h1 class="auth-card__title">{{ $t("auth.registerTitle") }}</h1>
          <p class="auth-card__sub">{{ $t("auth.registerSubtitle") }}</p>
        </div>

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

        <div class="auth-footer">
          {{ $t("auth.haveAccount") }}
          <router-link to="/login">{{ $t("auth.signInLink") }}</router-link>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { useAuthStore } from "../stores/auth";
import logoUrl from "../assets/omnirelay-logo.svg";

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

<style scoped>
.auth-shell {
  min-height: 100vh;
  background: #0d0d0f;
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
  overflow: hidden;
}
.auth-grid {
  position: absolute;
  inset: 0;
  background-image: radial-gradient(
    circle,
    rgba(232, 160, 32, 0.12) 1px,
    transparent 1px
  );
  background-size: 28px 28px;
  opacity: 0.5;
  mask-image: radial-gradient(ellipse 70% 70% at 50% 50%, black, transparent);
}
.auth-glow {
  position: absolute;
  top: 35%;
  left: 50%;
  transform: translate(-50%, -50%);
  width: 600px;
  height: 400px;
  background: radial-gradient(
    ellipse at center,
    rgba(232, 160, 32, 0.07) 0%,
    transparent 70%
  );
  pointer-events: none;
}
.auth-card-wrap {
  position: relative;
  z-index: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 28px;
  width: 100%;
  max-width: 400px;
  padding: 24px;
  animation: fadeUp 0.5s ease;
}
@keyframes fadeUp {
  from {
    opacity: 0;
    transform: translateY(16px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
.auth-brand {
  display: flex;
  align-items: center;
  gap: 10px;
}
.auth-brand__logo {
  display: block;
  width: 40px;
  height: 40px;
}
.auth-brand__name {
  font-family: "Fraunces", Georgia, serif;
  font-size: 1.25rem;
  font-weight: 600;
  font-style: italic;
  color: #e8e6e1;
  letter-spacing: -0.025em;
}
.auth-card {
  width: 100%;
  background: #131316;
  border: 1px solid rgba(232, 160, 32, 0.2);
  border-radius: 16px;
  padding: 32px;
  box-shadow:
    0 32px 80px rgba(0, 0, 0, 0.6),
    0 0 60px rgba(232, 160, 32, 0.04);
}
.auth-card__header {
  margin-bottom: 28px;
}
.auth-card__title {
  font-family: "Fraunces", Georgia, serif;
  font-size: 1.5rem;
  font-weight: 600;
  color: #e8e6e1;
  letter-spacing: -0.025em;
  margin: 0 0 6px;
}
.auth-card__sub {
  font-family: "DM Sans", sans-serif;
  font-size: 0.875rem;
  color: #7c7a75;
  margin: 0;
}
.auth-form {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.field-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.field-label {
  font-family: "DM Sans", sans-serif;
  font-size: 0.78rem;
  font-weight: 500;
  color: #7c7a75;
  letter-spacing: 0.05em;
  text-transform: uppercase;
}
.field-wrap {
  display: flex;
  align-items: center;
  gap: 10px;
  background: #1a1a1f;
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 9px;
  padding: 0 12px;
  transition:
    border-color 0.15s,
    box-shadow 0.15s;
}
.field-wrap:focus-within {
  border-color: rgba(232, 160, 32, 0.5);
  box-shadow: 0 0 0 3px rgba(232, 160, 32, 0.08);
}
.field-wrap--mismatch {
  border-color: rgba(255, 87, 87, 0.4) !important;
}
.field-icon {
  color: #4a4844;
  flex-shrink: 0;
  transition: color 0.15s;
}
.field-wrap:focus-within .field-icon {
  color: #e8a020;
}
.field-input {
  flex: 1;
  background: transparent;
  border: none;
  outline: none;
  color: #e8e6e1;
  font-family: "DM Sans", sans-serif;
  font-size: 0.9rem;
  padding: 12px 0;
  min-width: 0;
}
.field-input::placeholder {
  color: #4a4844;
}
.field-hint-error {
  font-family: "DM Sans", sans-serif;
  font-size: 0.78rem;
  color: #ff5757;
  padding-left: 4px;
}
.field-hint {
  font-family: "DM Sans", sans-serif;
  font-size: 0.78rem;
  color: #4a4844;
  padding-left: 4px;
}
.pw-toggle {
  background: none;
  border: none;
  cursor: pointer;
  color: #4a4844;
  padding: 4px;
  display: flex;
  align-items: center;
  transition: color 0.15s;
}
.pw-toggle:hover {
  color: #e8a020;
}
.auth-error {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 12px;
  background: rgba(255, 87, 87, 0.08);
  border: 1px solid rgba(255, 87, 87, 0.2);
  border-radius: 8px;
  font-family: "DM Sans", sans-serif;
  font-size: 0.825rem;
  color: #ff5757;
}
.auth-submit {
  width: 100%;
  padding: 13px;
  background: #e8a020;
  color: #0d0d0f;
  border: none;
  border-radius: 9px;
  font-family: "DM Sans", sans-serif;
  font-size: 0.9rem;
  font-weight: 600;
  cursor: pointer;
  transition:
    background 0.15s,
    box-shadow 0.15s,
    transform 0.1s;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-top: 4px;
  letter-spacing: 0.01em;
}
.auth-submit:hover:not(.auth-submit--disabled) {
  background: #f5c842;
  box-shadow: 0 0 24px rgba(232, 160, 32, 0.35);
}
.auth-submit:active {
  transform: scale(0.985);
}
.auth-submit--disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.auth-submit--loading {
  opacity: 0.7;
  cursor: not-allowed;
}
.submit-spinner {
  width: 16px;
  height: 16px;
  border: 2px solid rgba(13, 13, 15, 0.3);
  border-top-color: #0d0d0f;
  border-radius: 50%;
  animation: spin 0.7s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
.auth-footer {
  margin-top: 20px;
  text-align: center;
  font-family: "DM Sans", sans-serif;
  font-size: 0.825rem;
  color: #7c7a75;
}
.auth-footer a {
  color: #e8a020;
  font-weight: 500;
  text-decoration: none;
}
.auth-footer a:hover {
  color: #f5c842;
  text-decoration: underline;
}

@media (max-width: 480px) {
  .auth-card-wrap {
    padding: 16px;
    max-width: 100%;
  }
  .auth-card {
    border-radius: 12px;
    padding: 24px;
  }
  .auth-card__title {
    font-size: 1.3rem;
  }
  .field-input {
    font-size: 16px;
  }
}
</style>
