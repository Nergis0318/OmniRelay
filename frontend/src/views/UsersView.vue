<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h1 class="page-title">{{ $t("users.title") }}</h1>
        <p class="page-sub">{{ $t("users.subtitle") }}</p>
      </div>
    </div>

    <div class="table-card">
      <v-data-table
        :headers="headers"
        :items="store.users"
        :loading="store.loading"
        density="comfortable"
        hide-default-footer
        :items-per-page="-1"
      >
        <template #item.username="{ item }">
          <span class="username-cell">
            <v-icon size="16" class="user-icon"
              >mdi-account-circle-outline</v-icon
            >
            {{ item.username }}
          </span>
        </template>
        <template #item.email="{ item }">
          <span class="dim-text">{{ item.email }}</span>
        </template>
        <template #item.is_admin="{ item }">
          <span
            class="status-chip"
            :class="item.is_admin ? 'status-chip--on' : 'status-chip--off'"
          >
            {{ item.is_admin ? $t("users.admin") : $t("users.member") }}
          </span>
        </template>
        <template #item.created_at="{ item }">
          <span class="dim-text">{{
            new Date(item.created_at).toLocaleDateString()
          }}</span>
        </template>
        <template #item.actions="{ item }">
          <div class="row-actions">
            <button
              class="row-btn"
              :title="$t('users.toggleRole')"
              @click="handleToggleRole(item)"
            >
              <v-icon size="15">mdi-shield-crown-outline</v-icon>
            </button>
            <button
              class="row-btn"
              :title="$t('users.resetPassword')"
              @click="handleResetPassword(item)"
            >
              <v-icon size="15">mdi-key-reset</v-icon>
            </button>
            <button
              class="row-btn"
              :title="$t('users.providers')"
              @click="openProvidersDialog(item)"
            >
              <v-icon size="15">mdi-server-network</v-icon>
            </button>
            <button
              class="row-btn row-btn--danger"
              :title="$t('users.delete')"
              @click="handleDelete(item)"
            >
              <v-icon size="15">mdi-delete-outline</v-icon>
            </button>
          </div>
        </template>
        <template #no-data>
          <div class="empty-state">
            <v-icon size="32" color="#4a4844">mdi-account-group-outline</v-icon>
            <p>{{ $t("users.noUsers") }}</p>
          </div>
        </template>
      </v-data-table>
    </div>

    <!-- Mobile cards -->
    <div class="mobile-cards">
      <MobileDataCard
        v-for="u in store.users"
        :key="u.id"
        :items="[
          { label: $t('users.username'), value: u.username },
          { label: $t('users.email'), value: u.email },
          {
            label: $t('users.role'),
            value: u.is_admin ? $t('users.admin') : $t('users.member'),
          },
          {
            label: $t('users.created'),
            value: new Date(u.created_at).toLocaleDateString(),
          },
        ]"
      >
        <template #actions>
          <button
            class="row-btn"
            :title="$t('users.toggleRole')"
            @click="handleToggleRole(u)"
          >
            <v-icon size="15">mdi-shield-crown-outline</v-icon>
          </button>
          <button
            class="row-btn"
            :title="$t('users.resetPassword')"
            @click="handleResetPassword(u)"
          >
            <v-icon size="15">mdi-key-reset</v-icon>
          </button>
          <button
            class="row-btn"
            :title="$t('users.providers')"
            @click="openProvidersDialog(u)"
          >
            <v-icon size="15">mdi-server-network</v-icon>
          </button>
          <button
            class="row-btn row-btn--danger"
            :title="$t('users.delete')"
            @click="handleDelete(u)"
          >
            <v-icon size="15">mdi-delete-outline</v-icon>
          </button>
        </template>
      </MobileDataCard>
      <div v-if="!store.users.length" class="empty-state">
        <v-icon size="32" color="#4a4844">mdi-account-group-outline</v-icon>
        <p>{{ $t("users.noUsers") }}</p>
      </div>
    </div>

    <!-- Confirm delete dialog -->
    <v-dialog v-model="deleteDialog" max-width="400">
      <div class="dialog-card">
        <div class="dialog-header">
          <h2 class="dialog-title">{{ $t("users.deleteTitle") }}</h2>
          <button class="dialog-close" @click="deleteDialog = false">
            <v-icon size="18">mdi-close</v-icon>
          </button>
        </div>
        <div class="dialog-body">
          <p class="confirm-text">
            {{ $t("users.deleteConfirm", { name: targetUser?.username }) }}
          </p>
          <div v-if="dialogError" class="alert alert--error">
            <v-icon size="14">mdi-alert-circle-outline</v-icon>
            {{ dialogError }}
          </div>
        </div>
        <div class="dialog-footer">
          <button class="btn-ghost" @click="deleteDialog = false">
            {{ $t("common.cancel") }}
          </button>
          <button class="btn-danger" @click="confirmDelete" :disabled="busy">
            <span v-if="!busy">{{ $t("common.delete") }}</span>
            <span v-else class="btn-spinner" />
          </button>
        </div>
      </div>
    </v-dialog>

    <!-- Reset password result dialog -->
    <v-dialog v-model="resetDialog" max-width="420">
      <div class="dialog-card">
        <div class="dialog-header">
          <h2 class="dialog-title" style="color: #2ec4b6">
            {{ $t("users.resetTitle") }}
          </h2>
          <button class="dialog-close" @click="resetDialog = false">
            <v-icon size="18">mdi-close</v-icon>
          </button>
        </div>
        <div class="dialog-body">
          <p class="reveal-note">{{ $t("users.resetNote") }}</p>
          <div class="key-reveal">
            <code class="key-value">{{ resetCode }}</code>
            <button
              class="copy-btn"
              @click="copyCode"
              :class="{ 'copy-btn--copied': copied }"
            >
              <v-icon size="15">{{
                copied ? "mdi-check" : "mdi-content-copy"
              }}</v-icon>
            </button>
          </div>
          <div v-if="dialogError" class="alert alert--error">
            <v-icon size="14">mdi-alert-circle-outline</v-icon>
            {{ dialogError }}
          </div>
        </div>
        <div class="dialog-footer">
          <button class="btn-primary" @click="resetDialog = false">
            {{ $t("common.close") }}
          </button>
        </div>
      </div>
    </v-dialog>

    <!-- Providers dialog -->
    <v-dialog
      v-model="providersDialog"
      :max-width="isMobile ? undefined : 460"
      :fullscreen="isMobile"
    >
      <div class="dialog-card">
        <div class="dialog-header">
          <h2 class="dialog-title">{{ $t("users.providersTitle") }}</h2>
          <button class="dialog-close" @click="providersDialog = false">
            <v-icon size="18">mdi-close</v-icon>
          </button>
        </div>
        <div class="dialog-body">
          <p class="field-hint" style="margin-bottom: 12px">
            {{ $t("users.providersHint") }}
          </p>
          <div v-if="providersLoading" class="empty-state">
            <span class="btn-spinner" />
          </div>
          <div v-else class="provider-checklist">
            <label
              v-for="p in providersStore.providers"
              :key="p.id"
              class="provider-check-item"
            >
              <input
                type="checkbox"
                :value="p.id"
                v-model="selectedProviderIds"
              />
              <span>{{ p.name }}</span>
              <code class="mono-tag">{{ p.provider_key }}</code>
            </label>
            <p v-if="!providersStore.providers.length" class="dim-text">
              {{ $t("users.noProviders") }}
            </p>
          </div>
          <div v-if="dialogError" class="alert alert--error">
            <v-icon size="14">mdi-alert-circle-outline</v-icon>
            {{ dialogError }}
          </div>
        </div>
        <div class="dialog-footer">
          <button class="btn-ghost" @click="providersDialog = false">
            {{ $t("common.cancel") }}
          </button>
          <button
            class="btn-primary"
            @click="saveProviders"
            :disabled="busy"
          >
            <span v-if="!busy">{{ $t("common.save") }}</span>
            <span v-else class="btn-spinner" />
          </button>
        </div>
      </div>
    </v-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from "vue";
import { useI18n } from "vue-i18n";
import { useUsersStore, type User } from "../stores/users";
import { useProvidersStore } from "../stores/providers";
import MobileDataCard from "../components/MobileDataCard.vue";

const { t } = useI18n();
const store = useUsersStore();
const providersStore = useProvidersStore();

const isMobile = ref(false);
function checkMobile() {
  isMobile.value = window.innerWidth <= 768;
}

const headers = computed(() => [
  { title: t("users.username"), key: "username" },
  { title: t("users.email"), key: "email" },
  { title: t("users.role"), key: "is_admin" },
  { title: t("users.created"), key: "created_at" },
  { title: "", key: "actions", sortable: false },
]);

const targetUser = ref<User | null>(null);
const deleteDialog = ref(false);
const resetDialog = ref(false);
const providersDialog = ref(false);
const resetCode = ref("");
const copied = ref(false);
const busy = ref(false);
const dialogError = ref("");
const providersLoading = ref(false);
const selectedProviderIds = ref<number[]>([]);

async function handleToggleRole(u: User) {
  try {
    dialogError.value = "";
    await store.setRole(u.id, !u.is_admin);
  } catch (err: any) {
    dialogError.value = err?.response?.data?.error || err?.message;
    alert(dialogError.value);
  }
}

function handleDelete(u: User) {
  targetUser.value = u;
  dialogError.value = "";
  deleteDialog.value = true;
}

async function confirmDelete() {
  if (!targetUser.value) return;
  busy.value = true;
  dialogError.value = "";
  try {
    await store.remove(targetUser.value.id);
    deleteDialog.value = false;
  } catch (err: any) {
    dialogError.value = err?.response?.data?.error || err?.message;
  } finally {
    busy.value = false;
  }
}

async function handleResetPassword(u: User) {
  dialogError.value = "";
  try {
    resetCode.value = await store.resetPassword(u.id);
    copied.value = false;
    resetDialog.value = true;
  } catch (err: any) {
    alert(err?.response?.data?.error || err?.message);
  }
}

function copyCode() {
  navigator.clipboard.writeText(resetCode.value);
  copied.value = true;
  setTimeout(() => (copied.value = false), 2000);
}

async function openProvidersDialog(u: User) {
  targetUser.value = u;
  dialogError.value = "";
  providersLoading.value = true;
  providersDialog.value = true;
  try {
    if (!providersStore.providers.length) {
      await providersStore.fetch();
    }
    selectedProviderIds.value = await store.getProviders(u.id);
  } catch (err: any) {
    dialogError.value = err?.response?.data?.error || err?.message;
  } finally {
    providersLoading.value = false;
  }
}

async function saveProviders() {
  if (!targetUser.value) return;
  busy.value = true;
  dialogError.value = "";
  try {
    await store.setProviders(targetUser.value.id, selectedProviderIds.value);
    providersDialog.value = false;
  } catch (err: any) {
    dialogError.value = err?.response?.data?.error || err?.message;
  } finally {
    busy.value = false;
  }
}

onMounted(() => {
  checkMobile();
  window.addEventListener("resize", checkMobile);
  store.fetch();
});
onUnmounted(() => {
  window.removeEventListener("resize", checkMobile);
});
</script>

<style scoped>
@import "../styles/page-shared.css";

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

.dim-text {
  font-family: "DM Sans", sans-serif;
  font-size: 0.825rem;
  color: #7c7a75;
}

.username-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  font-family: "DM Sans", sans-serif;
  font-size: 0.875rem;
  font-weight: 500;
  color: #e8e6e1;
}
.user-icon {
  color: #7c7a75;
  flex-shrink: 0;
}

.provider-checklist {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.provider-check-item {
  display: flex;
  align-items: center;
  gap: 10px;
  font-family: "DM Sans", sans-serif;
  font-size: 0.85rem;
  color: #e8e6e1;
  cursor: pointer;
}
.provider-check-item input[type="checkbox"] {
  accent-color: #2ec4b6;
  width: 16px;
  height: 16px;
}

.confirm-text {
  font-family: "DM Sans", sans-serif;
  font-size: 0.875rem;
  color: #b0ada6;
}
</style>
