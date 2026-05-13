<template>
  <v-navigation-drawer v-model="drawer" :rail="rail" permanent width="220">
    <!-- Brand -->
    <div class="brand-area" :class="{ 'brand-area--rail': rail }">
      <div class="brand-logo">
        <svg width="28" height="28" viewBox="0 0 28 28" fill="none">
          <polygon
            points="14,2 26,8 26,20 14,26 2,20 2,8"
            stroke="#E8A020"
            stroke-width="1.5"
            fill="rgba(232,160,32,0.06)"
          />
          <circle cx="14" cy="14" r="3.5" fill="#E8A020" />
          <line
            x1="14"
            y1="5"
            x2="14"
            y2="11"
            stroke="#E8A020"
            stroke-width="1"
            opacity="0.5"
          />
          <line
            x1="14"
            y1="17"
            x2="14"
            y2="23"
            stroke="#E8A020"
            stroke-width="1"
            opacity="0.5"
          />
          <line
            x1="5"
            y1="9.5"
            x2="10.5"
            y2="12.5"
            stroke="#E8A020"
            stroke-width="1"
            opacity="0.5"
          />
          <line
            x1="17.5"
            y1="15.5"
            x2="23"
            y2="18.5"
            stroke="#E8A020"
            stroke-width="1"
            opacity="0.5"
          />
          <line
            x1="5"
            y1="18.5"
            x2="10.5"
            y2="15.5"
            stroke="#E8A020"
            stroke-width="1"
            opacity="0.5"
          />
          <line
            x1="17.5"
            y1="12.5"
            x2="23"
            y2="9.5"
            stroke="#E8A020"
            stroke-width="1"
            opacity="0.5"
          />
        </svg>
      </div>
      <transition name="fade">
        <div v-if="!rail" class="brand-text">
          <div class="brand-name">OmniRelay</div>
          <div class="brand-sub">
            {{ auth.user?.username || $t("nav.gateway") }}
          </div>
        </div>
      </transition>
      <v-btn
        class="rail-toggle"
        :icon="rail ? 'mdi-chevron-right' : 'mdi-chevron-left'"
        variant="text"
        size="small"
        @click.stop="rail = !rail"
      />
    </div>

    <div class="nav-divider" />

    <!-- Nav items -->
    <nav class="nav-list">
      <router-link
        v-for="item in menuItems"
        :key="item.to"
        :to="item.to"
        class="nav-item"
        :class="{ 'nav-item--active': isActive(item.to) }"
        :title="rail ? $t(item.i18nKey) : undefined"
      >
        <v-icon size="18" class="nav-item__icon">{{ item.icon }}</v-icon>
        <transition name="fade">
          <span v-if="!rail" class="nav-item__label">{{
            $t(item.i18nKey)
          }}</span>
        </transition>
        <transition name="fade">
          <span v-if="!rail && isActive(item.to)" class="nav-item__dot" />
        </transition>
      </router-link>
    </nav>

    <!-- Footer -->
    <template #append>
      <div class="nav-footer" :class="{ 'nav-footer--rail': rail }">
        <!-- Language switcher -->
        <div class="nav-divider" />
        <div class="locale-switch" :class="{ 'locale-switch--rail': rail }">
          <button
            class="locale-btn"
            :class="{ 'locale-btn--active': locale === 'en' }"
            @click="switchLocale('en')"
          >
            EN
          </button>
          <button
            class="locale-btn"
            :class="{ 'locale-btn--active': locale === 'ko' }"
            @click="switchLocale('ko')"
          >
            KO
          </button>
        </div>
        <div class="nav-divider" />
        <button
          class="logout-btn"
          :class="{ 'logout-btn--rail': rail }"
          @click="handleLogout"
        >
          <v-icon size="16">mdi-logout</v-icon>
          <transition name="fade">
            <span v-if="!rail">{{ $t("common.signOut") }}</span>
          </transition>
        </button>
      </div>
    </template>
  </v-navigation-drawer>

  <v-main>
    <v-container fluid class="pa-8">
      <router-view />
    </v-container>
  </v-main>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { useRouter, useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import { useAuthStore } from "../stores/auth";
import { setLocale } from "../plugins/i18n";

const auth = useAuthStore();
const router = useRouter();
const route = useRoute();
const { locale } = useI18n();
const drawer = ref(true);
const rail = ref(false);

const menuItems = [
  { i18nKey: "nav.dashboard", icon: "mdi-view-dashboard-outline", to: "/" },
  { i18nKey: "nav.providers", icon: "mdi-server-outline", to: "/providers" },
  { i18nKey: "nav.models", icon: "mdi-cube-outline", to: "/models" },
  { i18nKey: "nav.apiKeys", icon: "mdi-key-outline", to: "/api-keys" },
  { i18nKey: "nav.usage", icon: "mdi-chart-line", to: "/usage" },
  { i18nKey: "nav.logs", icon: "mdi-text-box-search-outline", to: "/logs" },
];

function isActive(to: string) {
  if (to === "/") return route.path === "/";
  return route.path.startsWith(to);
}

function switchLocale(loc: string) {
  setLocale(loc);
}

function handleLogout() {
  auth.logout();
  router.push("/login");
}
</script>

<style scoped>
/* ── Brand ── */
.brand-area {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 18px 14px 14px;
  position: relative;
}
.brand-area--rail {
  padding: 18px 10px 14px;
  justify-content: center;
}

.brand-logo {
  flex-shrink: 0;
  display: flex;
  align-items: center;
}

.brand-text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
}
.brand-name {
  font-family: "Fraunces", Georgia, serif;
  font-size: 1rem;
  font-weight: 600;
  font-style: italic;
  color: #e8e6e1;
  white-space: nowrap;
  letter-spacing: -0.02em;
}
.brand-sub {
  font-family: "DM Sans", sans-serif;
  font-size: 0.7rem;
  color: #7c7a75;
  white-space: nowrap;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  margin-top: 1px;
}

.rail-toggle {
  flex-shrink: 0;
  color: #4a4844 !important;
  margin-left: auto;
}
.rail-toggle:hover {
  color: #e8a020 !important;
}

/* ── Divider ── */
.nav-divider {
  height: 1px;
  background: rgba(255, 255, 255, 0.06);
  margin: 0 14px;
}

/* ── Nav list ── */
.nav-list {
  padding: 12px 8px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 10px;
  border-radius: 7px;
  text-decoration: none;
  color: #7c7a75;
  font-family: "DM Sans", sans-serif;
  font-size: 0.86rem;
  font-weight: 400;
  transition: all 0.15s ease;
  position: relative;
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
}
.nav-item:hover {
  background: rgba(255, 255, 255, 0.04);
  color: #e8e6e1;
}
.nav-item--active {
  background: rgba(232, 160, 32, 0.09) !important;
  color: #e8a020 !important;
  font-weight: 500;
  border-left: 2px solid #e8a020;
  padding-left: 8px;
}
.nav-item__icon {
  flex-shrink: 0;
  opacity: 0.75;
  transition: opacity 0.15s;
}
.nav-item--active .nav-item__icon {
  opacity: 1;
  color: #e8a020 !important;
}
.nav-item__label {
  flex: 1;
}
.nav-item__dot {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: #e8a020;
  flex-shrink: 0;
  opacity: 0.6;
}

/* ── Footer ── */
.nav-footer {
  padding: 8px 8px 14px;
}
.nav-footer--rail {
  padding: 8px 4px 14px;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.logout-btn {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 9px 10px;
  border-radius: 7px;
  background: transparent;
  border: none;
  cursor: pointer;
  color: #7c7a75;
  font-family: "DM Sans", sans-serif;
  font-size: 0.86rem;
  transition: all 0.15s ease;
  margin-top: 6px;
  white-space: nowrap;
  overflow: hidden;
}
.logout-btn:hover {
  background: rgba(255, 87, 87, 0.08);
  color: #ff5757;
}
.logout-btn--rail {
  width: 36px;
  padding: 9px;
  justify-content: center;
}

.locale-switch {
  display: flex;
  gap: 2px;
  padding: 8px 8px;
}
.locale-switch--rail {
  flex-direction: column;
  padding: 6px 4px;
}
.locale-btn {
  flex: 1;
  padding: 5px 0;
  background: transparent;
  border: 1px solid rgba(255, 255, 255, 0.06);
  border-radius: 5px;
  cursor: pointer;
  color: #4a4844;
  font-family: "DM Sans", sans-serif;
  font-size: 0.68rem;
  font-weight: 500;
  letter-spacing: 0.06em;
  transition: all 0.15s ease;
}
.locale-btn:hover {
  border-color: rgba(232, 160, 32, 0.25);
  color: #e8a020;
}
.locale-btn--active {
  background: rgba(232, 160, 32, 0.1);
  border-color: rgba(232, 160, 32, 0.3);
  color: #e8a020;
}
.locale-switch--rail .locale-btn {
  width: 32px;
  padding: 4px 0;
}

/* ── Transitions ── */
.fade-enter-active,
.fade-leave-active {
  transition:
    opacity 0.15s ease,
    transform 0.15s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateX(-4px);
}

/* ── Main content ── */
:deep(.v-navigation-drawer) {
  background: #131316 !important;
  border-right: 1px solid rgba(255, 255, 255, 0.06) !important;
}
</style>
