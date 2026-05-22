# OmniRelay Frontend Mobile Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make all OmniRelay dashboard views and auth pages responsive down to 320px wide using CSS + mobile card UI for tables, with a bottom tab bar replacing the sidebar on mobile.

**Architecture:** Add a shared `MobileDataCard.vue` component for table-to-card rendering, wrap all data tables with conditional mobile card lists using CSS `@media` display toggles, and replace the sidebar navigation with a fixed bottom tab bar on mobile via `DefaultLayout.vue` viewport detection.

**Tech Stack:** Vue 3 + Vuetify 3, custom CSS, no new dependencies.

---

## Task 1: Global Mobile CSS Utilities

**Files:**
- Modify: `frontend/src/styles/page-shared.css`

- [ ] **Step 1: Add mobile padding and typography rules**

Append at the end of `page-shared.css`:

```css
/* ── Mobile utilities ── */
@media (max-width: 768px) {
  .page {
    gap: 16px;
  }
  .page-header {
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
  }
  .page-title {
    font-size: 1.25rem;
  }
  .table-card {
    border-radius: 10px;
  }
  .filter-bar {
    padding: 12px 14px;
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
  }
  .filter-col {
    min-width: 0;
  }
  .filter-submit {
    align-self: stretch;
  }
  .dialog-card {
    border-radius: 0;
    border-left: none;
    border-right: none;
  }
  .dialog-header {
    padding: 16px 16px 10px;
  }
  .dialog-body {
    padding: 14px 16px;
    gap: 12px;
  }
  .dialog-footer {
    padding: 10px 16px 16px;
  }
  .field-input,
  .field-select {
    font-size: 16px; /* prevent iOS zoom */
  }
}

@media (max-width: 768px) {
  pre,
  code {
    overflow-x: auto;
    white-space: pre;
    word-break: normal;
  }
}
```

- [ ] **Step 2: Verify no syntax errors**

Run: `cd /root/OmniRelay/frontend && bun run build`
Expected: Build completes without errors (may have existing warnings).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/styles/page-shared.css
git commit -m "feat: add global mobile CSS utilities"
```

---

## Task 2: Create MobileDataCard Component

**Files:**
- Create: `frontend/src/components/MobileDataCard.vue`

- [ ] **Step 1: Write the component**

Create `frontend/src/components/MobileDataCard.vue`:

```vue
<template>
  <div class="mobile-card">
    <div v-for="(item, idx) in items" :key="idx" class="mobile-card__row">
      <span class="mobile-card__label">{{ item.label }}</span>
      <span class="mobile-card__value">{{ item.value }}</span>
    </div>
    <div v-if="$slots.actions" class="mobile-card__actions">
      <slot name="actions" />
    </div>
  </div>
</template>

<script setup lang="ts">
interface CardItem {
  label: string;
  value: string;
}

defineProps<{
  items: CardItem[];
}>();
</script>

<style scoped>
.mobile-card {
  background: #131316;
  border: 1px solid rgba(255, 255, 255, 0.06);
  border-radius: 12px;
  padding: 16px;
  margin-bottom: 12px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}
.mobile-card__row {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  padding: 8px 0;
  border-bottom: 1px solid rgba(255, 255, 255, 0.04);
}
.mobile-card__row:last-child {
  border-bottom: none;
}
.mobile-card__label {
  font-family: "DM Sans", sans-serif;
  font-size: 0.75rem;
  font-weight: 500;
  color: #7c7a75;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  flex-shrink: 0;
}
.mobile-card__value {
  font-family: "JetBrains Mono", monospace;
  font-size: 0.82rem;
  color: #e8e6e1;
  text-align: right;
  word-break: break-word;
}
.mobile-card__actions {
  display: flex;
  justify-content: flex-end;
  gap: 6px;
  padding-top: 8px;
  margin-top: 4px;
  border-top: 1px solid rgba(255, 255, 255, 0.04);
}
</style>
```

- [ ] **Step 2: Run build to verify component compiles**

Run: `cd /root/OmniRelay/frontend && bun run build`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/MobileDataCard.vue
git commit -m "feat: add MobileDataCard component for mobile table views"
```

---

## Task 3: DefaultLayout Mobile Navigation

**Files:**
- Modify: `frontend/src/layouts/DefaultLayout.vue`

- [ ] **Step 1: Add mobile detection and bottom tab bar markup**

Replace the entire `<template>` block in `DefaultLayout.vue`:

```vue
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
    >
      <v-icon size="20">{{ item.icon }}</v-icon>
      <span class="mobile-tab__label">{{ $t(item.i18nKey) }}</span>
    </router-link>
    <button class="mobile-tab mobile-tab--logout" @click="handleLogout">
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
```

- [ ] **Step 2: Add mobile detection to script**

In the `<script setup>` section, after the existing imports and before `menuItems`, add:

```typescript
import { onMounted, onUnmounted } from "vue";

const isMobile = ref(false);
function checkMobile() {
  isMobile.value = window.innerWidth <= 768;
}
onMounted(() => {
  checkMobile();
  window.addEventListener("resize", checkMobile);
});
onUnmounted(() => {
  window.removeEventListener("resize", checkMobile);
});
```

- [ ] **Step 3: Add mobile tab bar styles**

Append to the `<style scoped>` block at the end:

```css
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
  padding-bottom: calc(56px + env(safe-area-inset-bottom, 0px) + 16px) !important;
}
```

- [ ] **Step 4: Run build**

Run: `cd /root/OmniRelay/frontend && bun run build`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/layouts/DefaultLayout.vue
git commit -m "feat: add mobile bottom tab bar and responsive layout"
```

---

## Task 4: DashboardView Mobile

**Files:**
- Modify: `frontend/src/views/DashboardView.vue`

- [ ] **Step 1: Add 480px breakpoint and chart height adjustment**

In the `<style scoped>` block, after the existing `@media (max-width: 560px)` block, add:

```css
@media (max-width: 480px) {
  .dash-title {
    font-size: 1.35rem;
  }
  .stat-card {
    padding: 14px 16px 10px;
  }
  .stat-card__value {
    font-size: 1.2rem;
  }
  .chart-wrap {
    height: 220px;
  }
  .charts-row {
    gap: 10px;
  }
}
```

- [ ] **Step 2: Reduce gaps for mobile**

Add inside the existing `@media (max-width: 560px)` block:

```css
@media (max-width: 560px) {
  .stat-grid {
    grid-template-columns: 1fr;
    gap: 10px;
  }
  .dash {
    gap: 18px;
  }
}
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/views/DashboardView.vue
git commit -m "feat: improve DashboardView mobile responsiveness"
```

---

## Task 5: ProvidersView Mobile

**Files:**
- Modify: `frontend/src/views/ProvidersView.vue`

- [ ] **Step 1: Add mobile card list alongside table**

After the `v-data-table` closing `</div>` (line 67, before the dialog), insert:

```vue
    <!-- Mobile cards -->
    <div class="mobile-cards">
      <MobileDataCard
        v-for="p in store.providers"
        :key="p.id"
        :items="[
          { label: $t('providers.key'), value: p.provider_key },
          { label: $t('providers.name'), value: p.name },
          { label: $t('providers.type'), value: p.provider_type },
          { label: $t('providers.status'), value: p.is_active ? $t('providers.active') : $t('providers.inactive') },
        ]"
      >
        <template #actions>
          <button class="row-btn" title="Edit" @click="openDialog(p)">
            <v-icon size="15">mdi-pencil-outline</v-icon>
          </button>
          <button class="row-btn" title="Sync Models" @click="handleSync(p.id)">
            <v-icon size="15">mdi-sync</v-icon>
          </button>
          <button class="row-btn row-btn--danger" title="Delete" @click="handleDelete(p.id)">
            <v-icon size="15">mdi-delete-outline</v-icon>
          </button>
        </template>
      </MobileDataCard>
      <div v-if="!store.providers.length" class="empty-state">
        <v-icon size="32" color="#4a4844">mdi-server-off</v-icon>
        <p>{{ $t("providers.noProviders") }}</p>
      </div>
    </div>
```

- [ ] **Step 2: Add dialog fullscreen for mobile**

Change the dialog opening line:

```vue
    <v-dialog v-model="dialog" :max-width="isMobile ? undefined : 520" :fullscreen="isMobile">
```

Add to `<script setup>`:

```typescript
import { ref, onMounted, onUnmounted } from "vue";

const isMobile = ref(false);
function checkMobile() {
  isMobile.value = window.innerWidth <= 768;
}
onMounted(() => {
  checkMobile();
  window.addEventListener("resize", checkMobile);
  store.fetch();
});
onUnmounted(() => {
  window.removeEventListener("resize", checkMobile);
});
```

Remove the old `onMounted(() => store.fetch());` line.

- [ ] **Step 3: Import MobileDataCard**

Add to the `<script setup>` imports:

```typescript
import MobileDataCard from "../components/MobileDataCard.vue";
```

- [ ] **Step 4: Add mobile toggle CSS**

In the `<style scoped>` block, add:

```css
.mobile-cards {
  display: none;
}
@media (max-width: 768px) {
  .v-data-table {
    display: none;
  }
  .mobile-cards {
    display: block;
  }
}
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/views/ProvidersView.vue
git commit -m "feat: make ProvidersView responsive with mobile cards"
```

---

## Task 6: ModelsView Mobile

**Files:**
- Modify: `frontend/src/views/ModelsView.vue`

- [ ] **Step 1: Add mobile card list alongside table**

After the `v-data-table` closing `</div>` (line 88, before the dialog), insert:

```vue
    <!-- Mobile cards -->
    <div class="mobile-cards">
      <MobileDataCard
        v-for="m in store.models"
        :key="m.id"
        :items="[
          { label: $t('models.model'), value: m.provider_key + '/' + m.model_id },
          { label: $t('models.provider'), value: m.provider_key },
          { label: $t('models.source'), value: m.is_manual ? $t('models.manual') : $t('models.auto') },
          { label: $t('models.pricing'), value: '$' + (m.input_price_per_1mtok ?? 0) + ' / $' + (m.output_price_per_1mtok ?? 0) },
          { label: $t('models.context'), value: m.context_window ? (m.context_window / 1000).toFixed(0) + 'k' : '—' },
        ]"
      >
        <template #actions>
          <button class="row-btn" title="Edit" @click="openEditDialog(m)">
            <v-icon size="15">mdi-pencil-outline</v-icon>
          </button>
          <button class="row-btn row-btn--danger" title="Delete" @click="handleDelete(m.id)">
            <v-icon size="15">mdi-delete-outline</v-icon>
          </button>
        </template>
      </MobileDataCard>
      <div v-if="!store.models.length" class="empty-state">
        <v-icon size="32" color="#4a4844">mdi-cube-off-outline</v-icon>
        <p>{{ $t("models.noModels") }}</p>
      </div>
    </div>
```

- [ ] **Step 2: Add dialog fullscreen for mobile**

Change the dialog opening line:

```vue
    <v-dialog v-model="dialog" :max-width="isMobile ? undefined : 500" :fullscreen="isMobile">
```

Add to `<script setup>`:

```typescript
import { ref, onMounted, onUnmounted } from "vue";
import MobileDataCard from "../components/MobileDataCard.vue";

const isMobile = ref(false);
function checkMobile() {
  isMobile.value = window.innerWidth <= 768;
}
onMounted(() => {
  checkMobile();
  window.addEventListener("resize", checkMobile);
});
onUnmounted(() => {
  window.removeEventListener("resize", checkMobile);
});
```

- [ ] **Step 3: Add mobile toggle CSS and price-grid mobile**

In the `<style scoped>` block, add:

```css
.mobile-cards {
  display: none;
}
@media (max-width: 768px) {
  .v-data-table {
    display: none;
  }
  .mobile-cards {
    display: block;
  }
  .price-grid {
    grid-template-columns: 1fr;
  }
}
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/views/ModelsView.vue
git commit -m "feat: make ModelsView responsive with mobile cards"
```

---

## Task 7: ApiKeysView Mobile

**Files:**
- Modify: `frontend/src/views/ApiKeysView.vue`

- [ ] **Step 1: Add mobile card list alongside table**

After the `v-data-table` closing `</div>` (line 72, before the dialog), insert:

```vue
    <!-- Mobile cards -->
    <div class="mobile-cards">
      <MobileDataCard
        v-for="k in store.apiKeys"
        :key="k.id"
        :items="[
          { label: $t('apiKeys.name'), value: k.name },
          { label: $t('apiKeys.keyPrefix'), value: k.key_prefix },
          { label: $t('apiKeys.status'), value: k.is_active ? $t('apiKeys.active') : $t('apiKeys.revoked') },
          { label: $t('apiKeys.rateLimit'), value: k.rate_limit_rpm === 0 ? '∞' : String(k.rate_limit_rpm) },
          { label: $t('apiKeys.lastUsed'), value: k.last_used_at ? new Date(k.last_used_at).toLocaleString() : $t('apiKeys.never') },
          { label: $t('apiKeys.created'), value: new Date(k.created_at).toLocaleDateString() },
        ]"
      >
        <template #actions>
          <button
            v-if="k.is_active"
            class="row-btn row-btn--danger"
            title="Revoke key"
            @click="handleDelete(k.id)"
          >
            <v-icon size="15">mdi-block-helper</v-icon>
          </button>
        </template>
      </MobileDataCard>
      <div v-if="!store.apiKeys.length" class="empty-state">
        <v-icon size="32" color="#4a4844">mdi-key-off-outline</v-icon>
        <p>{{ $t("apiKeys.noKeys") }}</p>
      </div>
    </div>
```

- [ ] **Step 2: Add dialog fullscreen for mobile**

Change both dialog opening lines:

```vue
    <v-dialog v-model="createDialog" :max-width="isMobile ? undefined : 460" :fullscreen="isMobile">
```

and:

```vue
    <v-dialog v-model="showKey" :max-width="isMobile ? undefined : 500" :fullscreen="isMobile">
```

Add to `<script setup>`:

```typescript
import { ref, onMounted, onUnmounted } from "vue";
import MobileDataCard from "../components/MobileDataCard.vue";

const isMobile = ref(false);
function checkMobile() {
  isMobile.value = window.innerWidth <= 768;
}
onMounted(() => {
  checkMobile();
  window.addEventListener("resize", checkMobile);
  store.fetch();
});
onUnmounted(() => {
  window.removeEventListener("resize", checkMobile);
});
```

Remove the old `onMounted(() => store.fetch());` line.

- [ ] **Step 3: Add mobile toggle CSS**

In the `<style scoped>` block, add:

```css
.mobile-cards {
  display: none;
}
@media (max-width: 768px) {
  .v-data-table {
    display: none;
  }
  .mobile-cards {
    display: block;
  }
}
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/views/ApiKeysView.vue
git commit -m "feat: make ApiKeysView responsive with mobile cards"
```

---

## Task 8: UsageView Mobile

**Files:**
- Modify: `frontend/src/views/UsageView.vue`

- [ ] **Step 1: Add mobile stats grid and chart adjustments**

In the `<style scoped>` block, add:

```css
@media (max-width: 768px) {
  .stats-grid {
    grid-template-columns: repeat(2, 1fr);
    gap: 8px;
  }
  .stat-card {
    padding: 12px 14px;
  }
  .stat-value {
    font-size: 1.1rem;
  }
  .chart-area {
    height: 240px;
  }
  .chart-section {
    padding: 14px;
  }
}
@media (max-width: 480px) {
  .stats-grid {
    grid-template-columns: 1fr;
  }
  .chart-area {
    height: 200px;
  }
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/views/UsageView.vue
git commit -m "feat: make UsageView responsive"
```

---

## Task 9: LogsView Mobile

**Files:**
- Modify: `frontend/src/views/LogsView.vue`

- [ ] **Step 1: Add mobile card list alongside table**

After the `v-data-table` closing tag and before the `.table-footer` div, insert:

```vue
      <!-- Mobile cards -->
      <div class="mobile-cards">
        <MobileDataCard
          v-for="log in store.logs"
          :key="log.id"
          :items="[
            { label: $t('logs.startedAt'), value: log.started_at ? formatTime(log.started_at) : '-' },
            { label: $t('logs.completedAt'), value: log.completed_at ? formatTime(log.completed_at) : '-' },
            { label: $t('logs.duration'), value: (log.latency_ms / 1000).toFixed(2) + 's' },
            { label: $t('logs.provider'), value: log.provider_name || '-' },
            { label: $t('logs.model'), value: getModelName(log.model) },
            { label: $t('logs.totalTokens'), value: log.total_tokens.toLocaleString() },
            { label: $t('logs.totalCost'), value: '$' + log.cost.toFixed(6) },
            { label: $t('logs.status'), value: log.is_error ? $t('logs.error') : $t('logs.ok') },
          ]"
        />
        <div v-if="!store.logs.length" class="empty-state">
          <v-icon size="32" color="#4a4844">mdi-text-box-search-outline</v-icon>
          <p>{{ $t("logs.noRecords") }}</p>
        </div>
      </div>
```

- [ ] **Step 2: Add mobile detection and import**

Add to `<script setup>`:

```typescript
import { ref, onMounted, onUnmounted } from "vue";
import MobileDataCard from "../components/MobileDataCard.vue";

const isMobile = ref(false);
function checkMobile() {
  isMobile.value = window.innerWidth <= 768;
}
onMounted(() => {
  checkMobile();
  window.addEventListener("resize", checkMobile);
  loadLogs();
});
onUnmounted(() => {
  window.removeEventListener("resize", checkMobile);
});
```

Remove the old `onMounted(() => loadLogs());` line.

- [ ] **Step 3: Add mobile toggle CSS**

In the `<style scoped>` block, add:

```css
.mobile-cards {
  display: none;
}
@media (max-width: 768px) {
  .v-data-table {
    display: none;
  }
  .mobile-cards {
    display: block;
  }
  .table-footer {
    justify-content: center;
  }
}
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/views/LogsView.vue
git commit -m "feat: make LogsView responsive with mobile cards"
```

---

## Task 10: LoginView Mobile

**Files:**
- Modify: `frontend/src/views/LoginView.vue`

- [ ] **Step 1: Add mobile auth styles**

In the `<style scoped>` block, append:

```css
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
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/views/LoginView.vue
git commit -m "feat: improve LoginView mobile layout"
```

---

## Task 11: RegisterView Mobile

**Files:**
- Modify: `frontend/src/views/RegisterView.vue`

- [ ] **Step 1: Add mobile auth styles**

In the `<style scoped>` block, append:

```css
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
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/views/RegisterView.vue
git commit -m "feat: improve RegisterView mobile layout"
```

---

## Task 12: Final Verification

**Files:** None (verification only)

- [ ] **Step 1: Run full frontend build**

Run: `cd /root/OmniRelay/frontend && bun run build`
Expected: Build completes successfully with `vue-tsc --noEmit` passing and Vite build finishing.

- [ ] **Step 2: Check for TypeScript errors**

If `vue-tsc` reports errors, fix them immediately. Common issues:
- Missing import for `MobileDataCard`
- `onMounted`/`onUnmounted` not imported from `vue`
- `isMobile` ref not defined before use in template

- [ ] **Step 3: Final commit**

```bash
git commit -m "feat: complete frontend mobile optimization"
```

---

## Spec Coverage Check

| Spec Section | Task |
|---|---|
| 1. Layout & Navigation (bottom tab bar) | Task 3 |
| 1. Layout & Navigation (tablet rail) | Task 3 (Vuetify `rail` prop already present) |
| 2. Data Tables → Mobile Card UI | Tasks 5, 6, 7, 9 |
| 2. Common Component | Task 2 |
| 3. Forms & Dialogs (fullscreen) | Tasks 5, 6, 7 |
| 4. Auth Pages | Tasks 10, 11 |
| 5. Dashboard & Charts | Task 4 |
| 6. Global Content Padding & Typography | Task 1 |
| 7. Breakpoint & CSS Architecture | Tasks 1, 3, 4, 5, 6, 7, 8, 9, 10, 11 |

---

## Placeholder Scan

- No "TBD", "TODO", "implement later" found.
- No vague "add appropriate error handling" found.
- No "Similar to Task N" shortcuts found.
- All file paths are exact.
- All code blocks contain complete content.

---

## Type Consistency Check

- `isMobile` ref name is consistent across all tasks.
- `checkMobile` function name is consistent.
- `MobileDataCard` component name and import path are consistent.
- Breakpoint `768px` is consistent across all tasks.
- `window.addEventListener` / `removeEventListener` pairs are present in all relevant tasks.
