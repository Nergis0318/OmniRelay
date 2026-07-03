<template>
  <!-- Desktop / Tablet sidebar -->
  <v-navigation-drawer
    v-if="!isMobile"
    v-model="drawer"
    :rail="rail"
    permanent
    width="220"
  >
    <!-- Brand -->
    <div class="brand-area" :class="{ 'brand-area--rail': rail }">
      <div class="brand-logo">
        <img :src="logoUrl" alt="OmniRelay" />
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
            :class="{ 'locale-btn--active': locale === 'ja' }"
            @click="switchLocale('ja')"
          >
            JA
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

  <!-- Mobile bottom tab bar -->
  <nav v-else class="mobile-tab-bar">
    <router-link
      v-for="item in menuItems"
      :key="item.to"
      :to="item.to"
      class="mobile-tab"
      :class="{ 'mobile-tab--active': isActive(item.to) }"
      :aria-current="isActive(item.to) ? 'page' : undefined"
    >
      <v-icon size="20">{{ item.icon }}</v-icon>
      <span class="mobile-tab__label">{{ $t(item.i18nKey) }}</span>
    </router-link>
    <button
      class="mobile-tab mobile-tab--logout"
      @click="handleLogout"
      :aria-label="$t('common.signOut')"
    >
      <v-icon size="20">mdi-logout</v-icon>
      <span class="mobile-tab__label">{{ $t("common.signOut") }}</span>
    </button>
  </nav>

  <v-main :class="{ 'main--mobile': isMobile }">
    <v-container fluid class="pa-8">
      <router-view />
    </v-container>
  </v-main>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from "vue";
import { useRouter, useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import { useAuthStore } from "../stores/auth";
import { setLocale } from "../plugins/i18n";
import logoUrl from "../assets/omnirelay-logo.svg";

const auth = useAuthStore();
const router = useRouter();
const route = useRoute();
const { locale } = useI18n();
const drawer = ref(true);
const rail = ref(false);

const MOBILE_BREAKPOINT = 768;
const isMobile = ref(false);
let mql: MediaQueryList | null = null;
function handleMQLChange(e: MediaQueryListEvent) {
  isMobile.value = e.matches;
}
onMounted(() => {
  mql = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT}px)`);
  isMobile.value = mql.matches;
  mql.addEventListener("change", handleMQLChange);
});
onUnmounted(() => {
  mql?.removeEventListener("change", handleMQLChange);
});

const menuItems = [
  { i18nKey: "nav.dashboard", icon: "mdi-view-dashboard-outline", to: "/" },
  { i18nKey: "nav.providers", icon: "mdi-server-outline", to: "/providers" },
  { i18nKey: "nav.models", icon: "mdi-cube-outline", to: "/models" },
  { i18nKey: "nav.apiKeys", icon: "mdi-key-outline", to: "/api-keys" },
  { i18nKey: "nav.usage", icon: "mdi-chart-line", to: "/usage" },
  { i18nKey: "nav.logs", icon: "mdi-text-box-search-outline", to: "/logs" },
  { i18nKey: "nav.users", icon: "mdi-account-group-outline", to: "/users" },
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
  justify-content: center;
  width: 32px;
  height: 32px;
}
.brand-logo img {
  display: block;
  width: 32px;
  height: 32px;
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

/* ── Mobile bottom tab bar ── */
.mobile-tab-bar {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  height: 56px;
  background: #131316;
  border-top: 1px solid rgba(255, 255, 255, 0.06);
  display: flex;
  align-items: center;
  justify-content: space-around;
  padding-bottom: env(safe-area-inset-bottom, 0px);
  z-index: 1000;
}
.mobile-tab {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  flex: 1;
  height: 100%;
  text-decoration: none;
  color: #4a4844;
  transition: color 0.15s;
  min-width: 0;
}
.mobile-tab--active {
  color: #e8a020;
}
.mobile-tab__label {
  font-family: "DM Sans", sans-serif;
  font-size: 0.6rem;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
}
.mobile-tab--logout {
  background: none;
  border: none;
  cursor: pointer;
}

.main--mobile .v-container {
  padding-bottom: calc(
    56px + env(safe-area-inset-bottom, 0px) + 16px
  ) !important;
}
</style>
