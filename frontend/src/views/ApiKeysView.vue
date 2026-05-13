<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h1 class="page-title">{{ $t("apiKeys.title") }}</h1>
        <p class="page-sub">{{ $t("apiKeys.subtitle") }}</p>
      </div>
      <button class="btn-primary" @click="openCreateDialog">
        <v-icon size="15">mdi-plus</v-icon>
        {{ $t("apiKeys.issueKey") }}
      </button>
    </div>

    <div class="table-card">
      <v-data-table
        :headers="headers"
        :items="store.apiKeys"
        :loading="store.loading"
        density="comfortable"
        hide-default-footer
        :items-per-page="-1"
      >
        <template #item.key_prefix="{ item }">
          <code class="mono-tag">{{ item.key_prefix }}</code>
        </template>
        <template #item.is_active="{ item }">
          <span
            class="status-chip"
            :class="item.is_active ? 'status-chip--on' : 'status-chip--off'"
          >
            {{ item.is_active ? $t("apiKeys.active") : $t("apiKeys.revoked") }}
          </span>
        </template>
        <template #item.last_used_at="{ item }">
          <span class="dim-text">
            {{
              item.last_used_at
                ? new Date(item.last_used_at).toLocaleString()
                : $t("apiKeys.never")
            }}
          </span>
        </template>
        <template #item.rate_limit_rpm="{ item }">
          <span class="mono-val">{{
            item.rate_limit_rpm === 0 ? "∞" : item.rate_limit_rpm
          }}</span>
        </template>
        <template #item.created_at="{ item }">
          <span class="dim-text">{{
            new Date(item.created_at).toLocaleDateString()
          }}</span>
        </template>
        <template #item.actions="{ item }">
          <div class="row-actions">
            <button
              v-if="item.is_active"
              class="row-btn row-btn--danger"
              title="Revoke key"
              @click="handleDelete(item.id)"
            >
              <v-icon size="15">mdi-block-helper</v-icon>
            </button>
          </div>
        </template>
        <template #no-data>
          <div class="empty-state">
            <v-icon size="32" color="#4a4844">mdi-key-off-outline</v-icon>
            <p>{{ $t("apiKeys.noKeys") }}</p>
          </div>
        </template>
      </v-data-table>
    </div>

    <!-- Create dialog -->
    <v-dialog v-model="createDialog" max-width="460">
      <div class="dialog-card">
        <div class="dialog-header">
          <h2 class="dialog-title">{{ $t("apiKeys.issueNew") }}</h2>
          <button class="dialog-close" @click="createDialog = false">
            <v-icon size="18">mdi-close</v-icon>
          </button>
        </div>
        <div class="dialog-body">
          <div class="field-group">
            <label class="field-label">{{ $t("apiKeys.keyName") }}</label>
            <input
              v-model="form.name"
              class="field-input"
              placeholder="My App Key"
            />
          </div>
          <div class="field-group">
            <label class="field-label">{{ $t("apiKeys.rateLimitRpm") }}</label>
            <input
              v-model.number="form.rate_limit_rpm"
              type="number"
              class="field-input"
              placeholder="0 = unlimited"
            />
            <span class="field-hint">{{ $t("apiKeys.rateLimitHint") }}</span>
          </div>
          <div v-if="dialogError" class="alert alert--error">
            <v-icon size="14">mdi-alert-circle-outline</v-icon>
            {{ dialogError }}
          </div>
        </div>
        <div class="dialog-footer">
          <button class="btn-ghost" @click="createDialog = false">
            {{ $t("common.cancel") }}
          </button>
          <button
            class="btn-primary"
            @click="handleCreate"
            :disabled="creating"
          >
            <span v-if="!creating">{{ $t("common.create") }}</span>
            <span v-else class="btn-spinner" />
          </button>
        </div>
      </div>
    </v-dialog>

    <!-- Show key dialog -->
    <v-dialog v-model="showKey" max-width="500">
      <div class="dialog-card">
        <div class="dialog-header">
          <h2 class="dialog-title" style="color: #2ec4b6">
            {{ $t("apiKeys.keyCreated") }}
          </h2>
          <button class="dialog-close" @click="showKey = false">
            <v-icon size="18">mdi-close</v-icon>
          </button>
        </div>
        <div class="dialog-body">
          <p class="reveal-note">{{ $t("apiKeys.saveKeyNote") }}</p>
          <div class="key-reveal">
            <code class="key-value">{{ newKey }}</code>
            <button
              class="copy-btn"
              @click="copyKey"
              :class="{ 'copy-btn--copied': copied }"
            >
              <v-icon size="15">{{
                copied ? "mdi-check" : "mdi-content-copy"
              }}</v-icon>
            </button>
          </div>
        </div>
        <div class="dialog-footer">
          <button
            class="btn-primary"
            @click="
              showKey = false;
              createDialog = false;
            "
          >
            {{ $t("common.done") }}
          </button>
        </div>
      </div>
    </v-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import { useI18n } from "vue-i18n";
import { useApiKeysStore } from "../stores/apikeys";

const { t } = useI18n();
const store = useApiKeysStore();
const createDialog = ref(false);
const showKey = ref(false);
const newKey = ref("");
const creating = ref(false);
const dialogError = ref("");
const copied = ref(false);

const form = ref({ name: "", rate_limit_rpm: 0 });

const headers = computed(() => [
  { title: t("apiKeys.name"), key: "name" },
  { title: t("apiKeys.keyPrefix"), key: "key_prefix" },
  { title: t("apiKeys.status"), key: "is_active" },
  { title: t("apiKeys.rateLimit"), key: "rate_limit_rpm" },
  { title: t("apiKeys.lastUsed"), key: "last_used_at" },
  { title: t("apiKeys.created"), key: "created_at" },
  { title: "", key: "actions", sortable: false, width: 60 },
]);

function openCreateDialog() {
  dialogError.value = "";
  form.value = { name: "", rate_limit_rpm: 0 };
  createDialog.value = true;
}

async function handleCreate() {
  creating.value = true;
  dialogError.value = "";
  try {
    const { data } = await store.create(
      form.value.name,
      form.value.rate_limit_rpm,
    );
    newKey.value = data.plain_key;
    showKey.value = true;
    await store.fetch();
  } catch (e: any) {
    dialogError.value = e.response?.data?.error || t("apiKeys.createFailed");
  } finally {
    creating.value = false;
  }
}

function copyKey() {
  navigator.clipboard.writeText(newKey.value);
  copied.value = true;
  setTimeout(() => {
    copied.value = false;
  }, 2000);
}

async function handleDelete(id: number) {
  if (!confirm(t("apiKeys.revokeConfirm"))) return;
  await store.remove(id);
}

onMounted(() => store.fetch());
</script>

<style scoped>
@import "../styles/page-shared.css";

.dim-text {
  font-family: "DM Sans", sans-serif;
  font-size: 0.825rem;
  color: #7c7a75;
}
.mono-val {
  font-family: "JetBrains Mono", monospace;
  font-size: 0.82rem;
  color: #e8e6e1;
}
.field-hint {
  font-family: "DM Sans", sans-serif;
  font-size: 0.75rem;
  color: #4a4844;
  padding-left: 2px;
}

.reveal-note {
  font-family: "DM Sans", sans-serif;
  font-size: 0.825rem;
  color: #7c7a75;
  margin: 0 0 12px;
}
.key-reveal {
  display: flex;
  align-items: center;
  gap: 8px;
  background: #1a1a1f;
  border: 1px solid rgba(46, 196, 182, 0.25);
  border-radius: 9px;
  padding: 10px 14px;
}
.key-value {
  flex: 1;
  font-family: "JetBrains Mono", monospace !important;
  font-size: 0.8rem !important;
  color: #2ec4b6 !important;
  background: transparent !important;
  border: none !important;
  padding: 0 !important;
  border-radius: 0 !important;
  word-break: break-all;
}
.copy-btn {
  width: 28px;
  height: 28px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(46, 196, 182, 0.1);
  border: 1px solid rgba(46, 196, 182, 0.2);
  border-radius: 6px;
  cursor: pointer;
  color: #2ec4b6;
  transition: all 0.15s;
}
.copy-btn:hover {
  background: rgba(46, 196, 182, 0.18);
}
.copy-btn--copied {
  color: #e8a020;
  border-color: rgba(232, 160, 32, 0.3);
  background: rgba(232, 160, 32, 0.08);
}
</style>
